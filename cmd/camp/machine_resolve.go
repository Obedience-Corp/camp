package main

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

// resolveProbeTimeout bounds the name lookup diagnose runs before it lets ssh
// try. A resolver that cannot answer inside this budget is itself the finding,
// so the deadline is short and the failure is reported rather than retried.
const resolveProbeTimeout = 2 * time.Second

// magicDNSSuffix is the domain every MagicDNS name sits under. A host below it
// that will not resolve is a MagicDNS failure specifically, which has a
// different cause and a different fix than an ordinary DNS miss, so the two
// get different hints.
const magicDNSSuffix = ".ts.net"

// resolveReport is what diagnose can say about turning a machine's host into an
// address, established before any ssh is attempted.
//
// This exists because "camp is unavailable" and "the name never resolved" are
// different findings with different fixes, and diagnose used to report both as
// the former. An operator whose MagicDNS is down was told their remote camp
// might be missing or too old, which is advice for a machine they cannot even
// address yet.
type resolveReport struct {
	// Checked is false when there was no host to look up. It keeps "not
	// applicable" distinct from "looked, and it failed" for callers that would
	// otherwise read the zero value as a failure.
	Checked  bool
	Resolved bool
	Addrs    []string
	Hint     string
}

// resolveProbes are the seams the cause matrix is tested through, mirroring
// reverseProbes (machine_reverse.go).
type resolveProbes struct {
	LookupHost func(ctx context.Context, host string) ([]string, error)
	// TailscaleHealth returns tailscale's own health messages and whether they
	// were readable at all. Advisory: when it cannot answer, the MagicDNS hint
	// falls back to naming the command that will.
	TailscaleHealth func(ctx context.Context) ([]string, bool)
}

// defaultResolveProbes wires the real resolver and the real tailscale CLI.
//
// The health read is memoized per call because it is one fact about this
// machine, not one per row: diagnosing a fleet whose MagicDNS is down would
// otherwise shell out to tailscale once per broken machine to learn the same
// sentence. checkReverseReachability solves the same problem by being hoisted
// out of the loop; this one cannot be, because whether it is consulted at all
// depends on the individual host.
func defaultResolveProbes() resolveProbes {
	health := sync.OnceValues(func() ([]string, bool) {
		return tailscaleHealthMessages(context.Background())
	})
	return resolveProbes{
		LookupHost: func(ctx context.Context, host string) ([]string, error) {
			return net.DefaultResolver.LookupHost(ctx, host)
		},
		TailscaleHealth: func(context.Context) ([]string, bool) { return health() },
	}
}

// checkHostResolves reports whether host can be turned into an address, and
// names the cause when it cannot.
//
// Like the reverse check, this reports only what it can observe and never
// repairs anything (D003): every failure hint names exactly one action for the
// operator to take.
func checkHostResolves(ctx context.Context, host string, p resolveProbes) resolveReport {
	host = strings.TrimSpace(host)
	if host == "" || p.LookupHost == nil {
		return resolveReport{}
	}
	// An address literal is already an address. Handing it to the resolver
	// would succeed for the wrong reason on hosts where the resolver appends a
	// search domain, and costs a lookup to learn nothing.
	if net.ParseIP(host) != nil {
		return resolveReport{Checked: true, Resolved: true, Addrs: []string{host}}
	}

	ctx, cancel := context.WithTimeout(ctx, resolveProbeTimeout)
	defer cancel()

	// An empty answer with a nil error is treated as failure: a name that
	// resolves to nothing is not one ssh can dial, whatever the resolver meant.
	if addrs, err := p.LookupHost(ctx, host); err == nil && len(addrs) > 0 {
		return resolveReport{Checked: true, Resolved: true, Addrs: addrs}
	}
	return resolveReport{Checked: true, Hint: resolveFailureHint(ctx, host, p)}
}

// resolveFailureHint names the cause of a failed lookup. MagicDNS names get a
// MagicDNS answer — quoting tailscale's own health text when it will give one,
// because that sentence is the actual diagnosis and camp should not paraphrase
// it — and everything else gets the ordinary-DNS answer.
func resolveFailureHint(ctx context.Context, host string, p resolveProbes) string {
	if !isMagicDNSName(host) {
		return "this name does not resolve; check it for a typo, then confirm your resolver " +
			"can answer for it ('getent hosts " + host + "')"
	}
	if p.TailscaleHealth != nil {
		if msgs, ok := p.TailscaleHealth(ctx); ok {
			if msg := firstDNSHealthMessage(msgs); msg != "" {
				return "MagicDNS is not resolving here; tailscale reports: " + msg
			}
		}
	}
	return "this is a MagicDNS name and it does not resolve; run 'tailscale status' and read " +
		"the health output at the bottom for the reason"
}

// isMagicDNSName reports whether host sits under the MagicDNS domain, tolerating
// the trailing FQDN dot and any casing (hostnames are case-insensitive).
func isMagicDNSName(host string) bool {
	h := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	return strings.HasSuffix(h, magicDNSSuffix)
}

// firstDNSHealthMessage picks the DNS-related entry out of tailscale's health
// messages. The array carries every current complaint, most of which have
// nothing to do with name resolution, and pasting an unrelated one under a
// resolution failure would misdirect the operator.
func firstDNSHealthMessage(msgs []string) string {
	for _, m := range msgs {
		m = strings.TrimSpace(m)
		if m != "" && strings.Contains(strings.ToLower(m), "dns") {
			return m
		}
	}
	return ""
}

// tailscaleHealthMessages reads the Health array out of `tailscale status --json`.
//
// The field has carried two shapes across tailscale releases — plain strings,
// and structured objects with a title/text — so each entry is decoded either
// way and an entry matching neither is skipped rather than failing the read.
// The whole call is advisory: when it cannot answer, the caller loses a better
// sentence, never the diagnosis.
func tailscaleHealthMessages(ctx context.Context) ([]string, bool) {
	ctx, cancel := context.WithTimeout(ctx, resolveProbeTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, "tailscale", "status", "--json").Output()
	if err != nil {
		return nil, false
	}
	return parseTailscaleHealth(out)
}

// parseTailscaleHealth is the pure half of tailscaleHealthMessages, split out so
// the shape tolerance is testable without a tailscale binary. It skips any
// warning banner printed before the JSON, matching parseTailscaleStatus.
func parseTailscaleHealth(data []byte) ([]string, bool) {
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
	// Readable but empty is a real answer: tailscale is healthy. Distinguishing
	// it from unreadable is what stops the hint from quoting silence.
	return msgs, true
}
