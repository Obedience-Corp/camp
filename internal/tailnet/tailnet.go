// Package tailnet reads what the local tailscaled already knows: the health of
// this node and the addresses of its peers. It exists so the dial-fallback path
// (internal/remote) and the diagnose path (cmd/camp) consult one copy of that
// knowledge instead of each parsing `tailscale status --json` their own way.
//
// Everything here is advisory. Every function degrades to "no answer" — never
// an error the caller must handle — because the tailnet is a second source of
// truth, not a dependency: a machine without tailscale installed must behave
// exactly as it did before this package existed.
package tailnet

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// probeTimeout bounds the `tailscale status --json` read. tailscaled answers
// from local state, so it is fast or it is wedged; either way we do not wait.
const probeTimeout = 2 * time.Second

// magicDNSSuffix is the domain every MagicDNS name sits under.
const magicDNSSuffix = ".ts.net"

// NormalizeDNSName mirrors the festival app's normalize_dns_name: trim
// whitespace and the trailing FQDN dot MagicDNS names carry
// ("devbox.tailnet.ts.net.").
func NormalizeDNSName(v string) string {
	return strings.TrimSuffix(strings.TrimSpace(v), ".")
}

// IsMagicDNSName reports whether host sits under the MagicDNS domain,
// tolerating the trailing FQDN dot and any casing (hostnames are
// case-insensitive).
func IsMagicDNSName(host string) bool {
	return strings.HasSuffix(strings.ToLower(NormalizeDNSName(host)), magicDNSSuffix)
}

// failureRetryInterval is how long a failed `tailscale status --json` read is
// remembered before the next caller may retry it. Failure is memoized at all —
// rather than retried per caller — so a fleet operation on a machine without
// tailscale pays for one failed subprocess, not one per row. It expires so a
// long-lived process (the machine TUI) recovers when tailscaled comes back,
// instead of freezing "no fallback" until exit. Success is kept for the whole
// process: tailnet 100.x addresses are stable, and every consumer here is
// advisory.
const failureRetryInterval = 15 * time.Second

// statusSource memoizes one raw `tailscale status --json` read. The peer table
// and the health array are two views of the same snapshot, and a process that
// consults both (or consults either for many machines, as `list --remote`
// does) should pay for the subprocess once. The mutex makes the memo safe
// under the concurrent fan-out: later callers block on the first read rather
// than racing their own.
type statusSource struct {
	mu       sync.Mutex
	read     bool
	data     []byte
	err      error
	failedAt time.Time
	run      func() ([]byte, error)
	now      func() time.Time // test seam; nil means time.Now
}

func (s *statusSource) get() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	nowFn := s.now
	if nowFn == nil {
		nowFn = time.Now
	}
	if s.read && (s.err == nil || nowFn().Sub(s.failedAt) < failureRetryInterval) {
		return s.data, s.err
	}
	s.data, s.err = s.run()
	s.read = true
	if s.err != nil {
		s.failedAt = nowFn()
	}
	return s.data, s.err
}

// defaultStatus is the per-process snapshot. First caller triggers the read;
// its own probeTimeout bounds it, so a caller's context arriving later cannot
// extend or cut it — acceptable for a bounded 2s local read.
var defaultStatus = &statusSource{run: runStatusJSON}

func runStatusJSON() ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	return exec.CommandContext(ctx, "tailscale", "status", "--json").Output()
}

// HealthMessages returns tailscale's own health complaints and whether they
// were readable at all. Readable-but-empty is a real answer (tailscale is
// healthy); unreadable means the caller loses a better sentence, never a
// diagnosis.
func HealthMessages(_ context.Context) ([]string, bool) {
	out, err := defaultStatus.get()
	if err != nil {
		return nil, false
	}
	return ParseHealth(out)
}

// PeerAddress returns the tailnet address of the peer whose MagicDNS name is
// dnsName, from the local peer table — no DNS involved. found is false when
// tailscale is absent, the status is unreadable, or no peer matches; callers
// then behave as if this package did not exist.
//
// Offline peers are returned like any other: their address is still the right
// address to dial, and letting ssh report the timeout is more honest than
// asserting "down" from a status field that may be stale.
func PeerAddress(_ context.Context, dnsName string) (addr string, found bool) {
	out, err := defaultStatus.get()
	if err != nil {
		return "", false
	}
	return ParsePeerAddress(out, dnsName)
}

