package fresh

import (
	"context"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
)

// validateStackCleanupTarget rejects missing branches and default-branch
// targets (main/master, or the repo's default) unless allowDefaultTarget.
func validateStackCleanupTarget(ctx context.Context, repoPath, target string, allowDefaultTarget bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(target) == "" {
		return camperrors.New("--cleanup-stack requires --branch <existing-branch>")
	}
	if !git.BranchExists(ctx, repoPath, target) {
		return camperrors.Newf("branch %q does not exist — use --branch with an existing branch for --cleanup-stack", target)
	}
	defaultBranch := git.DefaultBranch(ctx, repoPath)
	if isProtectedDefaultBranch(target, defaultBranch) && !allowDefaultTarget {
		return camperrors.Newf(
			"--cleanup-stack refuses the default branch %q because it would treat every merged feature worktree as a stack child\nHint: pass --branch with an aggregate/stack branch, or pass --allow-default-target to override, or use `camp prune` to clean branches merged into the default branch",
			target,
		)
	}
	return nil
}

// isProtectedDefaultBranch reports whether target is the repo default or a
// conventional default-branch name. Those targets have no stack affinity:
// every merged feature looks like a child.
func isProtectedDefaultBranch(target, defaultBranch string) bool {
	t := strings.TrimSpace(target)
	if t == "" {
		return false
	}
	if t == "main" || t == "master" {
		return true
	}
	def := strings.TrimSpace(defaultBranch)
	return def != "" && t == def
}

// shouldScopeToStackChildren is false only when the operator explicitly
// targeted a protected default branch with --allow-default-target.
func shouldScopeToStackChildren(target, defaultBranch string, allowDefaultTarget bool) bool {
	return !(allowDefaultTarget && isProtectedDefaultBranch(target, defaultBranch))
}

// stackScopingBase is the branch used to exclude "already on default" worktrees
// from stack cleanup. Prefer the repo default; fall back to main/master.
func stackScopingBase(ctx context.Context, repoPath, target, defaultBranch string) string {
	if def := strings.TrimSpace(defaultBranch); def != "" && def != target {
		return def
	}
	for _, candidate := range []string{"main", "master"} {
		if candidate == target {
			continue
		}
		if git.BranchExists(ctx, repoPath, candidate) {
			return candidate
		}
	}
	return ""
}
