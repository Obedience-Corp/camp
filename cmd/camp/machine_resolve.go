package main

import (
	"context"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/Obedience-Corp/camp/internal/tailnet"
)

// resolveProbeTimeout bounds the name lookup diagnose runs before it lets ssh
// try. A resolver that cannot answer inside this budget is itself the finding,
// so the deadline is short and the failure is reported rather than retried.
const resolveProbeTimeout = 2 * time.Second

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
// depends on the individual host. (tailnet.HealthMessages memoizes the status
// read process-wide as well; the OnceValues here keeps this seam's contract
// independent of that.)
func defaultResolveProbes() resolveProbes {
	health := sync.OnceValues(func() ([]string, bool) {
		return tailnet.HealthMessages(context.Background())
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
	if !tailnet.IsMagicDNSName(host) {
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
