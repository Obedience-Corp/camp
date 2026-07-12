// Package remote holds camp's ssh primitives for reaching campaigns on other
// machines listed in ~/.obey/machines.yaml. It mirrors the festival app's ssh
// construction (src-tauri/src/remote/connection.rs) so the terminal and the app
// reach the same hosts the same way. v1 is agent/key auth only; password-auth
// machines are rejected here (EnsureKeyAuth) rather than prompted.
package remote

import (
	"bytes"
	"context"
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

// authArgs mirrors the app's ssh_auth_args (connection.rs:217-239) for the
// agent/key case: BatchMode=yes (never prompt), plus IdentitiesOnly and -i when
// an identity file is configured. Password auth is rejected upstream by
// EnsureKeyAuth, so its interactive-prompt args are never emitted.
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
// bounded by ctx (and DefaultTimeout if ctx has no earlier deadline). A non-zero
// exit or timeout is a wrapped error carrying the remote stderr.
func Run(ctx context.Context, target string, opts []string, remoteCmd string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()

	args := append(append([]string{}, opts...), target, remoteCmd)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, camperrors.Wrapf(ctx.Err(), "ssh to %s timed out", target)
		}
		return nil, camperrors.Wrapf(err, "ssh to %s: %s", target, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// ResolveRoot runs the remote's OWN `camp switch <remainder> --print` so the
// remote registry, org config, and fuzzy matching decide the campaign root —
// never the local filesystem. The remainder is single-quoted for injection
// safety. The returned path is meaningful only on the far machine.
func ResolveRoot(ctx context.Context, m *machines.Machine, remainder string) (string, error) {
	if err := EnsureKeyAuth(m); err != nil {
		return "", err
	}
	remoteCmd := "camp switch " + ShellQuote(remainder) + " --print"
	out, err := Run(ctx, Target(m), Opts(m), remoteCmd)
	if err != nil {
		return "", camperrors.Wrapf(err, "could not resolve %q on %s", remainder, m.ID)
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", camperrors.New("could not resolve " + remainder + " on " + m.ID + ": remote returned no path")
	}
	return root, nil
}
