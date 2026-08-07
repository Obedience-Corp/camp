// Package rsyncprobe resolves which rsync implementation a transfer will
// actually execute, on this machine and on a peer, and decides whether its
// delta engine can be trusted (decision D002).
//
// It exists because "rsync" is not one program. A single macOS host routinely
// carries Homebrew's genuine rsync 3.4.4 (protocol 32) first on PATH and
// Apple's openrsync at /usr/bin/rsync at the same time, and which one runs
// depends on the PATH of whichever shell ends up invoking it — a login shell
// over ssh may not resolve the same binary an interactive terminal does.
// Assuming an engine from the operating system is therefore wrong on exactly
// the machines this matters for, so camp asks the binary it is about to run.
//
// The package lives on its own rather than inside a transport because three
// callers exec rsync independently (internal/artifacts, internal/transfer,
// internal/clone) and all three need the same answer. It depends only on
// remote/machines/errors/fsutil, so any of them can import it.
package rsyncprobe

import (
	"context"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// Kind is which implementation answered the probe.
type Kind string

const (
	// KindRsync is genuine rsync (rsync.samba.org), the only implementation
	// with a delta engine camp will use.
	KindRsync Kind = "rsync"
	// KindOpenRsync is Apple's openrsync. It speaks protocol 29 and advertises
	// itself as "rsync version 2.6.9 compatible", but it is a separate
	// implementation and camp does not rely on its delta behaviour.
	KindOpenRsync Kind = "openrsync"
	// KindAbsent means no rsync was found on that end at all.
	KindAbsent Kind = "absent"
	// KindUnknown means something answered but camp could not identify it.
	// Treated exactly like an unusable engine: unrecognized is not trusted.
	KindUnknown Kind = "unknown"
)

// minDeltaProtocol is the protocol version at which camp trusts the delta
// engine. Genuine rsync gained the protocol-30 wire format in 3.0.0; below
// that the negotiation and incremental-recursion behaviour differ enough that
// camp prefers an honest whole-file copy to a delta it cannot reason about.
const minDeltaProtocol = 30

// Engine is what the probe found at one end of a transfer.
type Engine struct {
	// Binary is the path that was actually resolved and executed. Recorded
	// because "which rsync" is the whole question this package answers.
	Binary string `json:"binary,omitempty"`
	// Kind is the implementation that answered.
	Kind Kind `json:"kind"`
	// Version is the version string reported. For openrsync this is the
	// compatibility version it claims (2.6.9), not a genuine rsync version —
	// Kind, not Version, is what decides anything.
	Version string `json:"version,omitempty"`
	// Protocol is the reported wire protocol version, 0 when unknown.
	Protocol int `json:"protocol,omitempty"`
}

// DeltaUsable reports whether camp will use this engine's delta transfer.
// Only genuine rsync at protocol >= 30 qualifies; everything else, including
// anything camp could not identify, degrades to a whole-file copy.
func (e Engine) DeltaUsable() bool {
	return e.Kind == KindRsync && e.Protocol >= minDeltaProtocol
}

// Reason explains a non-delta verdict in one operator-facing line. Empty when
// the engine is delta-usable.
func (e Engine) Reason() string {
	switch {
	case e.DeltaUsable():
		return ""
	case e.Kind == KindAbsent:
		return "no rsync found"
	case e.Kind == KindOpenRsync:
		return "openrsync (protocol " + strconv.Itoa(e.Protocol) + "); camp does not use its delta engine"
	case e.Kind == KindUnknown:
		return "unrecognized rsync implementation"
	default:
		return "rsync protocol " + strconv.Itoa(e.Protocol) + " is below " + strconv.Itoa(minDeltaProtocol)
	}
}

// Pair is the probe result for both ends of a transfer.
type Pair struct {
	// Local is this machine's engine.
	Local Engine `json:"local"`
	// Peer is the far machine's engine.
	Peer Engine `json:"peer"`
}

// DeltaUsable reports whether a delta transfer is safe, which needs both ends
// to qualify: rsync negotiates down to the lower protocol, so one weak end
// decides the transfer.
func (p Pair) DeltaUsable() bool {
	return p.Local.DeltaUsable() && p.Peer.DeltaUsable()
}

// Reason names the end that forced a whole-file copy, so the operator knows
// which machine to fix. Empty when a delta transfer is usable.
func (p Pair) Reason() string {
	if p.DeltaUsable() {
		return ""
	}
	if !p.Local.DeltaUsable() {
		return "local: " + p.Local.Reason()
	}
	return "peer: " + p.Peer.Reason()
}

var (
	// genuineRe matches genuine rsync's banner:
	//   rsync  version 3.4.4  protocol version 32
	genuineRe = regexp.MustCompile(`rsync\s+version\s+(\S+)\s+protocol\s+version\s+(\d+)`)
	// protocolRe matches a bare protocol statement, which is how openrsync
	// leads: "openrsync: protocol version 29".
	protocolRe = regexp.MustCompile(`protocol\s+version\s+(\d+)`)
	// compatRe matches openrsync's compatibility claim:
	//   rsync version 2.6.9 compatible
	compatRe = regexp.MustCompile(`rsync\s+version\s+(\S+)\s+compatible`)
)

// ParseVersion identifies an implementation from its --version output. Pure:
// no exec, no filesystem, so the whole persona matrix is a unit test.
//
// The openrsync check runs first and that ordering is load-bearing, not
// stylistic. Apple's openrsync prints:
//
//	openrsync: protocol version 29
//	rsync version 2.6.9 compatible
//
// The second line matches the genuine-rsync shape closely enough that a parser
// which looks for a version anywhere first will label openrsync as rsync and
// hand it work camp only trusts genuine rsync with.
func ParseVersion(binary, out string) Engine {
	e := Engine{Binary: binary, Kind: KindUnknown}
	if strings.TrimSpace(out) == "" {
		return e
	}

	if strings.Contains(strings.ToLower(out), "openrsync") {
		e.Kind = KindOpenRsync
		if m := protocolRe.FindStringSubmatch(out); m != nil {
			e.Protocol, _ = strconv.Atoi(m[1])
		}
		if m := compatRe.FindStringSubmatch(out); m != nil {
			e.Version = m[1]
		}
		return e
	}

	if m := genuineRe.FindStringSubmatch(out); m != nil {
		e.Kind = KindRsync
		e.Version = m[1]
		e.Protocol, _ = strconv.Atoi(m[2])
		return e
	}

	return e
}

// binaryMarker prefixes the resolved path in the probe script's output, so one
// round-trip carries both which binary ran and what it said about itself.
const binaryMarker = "CAMP-RSYNC-BINARY:"

// probeScript resolves rsync the same way the transport will — through the
// shell's own PATH — and reports the resolved path alongside the version
// banner. Absent rsync exits cleanly with no marker rather than failing, so
// "no rsync" arrives as a result instead of an error.
const probeScript = `p=$(command -v rsync 2>/dev/null) || exit 0
[ -n "$p" ] || exit 0
printf '` + binaryMarker + `%s\n' "$p"
"$p" --version 2>&1`

// parseProbeOutput turns the probe script's output into an Engine. Absent
// output, or output with no binary marker, means nothing was found.
func parseProbeOutput(out string) Engine {
	binary := ""
	var banner strings.Builder
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(strings.TrimRight(line, "\r"))
		if strings.HasPrefix(trimmed, binaryMarker) {
			binary = strings.TrimSpace(strings.TrimPrefix(trimmed, binaryMarker))
			continue
		}
		if binary != "" {
			banner.WriteString(line)
			banner.WriteString("\n")
		}
	}
	if binary == "" {
		return Engine{Kind: KindAbsent}
	}
	return ParseVersion(binary, banner.String())
}

