// Package remote holds camp's ssh primitives for reaching campaigns on other
// machines listed in ~/.obey/machines.yaml. It mirrors the festival app's ssh
// construction (src-tauri/src/remote/connection.rs) so the terminal and the app
// reach the same hosts the same way. v1 is agent/key auth only; password-auth
// machines are rejected here (EnsureKeyAuth) rather than prompted.
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

// Target returns the ssh destination: user@host when ssh_user is set, else host.
// Mirrors the app's ssh_target (remote/connection.rs:209-214).
func Target(m *machines.Machine) string {
	if m.SSHUser != "" {
		return m.SSHUser + "@" + m.Host
	}
	return m.Host
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
// identity options, not the full ControlMaster multiplex path.
func ProbeCommand(m *machines.Machine) string {
	if m == nil {
		return ""
	}
	parts := []string{"ssh", "-o", "BatchMode=yes"}
	if m.IdentityFile != "" {
		parts = append(parts, "-o", "IdentitiesOnly=yes", "-i", expandTilde(m.IdentityFile))
	}
	parts = append(parts, Target(m), "true")
	return strings.Join(parts, " ")
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
	opts := []string{
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ConnectTimeout=8",
		"-o", "ControlMaster=auto",
		"-o", "ControlPath=" + controlPath(m),
		"-o", "ControlPersist=30s",
	}
	return append(opts, authArgs(m)...)
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
		return rest, true
	}
	if end == 0 {
		return "", false
	}
	return rest[:end], true
}

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

// HopFailureDetail returns the best operator-facing classification for a hop
// error: Tailscale check-mode, host-key mismatch (H10), or publickey denial.
// Empty means callers should fall back to generic formatting.
func HopFailureDetail(err error) string {
	if err == nil {
		return ""
	}
	return classifySSHFailure(errText(err))
}

// HostKeyMismatchDetail is true when err is an H10 host-key verification failure.
func HostKeyMismatchDetail(err error) string {
	if err == nil {
		return ""
	}
	if !isHostKeyMismatch(errText(err)) {
		return ""
	}
	return formatHostKeyMismatch(errText(err))
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

// RemoteCampPathEnv, when set, is the exact camp invocation used on the far
// machine in place of the bare name "camp" resolved off the login shell's
// PATH. It is the escape hatch for a machine where camp is installed
// somewhere no profile script exports onto PATH, and is documented on
// `camp switch --help` and `camp list --help`.
const RemoteCampPathEnv = "CAMP_REMOTE_CAMP_PATH"

// remoteCampBinary returns the argv[0] token for a remote camp invocation:
// CAMP_REMOTE_CAMP_PATH, shell-quoted, when set; otherwise the bare name
// "camp" for the remote login shell to resolve off its own PATH.
func remoteCampBinary() string {
	if p := os.Getenv(RemoteCampPathEnv); p != "" {
		return ShellQuote(p)
	}
	return "camp"
}

// RunCampCommand execs the remote machine's OWN camp binary over ssh, through
// a POSIX login shell (`sh -lc`), and returns stdout. args is everything
// after the binary name, e.g. `switch 'foo' --print` or `list --json`.
//
// A bare non-interactive `ssh host 'camp ...'` runs under ssh's own
// non-login shell, which never sources a login profile (~/.profile,
// ~/.bash_profile, etc.) — so a camp installed via a PATH addition that only
// a login shell picks up (~/.local/bin, asdf, a shell-managed version
// manager) was invisible to it. `sh -lc` forces the remote's POSIX shell to
// run as a login shell first, then execute the command, so it sees the same
// PATH an interactive ssh session would. sh is used rather than assuming
// bash/zsh because POSIX guarantees /bin/sh exists; the user's actual login
// shell is whatever their own account is configured to run.
func RunCampCommand(ctx context.Context, m *machines.Machine, args string) ([]byte, error) {
	if err := EnsureKeyAuth(m); err != nil {
		return nil, err
	}
	binary := remoteCampBinary()
	out, err := Run(ctx, Target(m), Opts(m), campRemoteCommandLine(binary, args))
	if err != nil {
		return nil, campNotFoundHint(err, m, binary)
	}
	return out, nil
}

// campRemoteCommandLine builds the single token handed to ssh as the remote
// command for a camp invocation: binary+args wrapped in a POSIX login shell
// (`sh -lc '<binary> <args>'`). Kept as a pure function of its inputs (no ssh,
// no machine lookup) so the exact wrapping and quoting is unit-testable
// without a live ssh connection.
func campRemoteCommandLine(binary, args string) string {
	return "sh -lc " + ShellQuote(binary+" "+args)
}

// campNotFoundHint appends the login-shell context and the
// CAMP_REMOTE_CAMP_PATH escape hatch to err, but only when the failure
// carries exit code 127 — the POSIX convention every common shell (sh, bash,
// dash, ash/busybox, zsh) uses exclusively for "command not found". That
// narrow, explicit signal (rather than matching stderr text) matters here:
// camp's own domain errors can legitimately contain the words "not found"
// too (e.g. ErrCampaignNotFound, when the campaign name just does not
// resolve on a far machine whose camp binary ran fine), and mislabeling
// those as a missing binary would send the user chasing PATH for nothing.
// Any exit code other than 127 is returned unchanged.
func campNotFoundHint(err error, m *machines.Machine, binary string) error {
	var cmdErr *camperrors.CommandError
	if !errors.As(err, &cmdErr) || cmdErr.ExitCode != 127 {
		return err
	}
	return camperrors.Wrapf(err,
		"remote camp not found on %s (tried %q via sh -lc, i.e. the machine's login-shell PATH); "+
			"if camp lives outside that PATH, set %s to its exact path on that machine",
		m.ID, binary, RemoteCampPathEnv)
}

// resolveRootArgs builds the `switch` args RunCampCommand appends after the
// resolved camp binary. remainder is single-quoted for injection safety.
// Split out from ResolveRoot so it is unit-testable without ssh.
func resolveRootArgs(remainder string) string {
	return "switch " + ShellQuote(remainder) + " --print"
}

// ResolveRoot runs the remote's OWN `camp switch <remainder> --print` so the
// remote registry, org config, and fuzzy matching decide the campaign root —
// never the local filesystem. The remainder is single-quoted for injection
// safety. The returned path is meaningful only on the far machine.
func ResolveRoot(ctx context.Context, m *machines.Machine, remainder string) (string, error) {
	out, err := RunCampCommand(ctx, m, resolveRootArgs(remainder))
	if err != nil {
		return "", camperrors.Wrapf(err, "could not resolve %q on %s", remainder, m.ID)
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", camperrors.New("could not resolve " + remainder + " on " + m.ID + ": remote returned no path")
	}
	return root, nil
}
