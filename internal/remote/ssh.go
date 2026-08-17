// Package remote holds camp's ssh primitives for reaching campaigns on other
// machines listed in ~/.obey/machines.yaml. It mirrors the festival app's ssh
// OPTION construction (src-tauri/src/remote/connection.rs) so the terminal and
// the app build hops the same way. Address RESOLUTION is deliberately not part
// of that mirror: camp falls back to the tailnet peer table when a MagicDNS
// name stops resolving (endpoint.go, design WI-feedca), which the app does not
// yet do. v1 is agent/key auth only; password-auth machines are rejected here
// (EnsureKeyAuth) rather than prompted.
package remote

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/machines"
)

// DefaultTimeout bounds a single remote ssh operation. camp picks its own bound;
// the app uses DEFAULT_TIMEOUT (cli/executor) as the reference order of magnitude.
const DefaultTimeout = 10 * time.Second

// Target returns the ssh destination for a direct (non-fallback) dial:
// user@host when ssh_user is set, else host. Mirrors the app's ssh_target
// (remote/connection.rs:209-214). Dial paths that should survive a MagicDNS
// outage go through ResolveEndpoint instead (endpoint.go).
func Target(m *machines.Machine) string {
	return Direct(m).Target()
}

// authArgs builds OpenSSH auth-related options for a hop. Per the dual-auth
// contract (design WI-ca06e1 D1.2/D3): BatchMode stays on for both OpenSSH and
// Tailscale SSH so agents never hang on interactive prompts. Client argv may
// legitimately converge across auth methods — Tailscale SSH authenticates
// server-side; distinct product behavior is prerequisites + error
// classification (see classifySSHFailure), not artificial flag divergence.
//
// Identity handling applies when identity_file is set (typical for
// ssh-agent). Password auth is rejected upstream by EnsureKeyAuth.
//
// The identity path is tilde-expanded here: OpenSSH's IdentityFile config
// directive expands a leading ~, but whether ssh expands ~ in a -i argument is
// client- and platform-dependent, so camp resolves it to an absolute path
// itself. That makes `camp machine add --identity '~/.ssh/id'` behave the way
// users expect from ssh_config regardless of the ssh build, and keeps the path
// camp hands off unambiguous.
func authArgs(m *machines.Machine) []string {
	args := []string{"-o", "BatchMode=yes"}
	if m.IdentityFile != "" {
		args = append(args, "-o", "IdentitiesOnly=yes", "-i", expandTilde(m.IdentityFile))
	}
	return args
}

// AuthDisplayName is the operator-facing label for a machines.yaml auth_method
// (D7). Wire values stay in the file; this is display and diagnose only.
func AuthDisplayName(auth string) string {
	switch auth {
	case machines.AuthSSHAgent:
		return "OpenSSH (keys / agent)"
	case machines.AuthTailscaleSSH:
		return "Tailscale SSH (identity)"
	case machines.AuthSSHPassword:
		return "password (not supported for terminal hop)"
	default:
		if auth == "" {
			return "OpenSSH (keys / agent)"
		}
		return auth
	}
}

// ProbeCommand returns a copy-paste BatchMode ssh line the operator can run
// outside camp to isolate hop failures (D7). It mirrors camp's target and
// identity options, not the full ControlMaster multiplex path. This is the
// direct-dial form; surfaces that resolved a fallback endpoint use
// Endpoint.ProbeCommand so the pasted line reproduces the real dial.
func ProbeCommand(m *machines.Machine) string {
	if m == nil {
		return ""
	}
	return Direct(m).ProbeCommand()
}

// AuthModeHint returns an optional one-line diagnose note for the machine's
// auth_method (D7). Empty when no extra guidance is needed.
func AuthModeHint(m *machines.Machine) string {
	if m == nil {
		return ""
	}
	switch m.AuthMethod {
	case machines.AuthTailscaleSSH:
		return "Tailscale SSH: if hop fails, look for a login.tailscale.com check URL — approve in a browser, then retry (camp cannot complete check-mode under BatchMode)"
	case machines.AuthSSHAgent:
		if m.IdentityFile == "" {
			return "OpenSSH: ensure ssh-agent has keys (`ssh-add -l`) or set identity_file; Tailnet reachability alone is not login"
		}
		return "OpenSSH: hop uses identity_file / agent; peer must accept publickey (not Tailscale check-mode alone)"
	case machines.AuthSSHPassword:
		return "password auth is not supported for terminal hop; re-add with ssh-agent or tailscale-ssh"
	default:
		return ""
	}
}

