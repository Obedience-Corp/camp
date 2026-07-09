// Package remote holds camp's ssh primitives for reaching campaigns on other
// machines listed in ~/.obey/machines.yaml. It mirrors the festival app's ssh
// construction (src-tauri/src/remote/connection.rs) so the terminal and the app
// reach the same hosts the same way. v1 is agent/key auth only; password-auth
// machines are rejected here (EnsureKeyAuth) rather than prompted.
package remote

import (
	"bytes"
	"context"
	"os/exec"
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
func authArgs(m *machines.Machine) []string {
	args := []string{"-o", "BatchMode=yes"}
	if m.IdentityFile != "" {
		args = append(args, "-o", "IdentitiesOnly=yes", "-i", m.IdentityFile)
	}
	return args
}

// Opts returns the ssh option args (excluding the target) for a bounded,
// non-interactive command on m. ControlMaster multiplexing is layered on in
// sequence 02; this is the single-connection form.
func Opts(m *machines.Machine) []string {
	opts := []string{"-o", "StrictHostKeyChecking=accept-new"}
	return append(opts, authArgs(m)...)
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
