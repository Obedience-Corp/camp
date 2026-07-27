package transfer

import (
	"context"
	"os"
	"os/exec"
	"path"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/machines"
	"github.com/Obedience-Corp/camp/internal/peer"
	"github.com/Obedience-Corp/camp/internal/remote"
)

// rsyncStallTimeout aborts a transfer that has stopped making progress. It is
// deliberately not a wall-clock deadline: a size-scaled bound needs the source
// size, which for a pull lives on the far machine, and any rate constant turns a
// slow link into a spurious failure on a transfer that was working. Stall
// detection answers the question a timeout is actually asking.
const rsyncStallTimeout = "60"

// remoteMissingExit is rsync's protocol-error code. Paired with a
// connection-closed message and zero bytes transferred, it is the signature of
// "the far side has no rsync", which is the only condition that triggers the scp
// fallback.
const remoteMissingExit = 12

// Runner executes a copy. Injected so the fallback and the argv are testable
// without moving bytes between machines.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// execRunner is the production Runner.
func execRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// lookPathFunc is injected so the scp fallback can be exercised on a machine
// that has rsync, which both fleet members do.
type lookPathFunc func(string) (string, error)

// CopyOptions describes one cross-machine copy.
type CopyOptions struct {
	Machine  *machines.Machine
	Endpoint Endpoint // the remote side
	Local    string   // absolute local path
	Pull     bool     // true: remote to local; false: local to remote
	Force    bool     // overwrite an existing destination
	Run      Runner
	LookPath lookPathFunc
}

// CopyRemote moves one file between this machine and Machine, reusing the same
// ssh option set as a hop so there is no second auth surface to keep in sync.
//
// rsync first, scp as a fallback. Neither is probed for on the far side: an
// earlier design folded a probe into the resolve round-trip, which would have
// corrupted every resolved path because ResolveRoot treats all of stdout as the
// path. Remote absence is detected from the failed attempt instead, which is
// also free in the common case.
func CopyRemote(ctx context.Context, opts CopyOptions) error {
	if opts.Run == nil {
		opts.Run = execRunner
	}
	if opts.LookPath == nil {
		opts.LookPath = exec.LookPath
	}
	src, dest, err := opts.endpoints(ctx)
	if err != nil {
		return err
	}
	return copyWithFallback(ctx, opts, src, dest)
}

// copyWithFallback is the copy step with the remote resolution already done, so
// the fallback rule is testable without ssh.
func copyWithFallback(ctx context.Context, opts CopyOptions, src, dest string) error {
	if opts.Run == nil {
		opts.Run = execRunner
	}
	if opts.LookPath == nil {
		opts.LookPath = exec.LookPath
	}
	if _, err := opts.LookPath("rsync"); err == nil {
		out, runErr := opts.Run(ctx, "rsync", opts.rsyncArgs(src, dest)...)
		if runErr == nil {
			if !opts.Force && !rsyncCopied(out) {
				return ErrDestinationExists
			}
			return nil
		}
		if !remoteLacksRsync(runErr, out) {
			return camperrors.Wrapf(runErr, "%s %s: %s", opts.direction(), opts.Machine.ID, trimOutput(out))
		}
		// Safe to retry: rsync never started a data stream, so nothing was
		// written on either side.
	}

	if _, err := opts.LookPath("scp"); err != nil {
		return camperrors.New("rsync not found on " + opts.Machine.ID + " and scp not found locally; " +
			"install rsync on both machines, or scp on this one")
	}

	// scp has no portable no-clobber flag, so the guard rsync gets from
	// --ignore-existing has to be a separate look. Without this the fallback
	// silently overwrote the destination and still reported "Transferred",
	// which is the opposite of what the command promises without --force.
	//
	// This is a check-then-copy, not an atomic claim: it establishes the
	// operator's consent, not exclusion against a concurrent writer.
	if !opts.Force {
		exists, err := opts.destinationExists(ctx, dest)
		if err != nil {
			return err
		}
		if exists {
			return ErrDestinationExists
		}
	}

	out, runErr := opts.Run(ctx, "scp", opts.scpArgs(src, dest)...)
	if runErr != nil {
		return camperrors.Wrapf(runErr, "%s %s: %s", opts.direction(), opts.Machine.ID, trimOutput(out))
	}
	return nil
}