// FormatHopFailure returns the best operator-facing hop error, optionally
// prefixed with the machine's auth mode so diagnose / list / TUI share one
// classification path (design 04 §B).
func FormatHopFailure(err error, m *machines.Machine) string {
	if err == nil {
		return ""
	}
	detail := HopFailureDetail(err)
	if detail == "" {
		return ""
	}
	if m == nil || m.AuthMethod == "" {
		return detail
	}
	// Avoid double-prefixing if the message already names the mode.
	label := AuthDisplayName(m.AuthMethod)
	if strings.Contains(detail, label) || strings.Contains(detail, m.AuthMethod) {
		return detail
	}
	return label + ": " + detail
}

// expandTilde resolves a leading ~ or ~/ to the current user's home directory,
// matching OpenSSH's IdentityFile expansion. A ~otheruser/ prefix is left
// untouched for ssh to resolve, and a path with no leading tilde (including one
// where ~ appears mid-path) is returned unchanged. If the home directory cannot
// be determined, the original path is returned so ssh still sees something.
func expandTilde(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return path
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[len("~/"):])
		}
	}
	return path
}

// Opts returns the ssh option args (excluding the target) for a command on m.
// ControlMaster multiplexing means the resolve step (ResolveRoot's
// `camp switch --print`) and the interactive hop share ONE connection — one auth,
// one handshake — because both build opts from the same per-machine ControlPath.
// Conceptually mirrors the app's ssh_base_args (connection.rs:241-255); host
// details beyond the machine's identity_file are left to the user's ~/.ssh/config.
func Opts(m *machines.Machine) []string {
	opts := append(baseOpts(),
		"-o", "ControlMaster=auto",
		"-o", "ControlPath="+controlPath(m),
		"-o", "ControlPersist=30s",
	)
	return append(opts, authArgs(m)...)
}

// OptsReuseOnly returns ssh options for a hop that must not change m's
// ControlMaster socket. ControlMaster=no still reuses a live master when one
// exists (one auth, no extra handshake), but it never opens a master, and —
// unlike ControlMaster=auto — never unlinks a stale socket to replace it with a
// fresh one. That last part is why this exists: any surface that reports socket
// state must hop this way, or its own probe heals the stale socket it is
// reporting, and the operator is told to reset a socket that no longer exists.
// ControlPersist is omitted because nothing here creates a master to persist,
// and the path comes from ControlSocketPath (not controlPath) so a read-only
// diagnostic does not create ~/.obey/ssh-ctl as a side effect.
func OptsReuseOnly(m *machines.Machine) []string {
	opts := append(baseOpts(),
		"-o", "ControlMaster=no",
		"-o", "ControlPath="+ControlSocketPath(m),
	)
	return append(opts, authArgs(m)...)
}

// baseOpts returns the ssh options shared by every camp hop, excluding the
// ControlMaster settings (which differ by hop kind) and auth args.
func baseOpts() []string {
	return []string{
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ConnectTimeout=8",
	}
}

// controlDir is ~/.obey/ssh-ctl, the directory holding one ControlMaster socket
// per machine.
func controlDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".obey", "ssh-ctl")
}

// ControlSocketPath returns the per-machine ssh ControlMaster socket path under
// ~/.obey/ssh-ctl without creating anything. A short, per-id name keeps the path
// under the OS socket-length limit for typical home directories. Exposed so
// diagnostics can inspect and clear a machine's multiplex socket.
func ControlSocketPath(m *machines.Machine) string {
	return filepath.Join(controlDir(), m.ID+".sock")
}

// controlPath returns the machine's ControlMaster socket path and
// best-effort-creates ~/.obey/ssh-ctl so ControlMaster=auto can bind the socket.
func controlPath(m *machines.Machine) string {
	_ = os.MkdirAll(controlDir(), 0o700)
	return ControlSocketPath(m)
}

// ControlMasterState describes a machine's ssh ControlMaster multiplex socket.
type ControlMasterState string

