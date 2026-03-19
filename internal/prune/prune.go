package prune

import (
	"context"
	"fmt"
	"strings"

	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/worktree"
)

// Status represents the outcome of a single branch prune operation.
type Status string

// SkipReason identifies why a prune result was skipped.
type SkipReason string

const (
	StatusDeleted             Status = "deleted"
	StatusWouldDelete         Status = "would delete"
	StatusSkipped             Status = "skipped"
	StatusError               Status = "error"
	StatusWouldPrune          Status = "would prune"
	StatusWorktreeRemoved     Status = "wt removed"
	StatusWorktreeWouldRemove Status = "wt would remove"

	SkipReasonActiveWorktree SkipReason = "active_worktree"
)

// Options holds configuration for a prune operation.
type Options struct {
	DryRun       bool
	Force        bool
	Remote       bool
	RemoteDelete bool
	BaseRef      string

	// SkipWorktreeBranches preserves merged branches that still have active
	// worktrees instead of removing the worktree and deleting the branch.
	SkipWorktreeBranches bool
}

// Result holds the outcome for a single branch.
type Result struct {
	Branch     string
	Status     Status
	Detail     string
	SkipReason SkipReason
}

// ProjectResult holds all results for a single project.
type ProjectResult struct {
	Name    string
	Path    string
	Results []Result
	Pruned  int // remote refs pruned
	Error   string
}

// Execute runs the prune logic for a single project.
func Execute(ctx context.Context, name, path string, opts Options) ProjectResult {
	pr := ProjectResult{Name: name, Path: path}

	baseRef := strings.TrimSpace(opts.BaseRef)
	var (
		merged []string
		err    error
	)
	if baseRef == "" {
		merged, err = git.MergedBranches(ctx, path)
	} else {
		merged, err = git.MergedBranchesFromRef(ctx, path, baseRef)
	}
	if err != nil {
		pr.Error = err.Error()
		return pr
	}

	if len(merged) == 0 && !opts.Remote {
		return pr
	}

	deleteLocalBranches(ctx, path, merged, opts, &pr)

	// Remote branch deletion uses the original merged list intentionally —
	// remote branches should be cleaned up regardless of local deletion outcome.
	deleteRemoteBranches(ctx, path, merged, opts, &pr)

	pruneTrackingRefs(ctx, path, opts, &pr)

	return pr
}

// detectWorktreesForBranches lists worktrees and returns a map of branch name → worktree entry
// for branches that appear in the merged set.
func detectWorktreesForBranches(ctx context.Context, path string, merged []string) map[string]worktree.GitWorktreeEntry {
	wt := worktree.NewGitWorktree(path)
	entries, err := wt.List(ctx)
	if err != nil {
		return nil
	}

	mergedSet := make(map[string]struct{}, len(merged))
	for _, b := range merged {
		mergedSet[b] = struct{}{}
	}

	result := make(map[string]worktree.GitWorktreeEntry)
	for _, e := range entries {
		if _, ok := mergedSet[e.Branch]; ok {
			result[e.Branch] = e
		}
	}
	return result
}