// endpoints resolves the remote campaign root through the remote's OWN camp, so
// the far registry decides the path and this machine never computes it.
func (o CopyOptions) endpoints(ctx context.Context) (src, dest string, err error) {
	root, err := remote.ResolveRoot(ctx, o.Machine, o.Endpoint.Campaign)
	if err != nil {
		return "", "", err
	}
	remotePath := path.Join(root, o.Endpoint.Path)
	target := remote.Target(o.Machine) + ":" + remotePath
	if o.Pull {
		return target, o.Local, nil
	}
	return o.Local, target, nil
}

func (o CopyOptions) direction() string {
	if o.Pull {
		return "pull from"
	}
	return "push to"
}

// rsyncArgs mirrors the artifact-pull argv minus the pieces that exist for a
// no-clobber directory sync: no --compare-dest, no exclude file, no staging
// tree. -s sends paths through the rsync protocol so spaces and metacharacters
// do not depend on remote-shell parsing.
func (o CopyOptions) rsyncArgs(src, dest string) []string {
	// --itemize-changes makes the skip observable: with --ignore-existing rsync
	// exits 0 whether it copied or declined, and reporting "Transferred" for a
	// file that was left alone is a lie the operator acts on.
	args := []string{"-a", "-s", "--no-links", "--itemize-changes", "--timeout=" + rsyncStallTimeout}
	if !o.Force {
		// The local guard is an os.Stat that cannot run for a remote
		// destination, and a pre-check would be racy anyway. Enforcing it at
		// the write itself keeps the user-visible contract identical: nothing
		// is clobbered without --force.
		args = append(args, "--ignore-existing")
	}
	if sshCmd := peer.SSHCommandFor(o.Machine); sshCmd != "" {
		args = append(args, "-e", sshCmd)
	}
	return append(args, src, dest)
}

// scpArgs passes the same option set as flags rather than inside -e. Remote
// paths go through the remote shell in this mode, so they are quoted; that is
// the layer rsync's -s makes unnecessary.
func (o CopyOptions) scpArgs(src, dest string) []string {
	args := append([]string{}, remote.Opts(o.Machine)...)
	args = append(args, "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=4")
	return append(args, quoteRemote(src), quoteRemote(dest))
}

// destinationExists reports whether dest is already occupied. A pull writes
// locally, so it is a stat; a push writes on the far machine, so it is one ssh
// using the same option set as the copy, which the ControlMaster socket already
// opened for the resolve.
func (o CopyOptions) destinationExists(ctx context.Context, dest string) (bool, error) {
	host, remotePath, isRemote := strings.Cut(dest, ":")
	if !isRemote {
		if _, err := os.Stat(dest); err == nil {
			return true, nil
		} else if !os.IsNotExist(err) {
			return false, camperrors.Wrapf(err, "check destination %s", dest)
		}
		return false, nil
	}

	args := append([]string{}, remote.Opts(o.Machine)...)
	args = append(args, host, "test", "-e", remote.ShellQuote(remotePath))
	out, err := o.Run(ctx, "ssh", args...)
	if err == nil {
		return true, nil
	}
	// `test -e` exits 1 for "not there"; anything else is a real failure and
	// must not be read as permission to overwrite.
	var exitErr *exec.ExitError
	if asExitError(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, camperrors.Wrapf(err, "check destination on %s: %s", o.Machine.ID, trimOutput(out))
}

func quoteRemote(spec string) string {
	host, p, ok := strings.Cut(spec, ":")
	if !ok {
		return spec
	}
	return host + ":" + remote.ShellQuote(p)
}

// remoteLacksRsync reports the one failure that justifies retrying with scp.
func remoteLacksRsync(err error, out []byte) bool {
	var exitErr *exec.ExitError
	if !asExitError(err, &exitErr) || exitErr.ExitCode() != remoteMissingExit {
		return false
	}
	text := strings.ToLower(string(out))
	return strings.Contains(text, "connection unexpectedly closed") ||
		strings.Contains(text, "command not found")
}

func trimOutput(out []byte) string {
	return strings.TrimSpace(string(out))
}

// rsyncCopied reports whether an --itemize-changes run actually sent a file. An
// itemized transfer line starts with '<' or '>' (direction); a run that only
// declined existing files prints nothing itemized.
func rsyncCopied(out []byte) bool {
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if line[0] == '<' || line[0] == '>' {
			return true
		}
	}
	return false
}
