package git

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// UnmergedBranchCount returns the number of local branches not yet merged
// into the default branch. Returns 0 on any error (supplementary info,
// graceful degradation).
func UnmergedBranchCount(ctx context.Context, repoPath string) int {
	if ctx.Err() != nil {
		return 0
	}

	defaultBranch := defaultBranchLocal(ctx, repoPath)
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

// defaultBranchLocal determines the default branch using local-only heuristics
// (no network calls). Used by latency-sensitive paths like status displays.
func defaultBranchLocal(ctx context.Context, repoPath string) string {
	if ctx.Err() != nil {
		return ""
	}

	if branch := symbolicRefOriginHead(ctx, repoPath); branch != "" {
		return branch
	}

	for _, candidate := range []string{"main", "master"} {
		cmd := exec.CommandContext(ctx, "git", "-C", repoPath,
			"rev-parse", "--verify", "--quiet", candidate)
		if cmd.Run() == nil {
			return candidate
		}
	}

	return ""
}

// DefaultBranch determines the remote's default branch for a repository.
// This may make a one-time network call if the local symbolic-ref cache
// is not set. Use defaultBranchLocal for latency-sensitive paths.
//
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

	return MergedBranchesFromRef(ctx, repoPath, defaultBranch)
}

// MergedBranchesFromRef returns local branches that have been merged into the
// given base ref, excluding the base ref itself and the current branch.
func MergedBranchesFromRef(ctx context.Context, repoPath, baseRef string) ([]string, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if strings.TrimSpace(baseRef) == "" {
		return nil, fmt.Errorf("base ref is required")
	}

	currentBranch := CurrentBranch(ctx, repoPath)
	localBaseRef := strings.TrimPrefix(baseRef, "refs/remotes/")
	localBaseRef = strings.TrimPrefix(localBaseRef, "origin/")

	cmd := exec.CommandContext(ctx, "git", "-C", repoPath,
		"branch", "--merged", baseRef, "--format=%(refname:short)")
	output, err := cmd.Output()
	if err != nil {
		return nil, camperrors.Wrapf(err, "list merged branches into %s", baseRef)
	}

	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return nil, nil
	}

	var branches []string
	for _, line := range strings.Split(trimmed, "\n") {
		branch := strings.TrimSpace(line)
		if branch == "" || branch == baseRef || branch == localBaseRef || branch == currentBranch {
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
		return camperrors.Wrapf(err, "delete branch %s: %s", branch, strings.TrimSpace(string(output)))
	}
	return nil
}

// DeleteBranchForce deletes a local branch using git branch -D (force delete).
// Required for squash-merged branches, which git's ancestry check does not
// recognize as merged. Callers must have independent evidence the branch is
// safe to remove (e.g. its upstream is gone after a squash-merge).
func DeleteBranchForce(ctx context.Context, repoPath, branch string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	cmd := exec.CommandContext(ctx, "git", "-C", repoPath,
		"branch", "-D", branch)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return camperrors.Wrapf(err, "force-delete branch %s: %s", branch, strings.TrimSpace(string(output)))
	}
	return nil
}

// GoneBranches returns local branches whose upstream tracking ref is gone
// (i.e. the remote branch was deleted since the last fetch --prune).
//
// This is the signal that a squash-merged PR's source branch has been
// removed on the remote. Unlike MergedBranchesFromRef, which uses git
// ancestry and so misses squash-merges, this check depends only on tracking
// metadata — callers typically want to run git fetch --prune first so the
// metadata is current.
//
// The current branch is excluded from the result; deleting the checked-out
// branch is unsafe without an explicit checkout elsewhere.
func GoneBranches(ctx context.Context, repoPath string) ([]string, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	current := CurrentBranch(ctx, repoPath)

	cmd := exec.CommandContext(ctx, "git", "-C", repoPath,
		"for-each-ref", "--format=%(refname:short) %(upstream:track)", "refs/heads/")
	output, err := cmd.Output()
	if err != nil {
		return nil, camperrors.Wrapf(err, "list branches with tracking info")
	}

	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return nil, nil
	}

	var branches []string
	for _, line := range strings.Split(trimmed, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		branch := fields[0]
		track := strings.Join(fields[1:], " ")
		if !strings.Contains(track, "gone") {
			continue
		}
		if branch == current {
			continue
		}
		branches = append(branches, branch)
	}

	return branches, nil
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
		return 0, camperrors.Wrapf(err, "prune remote: %s", strings.TrimSpace(string(output)))
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
		return camperrors.Wrapf(err, "delete remote branch %s: %s", branch, strings.TrimSpace(string(output)))
	}
	return nil
}

// IsAncestor reports whether ancestor is reachable from descendant.
func IsAncestor(ctx context.Context, repoPath, ancestor, descendant string) (bool, error) {
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if strings.TrimSpace(ancestor) == "" {
		return false, camperrors.Wrap(camperrors.ErrInvalidInput, "ancestor is required")
	}
	if strings.TrimSpace(descendant) == "" {
		return false, camperrors.Wrap(camperrors.ErrInvalidInput, "descendant is required")
	}

	cmd := exec.CommandContext(ctx, "git", "-C", repoPath,
		"merge-base", "--is-ancestor", ancestor, descendant)
	err := cmd.Run()
	if err == nil {
		return true, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}

	return false, camperrors.Wrapf(err, "check %s reachable from %s", ancestor, descendant)
}
