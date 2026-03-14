package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// Checkout switches to the given branch in the repository.
func Checkout(ctx context.Context, repoPath, branch string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "checkout", branch)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return camperrors.Wrapf(err, "checkout %s: %s", branch, strings.TrimSpace(string(output)))
	}
	return nil
}

// CreateBranch creates a new local branch from the current HEAD.
func CreateBranch(ctx context.Context, repoPath, branch string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "checkout", "-b", branch)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return camperrors.Wrapf(err, "create branch %s: %s", branch, strings.TrimSpace(string(output)))
	}
	return nil
}

// BranchExists checks if a local branch exists in the repository.
func BranchExists(ctx context.Context, repoPath, branch string) bool {
	if ctx.Err() != nil {
		return false
	}

	cmd := exec.CommandContext(ctx, "git", "-C", repoPath,
		"rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return cmd.Run() == nil
}

// PullFFOnly performs a fast-forward-only pull from the upstream remote.
// Returns the combined output and any error.
func PullFFOnly(ctx context.Context, repoPath string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "pull", "--ff-only")
	output, err := cmd.CombinedOutput()
	outStr := strings.TrimSpace(string(output))
	if err != nil {
		return outStr, camperrors.Wrapf(err, "pull --ff-only: %s", outStr)
	}
	return outStr, nil
}

// IsMergeInProgress checks if a merge is currently in progress by looking
// for the MERGE_HEAD file in the git directory.
func IsMergeInProgress(ctx context.Context, repoPath string) bool {
	if ctx.Err() != nil {
		return false
	}

	gitDir, err := Output(ctx, repoPath, "rev-parse", "--git-dir")
	if err != nil {
		return false
	}

	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(repoPath, gitDir)
	}

	_, err = os.Stat(filepath.Join(gitDir, "MERGE_HEAD"))
	return err == nil
}

// PushSetUpstream pushes a branch to origin and sets up upstream tracking.
func PushSetUpstream(ctx context.Context, repoPath, branch string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	cmd := exec.CommandContext(ctx, "git", "-C", repoPath,
		"push", "--set-upstream", "origin", branch)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return camperrors.Wrapf(err, "push --set-upstream origin %s: %s",
			branch, strings.TrimSpace(string(output)))
	}
	return nil
}
