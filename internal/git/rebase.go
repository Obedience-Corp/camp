package git

import (
	"context"
	"os"
	"path/filepath"
)

// IsRebaseInProgress checks whether a git rebase is in progress for the repo
// at the given path by looking for rebase-merge or rebase-apply directories.
func IsRebaseInProgress(ctx context.Context, repoPath string) bool {
	gitDir, err := Output(ctx, repoPath, "rev-parse", "--git-dir")
	if err != nil {
		return false
	}

	// Make gitDir absolute relative to repoPath if it's relative.
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(repoPath, gitDir)
	}

	for _, dir := range []string{"rebase-merge", "rebase-apply"} {
		if info, err := os.Stat(filepath.Join(gitDir, dir)); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}