// SelfAddress returns this machine's own tailnet address from the local
// status — the Self-node sibling of PeerAddress. It exists for probes that ask
// "can a peer reach me at the address the dial fallback would use": when
// MagicDNS is broken, this machine's own DNS name does not resolve locally
// either, and a probe that dials only the name reports "not listening" about
// an sshd that is (design WI-44f57e, Q7).
func SelfAddress(_ context.Context) (addr string, found bool) {
	out, err := defaultStatus.get()
	if err != nil {
		return "", false
	}
	return ParseSelfAddress(out)
}

// ParseSelfAddress is the pure half of SelfAddress.
func ParseSelfAddress(data []byte) (string, bool) {
	start := bytes.IndexByte(data, '{')
	if start < 0 {
		return "", false
	}
	var status struct {
		Self *struct {
			TailscaleIPs []string `json:"TailscaleIPs"`
		} `json:"Self"`
	}
	if err := json.Unmarshal(data[start:], &status); err != nil || status.Self == nil {
		return "", false
	}
	return preferredAddress(status.Self.TailscaleIPs)
}

// ParseHealth is the pure half of HealthMessages, split out so the shape
// tolerance is testable without a tailscale binary.
//
// The Health field has carried two shapes across tailscale releases — plain
// strings, and structured objects with a title/text — so each entry is decoded
// either way and an entry matching neither is skipped rather than failing the
// read. Any warning banner printed before the JSON is skipped, matching
// parseTailscaleStatus (cmd/camp/machine_discover.go).
func ParseHealth(data []byte) ([]string, bool) {
	start := bytes.IndexByte(data, '{')
	if start < 0 {
		return nil, false
	}
	var status struct {
		Health []json.RawMessage `json:"Health"`
	}
	if err := json.Unmarshal(data[start:], &status); err != nil {
		return nil, false
	}
	msgs := make([]string, 0, len(status.Health))
	for _, raw := range status.Health {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			if s = strings.TrimSpace(s); s != "" {
				msgs = append(msgs, s)
			}
			continue
		}
		var obj struct {
			Title string `json:"Title"`
			Text  string `json:"Text"`
		}
		if json.Unmarshal(raw, &obj) == nil {
			// Prefer the longer, more specific sentence; a bare title such as
			// "DNS" diagnoses nothing on its own.
			if s := strings.TrimSpace(obj.Text); s != "" {
				msgs = append(msgs, s)
			} else if s := strings.TrimSpace(obj.Title); s != "" {
				msgs = append(msgs, s)
			}
		}
	}
	return msgs, true
}

// ParsePeerAddress is the pure half of PeerAddress. Matching is on normalized
// DNSName (trailing dot, casing), the comparison machine discovery already
// performs — the cases where a hand-rolled comparison drifts. Self is
// consulted alongside the peers: a machines.yaml entry that names this node's
// own MagicDNS name (a loopback hop, or one machines file shared across the
// fleet) deserves the same rescue as any peer.
func ParsePeerAddress(data []byte, dnsName string) (string, bool) {
	want := strings.ToLower(NormalizeDNSName(dnsName))
	if want == "" {
		return "", false
	}
	start := bytes.IndexByte(data, '{')
	if start < 0 {
		return "", false
	}
	type node struct {
		DNSName      string   `json:"DNSName"`
		TailscaleIPs []string `json:"TailscaleIPs"`
	}
	var status struct {
		Self *node           `json:"Self"`
		Peer map[string]node `json:"Peer"`
	}
	if err := json.Unmarshal(data[start:], &status); err != nil {
		return "", false
	}
	nodes := make([]node, 0, len(status.Peer)+1)
	for _, n := range status.Peer {
		nodes = append(nodes, n)
	}
	if status.Self != nil {
		nodes = append(nodes, *status.Self)
	}
	for _, n := range nodes {
		if strings.ToLower(NormalizeDNSName(n.DNSName)) != want {
			continue
		}
		return preferredAddress(n.TailscaleIPs)
	}
	return "", false
}

// preferredAddress picks the dial address from a peer's TailscaleIPs: the
// first IPv4, else the first IPv6 (v6-only tailnet). v4 is preferred
// explicitly rather than positionally because downstream `host:path` transfer
// targets need bracketing for a bare v6 literal; v4 keeps the common case away
// from that entirely.
func preferredAddress(ips []string) (string, bool) {
	firstV6 := ""
	for _, raw := range ips {
		ip := net.ParseIP(strings.TrimSpace(raw))
		if ip == nil {
			continue
		}
		if ip.To4() != nil {
			return ip.String(), true
		}
		if firstV6 == "" {
			firstV6 = ip.String()
		}
	}
	if firstV6 != "" {
		return firstV6, true
	}
	return "", false
}