// ProbeLocal resolves and interrogates the rsync this machine would execute.
// A missing rsync is an absent engine, not an error: the caller's response is
// to degrade to whole-file, not to abort.
func ProbeLocal(ctx context.Context) (Engine, error) {
	if ctx.Err() != nil {
		return Engine{}, ctx.Err()
	}
	binary, err := exec.LookPath("rsync")
	if err != nil {
		return Engine{Kind: KindAbsent}, nil
	}
	out, err := exec.CommandContext(ctx, binary, "--version").CombinedOutput()
	if err != nil && len(out) == 0 {
		return Engine{Binary: binary, Kind: KindUnknown},
			camperrors.Wrapf(err, "run %s --version", binary)
	}
	return ParseVersion(binary, string(out)), nil
}

// ShellRunner runs a shell script somewhere and returns its stdout. Injected so
// the peer probe is exercised without ssh; *peer.Source satisfies it.
type ShellRunner interface {
	RunShell(ctx context.Context, script string) ([]byte, error)
}

// ProbePeer interrogates the rsync a peer would execute, in one round-trip over
// the connection camp already holds open. An unreachable peer is an error; a
// peer with no rsync is an absent engine.
func ProbePeer(ctx context.Context, src ShellRunner) (Engine, error) {
	if ctx.Err() != nil {
		return Engine{}, ctx.Err()
	}
	if src == nil {
		return Engine{}, camperrors.New("rsync probe requires a peer source")
	}
	out, err := src.RunShell(ctx, probeScript)
	if err != nil {
		return Engine{}, camperrors.Wrap(err, "probe rsync on peer")
	}
	return parseProbeOutput(string(out)), nil
}
