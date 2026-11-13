package prune

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/worktree"
)

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
	if opts.previewsPrimaryAtBase() {
		// The worktree holding the current branch is the primary worktree
		// itself. The real pass runs after it has left that branch, so it is
		// not a worktree to remove.
		delete(wtMap, git.CurrentBranch(ctx, path))
	}
	branchesToDelete := make([]string, 0, len(merged))
	plans := make(map[string]worktreePlan)
	now := time.Now()

	// Decide every worktree before asking anything, so the confirmation lists
	// exactly what the pass will do.
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
		if hasWT {
			plan := planWorktree(ctx, path, entry, branch, opts, now)
			switch plan.action {
			case actionKeep:
				detail := plan.reason
				if opts.DryRun {
					detail = "would keep " + detail
				}
				pr.Results = append(pr.Results, Result{Branch: branch, Status: StatusSkipped, Detail: detail, SkipReason: plan.skip})
				continue
			case actionError:
				pr.Results = append(pr.Results, Result{Branch: branch, Status: StatusError, Detail: plan.reason})
				continue
			}
			plans[branch] = plan
		}
		branchesToDelete = append(branchesToDelete, branch)
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
			if plan, hasWT := plans[b]; hasWT {
				suffix += " " + promptWorktreeNote(plan.action)
			}
			fmt.Printf("  %s %s%s\n", ui.Dim("-"), b, suffix)
		}
		fmt.Print("\nProceed? [y/N] ")
		var answer string
		// An empty answer is an error to Scanln and means no.
		_, _ = fmt.Scanln(&answer)
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

	wt := worktree.NewGitWorktree(path)
	skipBranchDelete := make(map[string]struct{})
	for _, branch := range branchesToDelete {
		plan, hasWT := plans[branch]
		if !hasWT {
			continue
		}
		results, ok := removePlannedWorktree(ctx, wt, path, branch, wtMap[branch], plan, opts)
		pr.Results = append(pr.Results, results...)
		if !ok {
			skipBranchDelete[branch] = struct{}{}
		}
	}

	// Clean stale worktree refs after removals.
	if !opts.DryRun && len(plans) > 0 {
		appendWorktreePruneError(ctx, wt, pr)
	}

	for _, branch := range branchesToDelete {
		if _, skip := skipBranchDelete[branch]; skip {
			continue
		}

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

func promptWorktreeNote(action worktreeAction) string {
	switch action {
	case actionSaveAndRemove:
		return "(has worktree — uncommitted edits will be saved, then it is removed)"
	case actionDropRegistration:
		return "(worktree folder is already gone — its registration will be dropped)"
	default:
		return "(has worktree — will be removed)"
	}
}

// removePlannedWorktree carries out one worktree plan. ok is false when the
// worktree is still there, in which case its branch must be kept too.
//
// A dirty worktree is removed only after its edits are saved and read back. No
// failure here falls back to deleting anything.
func removePlannedWorktree(ctx context.Context, wt *worktree.GitWorktree, projectPath, branch string, entry worktree.GitWorktreeEntry, plan worktreePlan, opts Options) (results []Result, ok bool) {
	restore := "restore with: camp project prune --restore " + branch
	if opts.DryRun {
		switch plan.action {
		case actionSaveAndRemove:
			results = append(results, Result{Branch: branch, Status: StatusEditsWouldSave, Detail: entry.Path})
		case actionDropRegistration:
			return []Result{{Branch: branch, Status: StatusWorktreeWouldRemove, Detail: "folder already gone, registration would be dropped: " + entry.Path}}, true
		}
		return append(results, Result{Branch: branch, Status: StatusWorktreeWouldRemove, Detail: entry.Path}), true
	}

	if plan.action == actionDropRegistration {
		if _, err := wt.Prune(ctx, false); err != nil {
			return []Result{{Branch: branch, Status: StatusError, Detail: fmt.Sprintf("worktree prune: %s", err)}}, false
		}
		return []Result{{Branch: branch, Status: StatusWorktreeRemoved, Detail: "folder was already gone, registration dropped: " + entry.Path}}, true
	}

	savedRef := ""
	if plan.action == actionSaveAndRemove {
		ref, err := saveWorktreeEdits(ctx, projectPath, entry.Path, branch)
		if err != nil {
			return []Result{{Branch: branch, Status: StatusSkipped, SkipReason: SkipReasonDirtyWorktree,
				Detail: fmt.Sprintf("dirty worktree kept, its edits could not be saved (%s): %s", err, entry.Path)}}, false
		}
		savedRef = ref
	}

	if err := wt.Remove(ctx, entry.Path, true); err != nil {
		detail := fmt.Sprintf("worktree remove: %s", err)
		if savedRef != "" {
			// The worktree may be partly removed, so the saved copy stays.
			detail += "; its edits are saved, " + restore + " once the folder is cleared"
		}
		return []Result{{Branch: branch, Status: StatusError, Detail: detail}}, false
	}
	if _, err := os.Lstat(entry.Path); err == nil {
		detail := "git removed the worktree but files remain in " + entry.Path
		if savedRef != "" {
			detail += "; its edits are saved, " + restore + " once the folder is cleared"
		}
		return []Result{{Branch: branch, Status: StatusError, Detail: detail}}, false
	}

	if savedRef != "" {
		results = append(results, Result{Branch: branch, Status: StatusEditsSaved, Detail: restore})
	}
	return append(results, Result{Branch: branch, Status: StatusWorktreeRemoved, Detail: entry.Path}), true
}