const (
	// ControlNone means no socket file exists — the next hop opens a fresh master.
	ControlNone ControlMasterState = "none"
	// ControlLive means the socket exists and its master answers `ssh -O check`.
	ControlLive ControlMasterState = "live"
	// ControlStale means the socket exists but the master no longer answers —
	// the state a sleep/network flap leaves behind, which can hang later hops
	// until ControlPersist expires or the socket is removed.
	ControlStale ControlMasterState = "stale"
)

// SocketDiagnosis is one machine's ControlMaster socket status.
type SocketDiagnosis struct {
	MachineID string             `json:"machine_id"`
	Socket    string             `json:"socket"`
	State     ControlMasterState `json:"state"`
}

// controlProbeTimeout bounds the local `ssh -O check`/`-O exit` control
// operations. They talk only to the local multiplex socket, so they are fast or
// they are hung; either way we do not wait long.
const controlProbeTimeout = 3 * time.Second

// CheckControlMaster reports the state of m's ControlMaster socket. A missing
// socket is ControlNone; a present socket is probed with `ssh -O check` and
// classified ControlLive or ControlStale. It never opens a new connection.
func CheckControlMaster(ctx context.Context, m *machines.Machine) SocketDiagnosis {
	d := SocketDiagnosis{MachineID: m.ID, Socket: ControlSocketPath(m), State: ControlNone}
	fi, err := os.Stat(d.Socket)
	if err != nil || fi.Mode()&os.ModeSocket == 0 {
		return d
	}
	if controlMasterAlive(ctx, m) {
		d.State = ControlLive
	} else {
		d.State = ControlStale
	}
	return d
}

// controlMasterAlive returns true when `ssh -O check` reports a running master
// for m. It uses the same per-machine opts (and thus ControlPath) as a real hop,
// and does not fall back to opening a connection.
func controlMasterAlive(ctx context.Context, m *machines.Machine) bool {
	ctx, cancel := context.WithTimeout(ctx, controlProbeTimeout)
	defer cancel()
	args := append(append([]string{}, Opts(m)...), "-O", "check", Target(m))
	return exec.CommandContext(ctx, "ssh", args...).Run() == nil
}

// ResetControlMaster tears down m's ControlMaster socket: it asks the master to
// exit (`ssh -O exit`, best effort — a stale master will not answer) and then
// removes the socket file so a stuck socket cannot hang the next hop. A machine
// with no socket is a no-op. It returns an error only if the socket file is
// present and cannot be removed.
func ResetControlMaster(ctx context.Context, m *machines.Machine) error {
	socket := ControlSocketPath(m)
	if _, err := os.Stat(socket); err != nil {
		return nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, controlProbeTimeout)
	defer cancel()
	args := append(append([]string{}, Opts(m)...), "-O", "exit", Target(m))
	_ = exec.CommandContext(probeCtx, "ssh", args...).Run()
	if err := os.Remove(socket); err != nil && !os.IsNotExist(err) {
		return camperrors.Wrapf(err, "remove control socket %s", socket)
	}
	return nil
}

// EnsureKeyAuth rejects password-auth machines: v1 terminal switch/list is
// agent/key only. Callers guard before attempting any hop.
func EnsureKeyAuth(m *machines.Machine) error {
	if m.AuthMethod == machines.AuthSSHPassword {
		return camperrors.New("machine " + m.ID +
			" uses password auth; configure key auth (ssh-agent or an identity file) for terminal switch")
	}
	return nil
}

// ShellQuote single-quotes s for safe interpolation into a remote shell command,
// mirroring the app's shell_single_quote (commands/remote.rs).
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Run execs `ssh <opts> <target> <remoteCmd>` and returns stdout. The call is
// bounded by ctx (and DefaultTimeout if ctx has no earlier deadline). A
// timeout is a wrapped context error that still carries any captured stderr
// (so a Tailscale SSH check URL is not discarded when the hop times out
// waiting for browser approval); a non-zero exit is a *camperrors.CommandError
// carrying the exit code and trimmed remote stderr, so callers can
// distinguish failure shapes (e.g. RunCampCommand's "binary not found"
// detection) without re-parsing the error string.
func Run(ctx context.Context, target string, opts []string, remoteCmd string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()

	args := append(append([]string{}, opts...), target, remoteCmd)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		trimmed := strings.TrimSpace(stderr.String())
		if ctx.Err() != nil {
			return nil, sshTimeoutError(target, trimmed, ctx.Err())
		}
		exitCode := 0
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
		return nil, sshExitError(target, exitCode, trimmed, err)
	}
	return stdout.Bytes(), nil
}

