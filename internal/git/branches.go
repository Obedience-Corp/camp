package git

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// UnmergedBranchCount returns the number of local branches not yet merged
// into the default branch. Returns 0 on any error (supplementary info,
// graceful degradation).
func UnmergedBranchCount(ctx context.Context, repoPath string) int {
	if ctx.Err() != nil {
		return 0
	}

	defaultBranch := DefaultBranch(ctx, repoPath)
	if defaultBranch == "" {
		return 0
	}

	// List branches not merged into the default branch
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath,
		"branch", "--no-merged", defaultBranch, "--format=%(refname:short)")
	output, err := cmd.Output()
	if err != nil {
		return 0
	}

	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return 0
	}

	count := 0
	for _, line := range strings.Split(trimmed, "\n") {
		branch := strings.TrimSpace(line)
		if branch != "" && branch != defaultBranch {
			count++
		}
	}
	return count
}

// DefaultBranch determines the remote's default branch for a repository.
// Strategy:
//  1. Check local symbolic-ref cache of origin/HEAD
//  2. If not set, run git remote set-head origin --auto (one-time network fetch)
//  3. Retry symbolic-ref
//  4. Fallback: check if main or master exist locally
func DefaultBranch(ctx context.Context, repoPath string) string {
	if ctx.Err() != nil {
		return ""
	}

	// Try symbolic-ref of origin/HEAD (local cache)
	if branch := symbolicRefOriginHead(ctx, repoPath); branch != "" {
		return branch
	}

	// Not set — try to auto-detect from remote (one-time network call)
	autoCmd := exec.CommandContext(ctx, "git", "-C", repoPath,
		"remote", "set-head", "origin", "--auto")
	if autoCmd.Run() == nil {
		if branch := symbolicRefOriginHead(ctx, repoPath); branch != "" {
			return branch
		}
	}

	// Fallback: check for common default branch names (NOT develop)
	for _, candidate := range []string{"main", "master"} {
		cmd := exec.CommandContext(ctx, "git", "-C", repoPath,
			"rev-parse", "--verify", "--quiet", candidate)
		if cmd.Run() == nil {
			return candidate
		}
	}

	return ""
}

// symbolicRefOriginHead reads the local symbolic-ref for origin/HEAD
// and extracts the branch name. Returns empty string on failure.
func symbolicRefOriginHead(ctx context.Context, repoPath string) string {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath,
		"symbolic-ref", "refs/remotes/origin/HEAD", "--short")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	ref := strings.TrimSpace(string(output))
	// ref is like "origin/main" — extract the branch name
	if parts := strings.SplitN(ref, "/", 2); len(parts) == 2 {
		return parts[1]
	}
	return ref
}

// CurrentBranch returns the currently checked-out branch name.
// Returns empty string on error (e.g., detached HEAD).
func CurrentBranch(ctx context.Context, repoPath string) string {
	if ctx.Err() != nil {
		return ""
	}

	cmd := exec.CommandContext(ctx, "git", "-C", repoPath,
		"rev-parse", "--abbrev-ref", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	branch := strings.TrimSpace(string(output))
	if branch == "HEAD" {
		return "" // Detached HEAD
	}
	return branch
}

// MergedBranches returns local branches that have been merged into the default
// branch, excluding the default branch itself and the current branch.
func MergedBranches(ctx context.Context, repoPath string) ([]string, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	defaultBranch := DefaultBranch(ctx, repoPath)
	if defaultBranch == "" {
		return nil, fmt.Errorf("could not determine default branch")
	}

	currentBranch := CurrentBranch(ctx, repoPath)

	cmd := exec.CommandContext(ctx, "git", "-C", repoPath,
		"branch", "--merged", defaultBranch, "--format=%(refname:short)")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("list merged branches: %w", err)
	}

	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return nil, nil
	}

	var branches []string
	for _, line := range strings.Split(trimmed, "\n") {
		branch := strings.TrimSpace(line)
		if branch == "" || branch == defaultBranch || branch == currentBranch {
			continue
		}
		branches = append(branches, branch)
	}

	return branches, nil
}

// DeleteBranch deletes a local branch using git branch -d (safe delete).
func DeleteBranch(ctx context.Context, repoPath, branch string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	cmd := exec.CommandContext(ctx, "git", "-C", repoPath,
		"branch", "-d", branch)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("delete branch %s: %s", branch, strings.TrimSpace(string(output)))
	}
	return nil
}

// PruneRemote removes stale remote tracking references for origin.
// Returns the number of pruned refs.
func PruneRemote(ctx context.Context, repoPath string) (int, error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}

	cmd := exec.CommandContext(ctx, "git", "-C", repoPath,
		"remote", "prune", "origin")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("prune remote: %s", strings.TrimSpace(string(output)))
	}

	// Count pruned lines (lines containing " * [pruned]")
	count := 0
	for _, line := range strings.Split(string(output), "\n") {
		if strings.Contains(line, "[pruned]") {
			count++
		}
	}

	return count, nil
}

// DeleteRemoteBranch pushes a branch deletion to origin.
func DeleteRemoteBranch(ctx context.Context, repoPath, branch string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	cmd := exec.CommandContext(ctx, "git", "-C", repoPath,
		"push", "origin", "--delete", branch)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("delete remote branch %s: %s", branch, strings.TrimSpace(string(output)))
	}
	return nil
}
