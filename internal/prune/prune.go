package prune

import (
	"context"
	"fmt"
	"path/filepath"
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
	SkipReasonDirtyWorktree  SkipReason = "dirty_worktree"
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
	if baseRef == "" {
		baseRef = git.DefaultBranch(ctx, path)
		if baseRef == "" {
			pr.Error = "could not determine default branch"
			return pr
		}
	}

	merged, err := git.MergedBranchesFromRef(ctx, path, baseRef)
	if err != nil {
		pr.Error = err.Error()
		return pr
	}

	// Gone-upstream branches catch squash-merged PRs: git's ancestry-based
	// --merged check misses them because squash creates a brand-new commit
	// on base that shares no history with the branch. When the remote
	// source branch is deleted (GitHub's "Automatically delete head
	// branches" default), fetch --prune marks the upstream as gone, which
	// is the same "done" signal across any forge that deletes merged
	// branches.
	gone, err := git.GoneBranches(ctx, path)
	if err != nil {
		pr.Results = append(pr.Results, Result{
			Branch: "(gone upstream)",
			Status: StatusError,
			Detail: fmt.Sprintf("list gone branches: %s", err),
		})
		gone = nil
	}

	candidates, forced := unionBranches(merged, gone)

	deleteDetachedWorktrees(ctx, path, baseRef, opts, &pr)
	if len(candidates) == 0 && !opts.Remote {
		return pr
	}

	deleteLocalBranches(ctx, path, candidates, forced, opts, &pr)

	// Remote branch deletion uses only the ancestry-merged list — a
	// gone-upstream branch has already been deleted upstream, so there is
	// nothing to delete on origin.
	deleteRemoteBranches(ctx, path, merged, opts, &pr)

	pruneTrackingRefs(ctx, path, opts, &pr)

	return pr
}

// unionBranches merges a branches list from ancestry (git --merged) with one
// from gone-upstream tracking, dedup'd, returning the combined list plus a
// set of branch names that must be force-deleted (i.e. came only from the
// gone path, where git's -d safe-delete would refuse because the branch is
// squash-merged rather than ancestry-merged).
func unionBranches(merged, gone []string) ([]string, map[string]struct{}) {
	mergedSet := make(map[string]struct{}, len(merged))
	for _, b := range merged {
		mergedSet[b] = struct{}{}
	}
	forced := make(map[string]struct{})

	out := make([]string, 0, len(merged)+len(gone))
	out = append(out, merged...)
	for _, b := range gone {
		if _, already := mergedSet[b]; already {
			continue
		}
		out = append(out, b)
		forced[b] = struct{}{}
	}
	return out, forced
}

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

	removedAny := false
	for _, entry := range entries {
		if !entry.IsDetached || entry.Commit == "" || entry.IsBare || entry.IsLocked {
			continue
		}

		if _, skip := skipPaths[filepath.Clean(entry.Path)]; skip {
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

// deleteLocalBranches handles confirmation and deletion of locally merged
// branches. If a branch has an active worktree, the worktree is removed
// first. Entries listed in forced (branches whose upstream is gone but
// which git's ancestry check does not see as merged) are deleted with -D
// instead of -d.
func deleteLocalBranches(ctx context.Context, path string, merged []string, forced map[string]struct{}, opts Options, pr *ProjectResult) {
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
		fmt.Printf("\n%s Will delete %d merged or gone-upstream branch(es) in %s:\n",
			ui.WarningIcon(), len(branchesToDelete), ui.Value(pr.Name))
		for _, b := range branchesToDelete {
			suffix := ""
			if _, isForced := forced[b]; isForced {
				suffix = " (gone upstream — force delete)"
			}
			if _, hasWT := worktreesToRemove[b]; hasWT {
				fmt.Printf("  %s %s%s (has worktree — will be removed)\n", ui.Dim("-"), b, suffix)
			} else {
				fmt.Printf("  %s %s%s\n", ui.Dim("-"), b, suffix)
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
		appendWorktreePruneError(ctx, wt, pr)
	}

	for _, branch := range branchesToDelete {
		detail := ""
		if _, isForced := forced[branch]; isForced {
			detail = "gone upstream"
		}

		if opts.DryRun {
			pr.Results = append(pr.Results, Result{
				Branch: branch,
				Status: StatusWouldDelete,
				Detail: detail,
			})
			continue
		}

		var deleteErr error
		if _, isForced := forced[branch]; isForced {
			deleteErr = git.DeleteBranchForce(ctx, path, branch)
		} else {
			deleteErr = git.DeleteBranch(ctx, path, branch)
		}

		if deleteErr != nil {
			pr.Results = append(pr.Results, Result{
				Branch: branch,
				Status: StatusError,
				Detail: deleteErr.Error(),
			})
		} else {
			pr.Results = append(pr.Results, Result{
				Branch: branch,
				Status: StatusDeleted,
				Detail: detail,
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
