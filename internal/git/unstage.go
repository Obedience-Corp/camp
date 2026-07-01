package git

import (
	"context"
	"os/exec"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// UnstagePath removes staged changes under path from the index without
// touching the working tree. Paths with nothing staged are a no-op, and
// unborn branches (no HEAD yet) are handled by git reset directly.
func UnstagePath(ctx context.Context, repoPath, path string) error {
	cfg := DefaultRetryConfig()
	cfg.OperationName = "unstage"

	return WithLockRetry(ctx, repoPath, cfg, func() error {
		cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "reset", "-q", "--", path)
		output, err := cmd.CombinedOutput()
		if err != nil {
			errType := ClassifyGitError(string(output), cmd.ProcessState.ExitCode())
			if errType == GitErrorLock {
				return &LockError{Path: "index.lock", Err: err}
			}
			return camperrors.NewGit("reset", "", errType.String(), strings.TrimSpace(string(output)), err)
		}
		return nil
	})
}
