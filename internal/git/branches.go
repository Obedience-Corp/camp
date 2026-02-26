package git

import (
	"context"
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

	defaultBranch := detectDefaultBranchLocal(ctx, repoPath)
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

// detectDefaultBranchLocal determines the default branch using local-only
// heuristics (no network calls). Checks symbolic-ref first, then falls
// back to checking if main/master/develop exist.
func detectDefaultBranchLocal(ctx context.Context, repoPath string) string {
	if ctx.Err() != nil {
		return ""
	}

	// Try symbolic-ref of origin/HEAD
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath,
		"symbolic-ref", "refs/remotes/origin/HEAD", "--short")
	output, err := cmd.Output()
	if err == nil {
		ref := strings.TrimSpace(string(output))
		// ref is like "origin/main" — extract the branch name
		if parts := strings.SplitN(ref, "/", 2); len(parts) == 2 {
			return parts[1]
		}
		return ref
	}

	// Fallback: check for common default branch names
	for _, candidate := range []string{"main", "master", "develop"} {
		cmd := exec.CommandContext(ctx, "git", "-C", repoPath,
			"rev-parse", "--verify", "--quiet", candidate)
		if cmd.Run() == nil {
			return candidate
		}
	}

	return ""
}
