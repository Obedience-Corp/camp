package prune

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/worktree"
)

func deleteDetachedWorktrees(ctx context.Context, path, baseRef string, opts Options, pr *ProjectResult) {
	wt := worktree.NewGitWorktree(path)
	entries, err := wt.List(ctx)
	if err != nil {
		pr.Results = append(pr.Results, Result{
			Branch: "(detached worktrees)",
			Status: StatusError,
			Detail: fmt.Sprintf("worktree list: %s", err),
		})
		return
	}

	skipPaths := map[string]struct{}{
		filepath.Clean(path): {},
	}
	if gitDir, err := git.Output(ctx, path, "rev-parse", "--absolute-git-dir"); err == nil {
		skipPaths[filepath.Clean(gitDir)] = struct{}{}
	}
	for _, preserved := range opts.PreserveDetachedWorktrees {
		if preserved = strings.TrimSpace(preserved); preserved != "" {
			skipPaths[filepath.Clean(preserved)] = struct{}{}
		}
	}

	removedAny := false
	for _, entry := range entries {
		if !entry.IsDetached || entry.Commit == "" || entry.IsBare || entry.IsLocked {
			continue
		}

		if _, skip := skipPaths[filepath.Clean(entry.Path)]; skip {
			continue
		}

		if _, err := os.Lstat(entry.Path); os.IsNotExist(err) {
			// The folder is gone; only git's registration is left.
			result := Result{Branch: formatDetachedWorktreeLabel(entry.Commit)}
			if opts.DryRun {
				result.Status = StatusWorktreeWouldRemove
				result.Detail = "folder already gone, registration would be dropped: " + entry.Path
			} else {
				result.Status = StatusWorktreeRemoved
				result.Detail = "folder was already gone, registration dropped: " + entry.Path
				removedAny = true
			}
			pr.Results = append(pr.Results, result)
			continue
		}

		merged, err := git.IsAncestor(ctx, path, entry.Commit, baseRef)
		if err != nil {
			pr.Results = append(pr.Results, Result{
				Branch: formatDetachedWorktreeLabel(entry.Commit),
				Status: StatusError,
				Detail: fmt.Sprintf("merge-base check for %s: %s", entry.Path, err),
			})
			continue
		}
		if !merged {
			continue
		}

		clean, err := detachedWorktreeClean(ctx, entry.Path)
		if err != nil {
			pr.Results = append(pr.Results, Result{
				Branch: formatDetachedWorktreeLabel(entry.Commit),
				Status: StatusError,
				Detail: fmt.Sprintf("dirty check for %s: %s", entry.Path, err),
			})
			continue
		}
		if !clean {
			detail := fmt.Sprintf("dirty detached worktree: %s", entry.Path)
			if opts.DryRun {
				detail = fmt.Sprintf("would keep dirty detached worktree: %s", entry.Path)
			}
			pr.Results = append(pr.Results, Result{
				Branch:     formatDetachedWorktreeLabel(entry.Commit),
				Status:     StatusSkipped,
				Detail:     detail,
				SkipReason: SkipReasonDirtyWorktree,
			})
			continue
		}

		result := Result{
			Branch: formatDetachedWorktreeLabel(entry.Commit),
			Detail: entry.Path,
		}

		if opts.DryRun {
			result.Status = StatusWorktreeWouldRemove
			pr.Results = append(pr.Results, result)
			continue
		}

		if err := wt.Remove(ctx, entry.Path, true); err != nil {
			result.Status = StatusError
			result.Detail = fmt.Sprintf("%s: %s", entry.Path, err)
			pr.Results = append(pr.Results, result)
			continue
		}

		result.Status = StatusWorktreeRemoved
		pr.Results = append(pr.Results, result)
		removedAny = true
	}

	if !opts.DryRun && removedAny {
		appendWorktreePruneError(ctx, wt, pr)
	}
}

func formatDetachedWorktreeLabel(commit string) string {
	const shortSHA = 7
	if len(commit) > shortSHA {
		commit = commit[:shortSHA]
	}
	return "detached@" + commit
}

func detachedWorktreeClean(ctx context.Context, path string) (bool, error) {
	hasChanges, err := git.HasChanges(ctx, path)
	if err != nil {
		return false, err
	}
	return !hasChanges, nil
}

func appendWorktreePruneError(ctx context.Context, wt *worktree.GitWorktree, pr *ProjectResult) {
	if _, err := wt.Prune(ctx, false); err != nil {
		pr.Results = append(pr.Results, Result{
			Branch: "(worktree admin)",
			Status: StatusError,
			Detail: fmt.Sprintf("worktree prune: %s", err),
		})
	}
}