// deleteLocalBranches handles confirmation and deletion of locally merged branches.
// If a branch has an active worktree, the worktree is removed first.
func deleteLocalBranches(ctx context.Context, path string, merged []string, opts Options, pr *ProjectResult) {
	if len(merged) == 0 {
		return
	}

	wtMap := detectWorktreesForBranches(ctx, path, merged)
	branchesToDelete := make([]string, 0, len(merged))
	worktreesToRemove := make(map[string]worktree.GitWorktreeEntry)

	for _, branch := range merged {
		entry, hasWT := wtMap[branch]
		if hasWT && opts.SkipWorktreeBranches {
			detail := fmt.Sprintf("active worktree: %s", entry.Path)
			if opts.DryRun {
				detail = fmt.Sprintf("would keep active worktree: %s", entry.Path)
			}
			pr.Results = append(pr.Results, Result{
				Branch:     branch,
				Status:     StatusSkipped,
				Detail:     detail,
				SkipReason: SkipReasonActiveWorktree,
			})
			continue
		}

		branchesToDelete = append(branchesToDelete, branch)
		if hasWT {
			worktreesToRemove[branch] = entry
		}
	}

	if len(branchesToDelete) == 0 {
		return
	}

	if !opts.DryRun && !opts.Force {
		fmt.Printf("\n%s Will delete %d merged branch(es) in %s:\n",
			ui.WarningIcon(), len(branchesToDelete), ui.Value(pr.Name))
		for _, b := range branchesToDelete {
			if _, hasWT := worktreesToRemove[b]; hasWT {
				fmt.Printf("  %s %s (has worktree — will be removed)\n", ui.Dim("-"), b)
			} else {
				fmt.Printf("  %s %s\n", ui.Dim("-"), b)
			}
		}
		fmt.Print("\nProceed? [y/N] ")
		var answer string
		fmt.Scanln(&answer)
		if !strings.HasPrefix(strings.ToLower(answer), "y") {
			for _, b := range branchesToDelete {
				pr.Results = append(pr.Results, Result{
					Branch: b,
					Status: StatusSkipped,
					Detail: "cancelled by user",
				})
			}
			return
		}
	}

	// Remove worktrees first for branches that have them.
	wt := worktree.NewGitWorktree(path)
	for branch, entry := range worktreesToRemove {
		if opts.DryRun {
			pr.Results = append(pr.Results, Result{
				Branch: branch,
				Status: StatusWorktreeWouldRemove,
				Detail: entry.Path,
			})
			continue
		}
		if err := wt.Remove(ctx, entry.Path, true); err != nil {
			pr.Results = append(pr.Results, Result{
				Branch: branch,
				Status: StatusError,
				Detail: fmt.Sprintf("worktree remove: %s", err),
			})
		} else {
			pr.Results = append(pr.Results, Result{
				Branch: branch,
				Status: StatusWorktreeRemoved,
				Detail: entry.Path,
			})
		}
	}

	// Clean stale worktree refs after removals.
	if !opts.DryRun && len(worktreesToRemove) > 0 {
		wt.Prune(ctx, false)
	}

	for _, branch := range branchesToDelete {
		if opts.DryRun {
			pr.Results = append(pr.Results, Result{
				Branch: branch,
				Status: StatusWouldDelete,
			})
			continue
		}

		if err := git.DeleteBranch(ctx, path, branch); err != nil {
			pr.Results = append(pr.Results, Result{
				Branch: branch,
				Status: StatusError,
				Detail: err.Error(),
			})
		} else {
			pr.Results = append(pr.Results, Result{
				Branch: branch,
				Status: StatusDeleted,
			})
		}
	}
}

// deleteRemoteBranches handles confirmation and deletion of merged branches on origin.
func deleteRemoteBranches(ctx context.Context, path string, merged []string, opts Options, pr *ProjectResult) {
	if !opts.RemoteDelete || len(merged) == 0 {
		return
	}

	if opts.DryRun {
		for _, branch := range merged {
			pr.Results = append(pr.Results, Result{
				Branch: "origin/" + branch,
				Status: StatusWouldDelete,
				Detail: "remote",
			})
		}
		return
	}

	// Always confirm remote deletion independently — --force only covers local
	fmt.Printf("\n%s Will DELETE %d branch(es) from origin (irreversible):\n",
		ui.WarningIcon(), len(merged))
	for _, b := range merged {
		fmt.Printf("  %s origin/%s\n", ui.Dim("-"), b)
	}
	fmt.Print("\nDelete from remote? [y/N] ")
	var answer string
	fmt.Scanln(&answer)
	if !strings.HasPrefix(strings.ToLower(answer), "y") {
		for _, branch := range merged {
			pr.Results = append(pr.Results, Result{
				Branch: "origin/" + branch,
				Status: StatusSkipped,
				Detail: "remote deletion cancelled",
			})
		}
		return
	}

	for _, branch := range merged {
		if err := git.DeleteRemoteBranch(ctx, path, branch); err != nil {
			pr.Results = append(pr.Results, Result{
				Branch: "origin/" + branch,
				Status: StatusError,
				Detail: err.Error(),
			})
		} else {
			pr.Results = append(pr.Results, Result{
				Branch: "origin/" + branch,
				Status: StatusDeleted,
				Detail: "remote",
			})
		}
	}
}

// pruneTrackingRefs handles pruning of stale remote tracking refs.
func pruneTrackingRefs(ctx context.Context, path string, opts Options, pr *ProjectResult) {
	if !opts.Remote {
		return
	}

	if opts.DryRun {
		pr.Results = append(pr.Results, Result{
			Branch: "(remote tracking refs)",
			Status: StatusWouldPrune,
		})
		return
	}

	count, err := git.PruneRemote(ctx, path)
	if err != nil {
		pr.Results = append(pr.Results, Result{
			Branch: "(remote tracking refs)",
			Status: StatusError,
			Detail: err.Error(),
		})
	} else {
		pr.Pruned = count
	}
}
