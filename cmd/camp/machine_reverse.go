package main

import (
	"context"
	"net"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// reverseProbeTimeout bounds the local reachability checks. They are local
// (a loopback dial, one CLI call), so this only guards a wedged binary.
const reverseProbeTimeout = 2 * time.Second

// reverseReport is what diagnose can honestly say about whether OTHER machines
// can reach back to this one.
type reverseReport struct {
	Capable bool
	Hint    string
}

// reverseProbes are the seams the cause matrix is tested through.
type reverseProbes struct {
	SSHDListening func(ctx context.Context) bool
	RemoteLoginOn func(ctx context.Context) (bool, bool) // (on, knowable)
	TailscaleUp   func(ctx context.Context) bool
	GOOS          string
}

// checkReverseReachability reports what this machine can know about being
// reachable FROM another machine.
//
// The honest framing matters, and it is why this reports "acceptability" rather
// than "reachability". Whether archdtop can actually open a connection to this
// mac depends on archdtop's client config, the tailnet ACL, and this machine's
// sshd, and camp can only observe the last one without executing on the far
// side. So it reports the half it can see and labels it, instead of claiming a
// verdict it has not earned. D003 also forbids fixing any of it: every hint
// names one action for the operator to take, and camp never takes it.
func checkReverseReachability(ctx context.Context, p reverseProbes) reverseReport {
	ctx, cancel := context.WithTimeout(ctx, reverseProbeTimeout)
	defer cancel()

	goos := p.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}

	listening := p.SSHDListening != nil && p.SSHDListening(ctx)
	if !listening {
		if goos == "darwin" {
			on, knowable := remoteLoginState(ctx, p)
			switch {
			case knowable && !on:
				// Softened absolute claim: classic sshd is off, but Tailscale SSH
				// can still accept on the tailnet IP without system Remote Login.
				return reverseReport{Hint: "macOS Remote Login is off (system sshd will not " +
					"accept on :22); enable it in System Settings > General > Sharing > Remote Login " +
					"for classic ssh hops. Tailscale SSH, if enabled separately, may still work"}
			case knowable && on:
				// Remote Login already on but nothing answers: do not tell the
				// operator to enable it again — point at sshd/port/firewall.
				return reverseReport{Hint: "Remote Login reports on but nothing is accepting on " +
					"port 22; check that sshd is running, listening on the expected port, and not " +
					"blocked by a local firewall"}
			default:
				// State unreadable: never claim it is off. Point at the settings
				// surface without asserting the current value.
				return reverseReport{Hint: "no sshd is listening on this machine; check " +
					"System Settings > General > Sharing > Remote Login and that sshd is " +
					"accepting on port 22 so other machines can hop here via classic ssh"}
			}
		}
		return reverseReport{Hint: "no sshd is listening on this machine; start it " +
			"(for example 'sudo systemctl enable --now sshd') so other machines can hop here " +
			"via classic ssh (Tailscale SSH, if enabled, may still work without system sshd)"}
	}

	// sshd answers, so the local half is satisfied. Everything left is policy on
	// the other side, which is exactly the part camp must not manage.
	// Capable stays true (local sshd half); the hint names the remaining local
	// problem so operators do not treat the green mark as "reverse is fine".
	if p.TailscaleUp != nil && !p.TailscaleUp(ctx) {
		return reverseReport{Capable: true, Hint: "sshd is listening, but tailscale is not up here, " +
			"so tailnet peers cannot route to this machine; run 'tailscale up'"}
	}
	return reverseReport{Capable: true, Hint: "sshd is listening here. Whether a given machine may " +
		"connect is that machine's keys plus your tailnet ACL, which camp does not manage"}
}

func remoteLoginState(ctx context.Context, p reverseProbes) (on, knowable bool) {
	if p.RemoteLoginOn == nil {
		return false, false
	}
	return p.RemoteLoginOn(ctx)
}

// sshdListening dials the loopback ssh port. Deliberately non-privileged and
// deliberately not a process scan: what matters is whether something is
// accepting connections, which is the question a peer's ssh client asks.
func sshdListening(ctx context.Context) bool {
	d := net.Dialer{Timeout: time.Second}
	conn, err := d.DialContext(ctx, "tcp", "127.0.0.1:22")
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// remoteLoginLine matches the English systemsetup line, requiring a word-boundary
// state so "Off" is not a false positive for substring "on".
var remoteLoginLine = regexp.MustCompile(`(?i)remote\s+login:\s*(on|off)\s*$`)

// remoteLoginOn reads macOS Remote Login state. `systemsetup -getremotelogin`
// needs no privileges for a read, and a failure reports "not knowable" rather
// than "off", so an unreadable state never produces a confident wrong hint.
// Parsing is line-based and exact on On/Off rather than a bare Contains("on").
func remoteLoginOn(ctx context.Context) (bool, bool) {
	out, err := exec.CommandContext(ctx, "systemsetup", "-getremotelogin").Output()
	if err != nil {
		return false, false
	}
	return parseRemoteLoginOutput(string(out))
}

// parseRemoteLoginOutput is the pure half of remoteLoginOn, unit-tested with
// fixture strings so localized or garbage success output stays unknowable.
func parseRemoteLoginOutput(out string) (on, knowable bool) {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		m := remoteLoginLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		return strings.EqualFold(m[1], "on"), true
	}
	return false, false
}

// tailscaleUpLocally reports whether this machine's tailscale backend is
// running, reusing the Self parse the origin payload already relies on.
func tailscaleUpLocally(ctx context.Context) bool {
	out, err := runTailscaleStatusForSelf(ctx)
	if err != nil {
		return false
	}
	return selfDNSName(out) != ""
}

// defaultReverseProbes wires the production checks.
func defaultReverseProbes() reverseProbes {
	return reverseProbes{
		SSHDListening: sshdListening,
		RemoteLoginOn: remoteLoginOn,
		TailscaleUp:   tailscaleUpLocally,
	}
}
