package git

import (
	"context"
	"errors"
	"os/exec"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// RunGitCmd executes a git command with unified error classification.
//
// It handles the common pattern: check context, run git -C <repoPath> <args>,
// classify stderr on failure (lock, not-repo, network, etc.), and return
// structured errors compatible with WithLockRetry.
//
// On success, returns trimmed stdout. On failure, returns a classified error:
//   - GitErrorLock    → *LockError (compatible with WithLockRetry)
//   - GitErrorNotRepo → ErrNotRepository
//   - GitErrorNetwork → ErrRemoteNotReachable
//   - Others          → camperrors.Wrapf with stderr context
func RunGitCmd(ctx context.Context, repoPath string, args ...string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	fullArgs := append([]string{"-C", repoPath}, args...)
	cmd := exec.CommandContext(ctx, "git", fullArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", classifyGitCmdError(output, err, args)
	}

	return strings.TrimSpace(string(output)), nil
}

// classifyGitCmdError converts raw git command failure into a structured error.
func classifyGitCmdError(output []byte, err error, args []string) error {
	stderr := strings.TrimSpace(string(output))
	exitCode := 0

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		exitCode = exitErr.ExitCode()
	}

	errType := ClassifyGitError(stderr, exitCode)
	op := "git"
	if len(args) > 0 {
		op = strings.Join(args[:min(len(args), 2)], " ")
	}

	switch errType {
	case GitErrorLock:
		return &LockError{Path: "index.lock", Err: err}
	case GitErrorNotRepo:
		return camperrors.WrapJoin(ErrNotRepository, err, op)
	case GitErrorNetwork:
		return camperrors.WrapJoin(ErrRemoteNotReachable, err, op+": "+stderr)
	case GitErrorPermission:
		return camperrors.Wrapf(err, "%s: permission denied: %s", op, stderr)
	default:
		return camperrors.Wrapf(err, "%s: %s", op, stderr)
	}
}

// HasStderr checks if an error's message contains the given substring.
// Useful for post-classifying domain-specific errors on top of RunGitCmd.
func HasStderr(err error, substr string) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), strings.ToLower(substr))
}