// tailscaleCheckMarker is the distinctive first line Tailscale SSH prints when
// check mode requires a human browser approval before the hop can proceed.
const tailscaleCheckMarker = "Tailscale SSH requires an additional check"

// ParseTailscaleCheckURL extracts the login.tailscale.com check URL from ssh
// stderr (or any text that embeds it). Returns false when the marker or URL is
// absent. Tailscale prints:
//
//	# Tailscale SSH requires an additional check.
//	# To authenticate, visit: https://login.tailscale.com/a/...
//
// camp runs with BatchMode=yes and cannot complete that browser step; callers
// surface the URL so the operator can approve once and retry. Also accepts
// camp's own annotated wording so re-parsing a already-classified error still
// yields the URL.
func ParseTailscaleCheckURL(text string) (string, bool) {
	if text == "" {
		return "", false
	}
	hasMarker := strings.Contains(text, tailscaleCheckMarker) ||
		strings.Contains(text, "Tailscale SSH requires a one-time browser check")
	if !hasMarker {
		return "", false
	}
	const prefix = "https://login.tailscale.com/"
	idx := strings.Index(text, prefix)
	if idx < 0 {
		return "", false
	}
	rest := text[idx:]
	end := strings.IndexAny(rest, " \t\r\n\"'")
	if end < 0 {
		end = len(rest)
	}
	url := strings.TrimRight(rest[:end], urlTrailingPunctuation)
	if url == "" {
		return "", false
	}
	return url, true
}

// urlTrailingPunctuation is punctuation that can follow a URL in prose but is
// never part of a Tailscale check URL. Trimming it is what makes this function
// idempotent, and idempotence is the whole point: camp formats the URL into a
// sentence ("open <url>, approve, then retry"), and every display surface
// re-parses that sentence rather than the raw ssh stderr. Without the trim the
// second pass captured the comma, so the operator was shown — and would copy —
// a 404.
const urlTrailingPunctuation = ",.;:!?)]}>"

// TailscaleCheckDetail returns a single-line, actionable explanation when err
// (or its chain) is a Tailscale SSH check-mode failure. Empty string means the
// failure is something else and callers should use their generic formatting.
func TailscaleCheckDetail(err error) string {
	if err == nil {
		return ""
	}
	if url, ok := ParseTailscaleCheckURL(errText(err)); ok {
		return formatTailscaleCheckDetail(url)
	}
	return ""
}

// TailscaleCheckURL returns the login.tailscale.com approval URL carried by err
// (or anything in its chain), or "" when err is not a check-mode failure.
//
// The URL is the only actionable thing about this failure, so callers get it as
// a value rather than having to find it inside TailscaleCheckDetail's sentence.
// That is what lets a screen put it on its own line, and lets a key open it.
func TailscaleCheckURL(err error) string {
	if err == nil {
		return ""
	}
	url, _ := ParseTailscaleCheckURL(errText(err))
	return url
}

// HopFailureDetail returns the best operator-facing classification for a hop
// error: Tailscale check-mode, host-key mismatch (H10), or publickey denial.
// Empty means callers should fall back to generic formatting.
func HopFailureDetail(err error) string {
	if err == nil {
		return ""
	}
	return classifySSHFailure(errText(err))
}

func errText(err error) string {
	var cmdErr *camperrors.CommandError
	if errors.As(err, &cmdErr) && cmdErr.Stderr != "" {
		return cmdErr.Stderr + "\n" + err.Error()
	}
	return err.Error()
}

// classifySSHFailure maps ssh stderr (or annotated error text) to a single
// actionable line. Order: check-mode (must win over timeout noise), host-key
// mismatch (never report as auth failure), publickey permission denied.
func classifySSHFailure(text string) string {
	if text == "" {
		return ""
	}
	if url, ok := ParseTailscaleCheckURL(text); ok {
		return formatTailscaleCheckDetail(url)
	}
	if isHostKeyMismatch(text) {
		return formatHostKeyMismatch(text)
	}
	if isPermissionDenied(text) {
		return formatPermissionDenied()
	}
	return ""
}

