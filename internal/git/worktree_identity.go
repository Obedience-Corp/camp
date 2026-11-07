package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// IsWorktreeRoot reports whether path is the root of its own Git worktree.
// Missing or empty submodule directories return false, so Git cannot walk up
// to the containing campaign and report its state as the child's state.
func IsWorktreeRoot(ctx context.Context, path string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, camperrors.Wrapf(err, "inspect worktree %s", path)
	}
	if !info.IsDir() {
		return false, camperrors.Newf("worktree %s is not a directory", path)
	}
	_, err = os.Lstat(filepath.Join(path, ".git"))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, camperrors.Wrapf(err, "inspect Git entry in %s", path)
	}
	cmd := exec.CommandContext(ctx, "git", "-C", path, "rev-parse", "--show-toplevel")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, camperrors.Wrapf(err, "resolve worktree root for %s: %s", path, strings.TrimSpace(string(output)))
	}
	want, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false, camperrors.Wrapf(err, "resolve worktree path %s", path)
	}
	want, err = filepath.Abs(want)
	if err != nil {
		return false, camperrors.Wrapf(err, "resolve absolute worktree path %s", path)
	}
	got, err := filepath.EvalSymlinks(strings.TrimSpace(string(output)))
	if err != nil {
		return false, camperrors.Wrapf(err, "resolve reported worktree root for %s", path)
	}
	return filepath.Clean(got) == filepath.Clean(want), nil
}