func formatTailscaleCheckDetail(url string) string {
	return "Tailscale SSH requires a one-time browser check — open " + url +
		", approve, then retry (camp cannot complete this interactively)"
}

func isHostKeyMismatch(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(text, "REMOTE HOST IDENTIFICATION HAS CHANGED") ||
		strings.Contains(text, "Host key verification failed") ||
		strings.Contains(lower, "host key mismatch")
}

func formatHostKeyMismatch(text string) string {
	host := hostFromKnownHostsMessage(text)
	if host != "" {
		return "SSH host key mismatch for " + host +
			" — remove the stale known_hosts entry with `ssh-keygen -R " + host +
			"` and retry (not an auth failure; common after reinstall or flipping Tailscale SSH vs sshd)"
	}
	return "SSH host key mismatch — remove the stale known_hosts entry with `ssh-keygen -R <host>` and retry (not an auth failure; common after reinstall or flipping Tailscale SSH vs sshd)"
}

// hostFromKnownHostsMessage extracts a host token from OpenSSH's changed-key
// banner when present ("Host key for X has changed").
func hostFromKnownHostsMessage(text string) string {
	const marker = "Host key for "
	if idx := strings.Index(text, marker); idx >= 0 {
		rest := text[idx+len(marker):]
		if end := strings.IndexAny(rest, " \t\r\n"); end > 0 {
			return rest[:end]
		}
	}
	return ""
}

func isPermissionDenied(text string) bool {
	return strings.Contains(strings.ToLower(text), "permission denied")
}

func formatPermissionDenied() string {
	return "SSH permission denied (publickey) — check ssh-agent keys (`ssh-add -l`), identity_file, remote authorized_keys, and ssh_user; for Tailscale SSH use auth_method=tailscale-ssh and complete any check URL"
}

// sshTimeoutError keeps stderr on the timeout path. The previous behaviour
// returned only "ssh to X timed out", which hid the Tailscale check URL that
// ssh had already printed while waiting for browser approval.
func sshTimeoutError(target, stderr string, err error) error {
	if detail := classifySSHFailure(stderr); detail != "" {
		// Wrap preserves errors.Is(err, context.DeadlineExceeded) while
		// putting the classified cause first so connectionFailureDetail / %v show it.
		return camperrors.Wrapf(err, "%s (while connecting to %s)", detail, target)
	}
	if stderr != "" {
		return camperrors.Wrapf(err, "ssh to %s timed out: %s", target, compactSSHStderr(stderr))
	}
	return camperrors.Wrapf(err, "ssh to %s timed out", target)
}

// sshExitError annotates non-zero ssh exits. Tailscale check mode sometimes
// exits (instead of hanging until our deadline) with the same marker+URL in
// stderr; prefer classified messages over the raw multi-line banner.
func sshExitError(target string, exitCode int, stderr string, err error) error {
	if detail := classifySSHFailure(stderr); detail != "" {
		return camperrors.NewCommand("ssh "+target, exitCode, detail, err)
	}
	return camperrors.NewCommand("ssh "+target, exitCode, stderr, err)
}

// compactSSHStderr collapses multi-line ssh noise into one short detail for
// timeout messages. Skips empty and #-comment lines (Tailscale banners use
// those) so the first real failure line wins when present.
func compactSSHStderr(stderr string) string {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return ""
	}
	if !strings.Contains(stderr, "\n") {
		return stderr
	}
	var firstComment string
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			if firstComment == "" {
				firstComment = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "# "), "#"))
			}
			continue
		}
		return line
	}
	if firstComment != "" {
		return firstComment
	}
	return strings.ReplaceAll(stderr, "\n", " ")
}

// LoginShellCommand wraps script in the remote account's configured login shell
// (`exec "$SHELL" -lc '<script>'`) for execution as ssh's remote command.
// OpenSSH sets SHELL from the account record before invoking a remote command,
// so this selects zsh/bash/fish according to the account rather than according
// to camp's assumptions. Every remote invocation goes through this function so
// shell selection and quoting have one owner. Pure function of its input: no
// ssh, no machine lookup, unit-testable.
func LoginShellCommand(script string) string {
	return `exec "$SHELL" -lc ` + ShellQuote(script)
}
