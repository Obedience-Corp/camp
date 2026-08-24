package fresh

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/worktree"
)

// stackCleanupAction is the verdict for a single child worktree discovered
// during a --cleanup-stack pass.
type stackCleanupAction int

const (
	// stackActionKeep preserves the target branch's own worktree and the
	// primary project path. They are never candidates for removal.
	stackActionKeep stackCleanupAction = iota
	// stackActionRemove targets a clean worktree whose branch is merged into
	// the selected stack root.
	stackActionRemove
	// stackActionSkipDirty leaves a worktree in place because it has
	// uncommitted changes.
	stackActionSkipDirty
	// stackActionSkipUnmerged leaves a worktree in place because its branch
	// is not merged into the stack root.
	stackActionSkipUnmerged
)

// stackWorktreePlan is the per-worktree entry in a cleanup plan.
type stackWorktreePlan struct {
	entry  worktree.GitWorktreeEntry
	action stackCleanupAction
}

// stackCleanupPlan is the full plan for a --cleanup-stack pass against one
// project. It is computed before any mutation so dry-run and real runs report
// the same decision set.
type stackCleanupPlan struct {
	targetBranch string
	projectPath  string
	primaryPath  string
	keep         []stackWorktreePlan
	remove       []stackWorktreePlan
	skipDirty    []stackWorktreePlan
	skipUnmerged []stackWorktreePlan
}

// planStackCleanup discovers linked worktrees for the project and classifies
// each against the target branch. The target branch's own worktree and the
// primary project path are always kept; linked worktrees whose branches are
// merged into the target branch are candidates for removal; dirty worktrees
// are skipped rather than destroyed.
func planStackCleanup(ctx context.Context, projectPath, targetBranch string) (stackCleanupPlan, error) {
	plan := stackCleanupPlan{
		targetBranch: targetBranch,
		projectPath:  projectPath,
		primaryPath:  filepath.Clean(projectPath),
	}

	if !git.BranchExists(ctx, projectPath, targetBranch) {
		return plan, camperrors.Newf("branch %q does not exist — use --branch with an existing branch for --cleanup-stack", targetBranch)
	}

	// Branches merged into the target branch are the stack's completed children.
	merged, err := git.MergedBranchesFromRef(ctx, projectPath, targetBranch)
	if err != nil {
		return plan, camperrors.Wrapf(err, "list branches merged into %s", targetBranch)
	}
	mergedSet := make(map[string]struct{}, len(merged))
	for _, b := range merged {
		mergedSet[b] = struct{}{}
	}

	wt := worktree.NewGitWorktree(projectPath)
	entries, err := wt.List(ctx)
	if err != nil {
		return plan, camperrors.Wrap(err, "list project worktrees")
	}

	primaryToplevel := worktreeToplevel(ctx, projectPath)

	for _, entry := range entries {
		// Only consider real linked worktrees, not the main working tree or
		// git-internal paths.
		if !worktree.IsLinkedWorktree(projectPath, entry) {
			plan.keep = append(plan.keep, stackWorktreePlan{entry: entry, action: stackActionKeep})
			continue
		}

		// Never remove the primary project path or the worktree that holds
		// the target branch itself.
		if sameWorktreePath(primaryToplevel, worktreeToplevel(ctx, entry.Path)) {
			plan.keep = append(plan.keep, stackWorktreePlan{entry: entry, action: stackActionKeep})
			continue
		}
		if entry.Branch == targetBranch {
			plan.keep = append(plan.keep, stackWorktreePlan{entry: entry, action: stackActionKeep})
			continue
		}

		// Detached worktrees are not part of a named-branch stack; keep them
		// so the default prune pass (or the user) handles them separately.
		if entry.IsDetached {
			plan.keep = append(plan.keep, stackWorktreePlan{entry: entry, action: stackActionKeep})
			continue
		}

		// Branch not merged into the stack root → keep it.
		if _, ok := mergedSet[entry.Branch]; !ok {
			plan.skipUnmerged = append(plan.skipUnmerged, stackWorktreePlan{entry: entry, action: stackActionSkipUnmerged})
			continue
		}

		// Merged into the stack root → check cleanliness before removing.
		hasChanges, err := git.HasChanges(ctx, entry.Path)
		if err != nil {
			// Treat an error as a skip: never destroy something we could not
			// verify.
			plan.skipDirty = append(plan.skipDirty, stackWorktreePlan{entry: entry, action: stackActionSkipDirty})
			continue
		}
		if hasChanges {
			plan.skipDirty = append(plan.skipDirty, stackWorktreePlan{entry: entry, action: stackActionSkipDirty})
			continue
		}

		plan.remove = append(plan.remove, stackWorktreePlan{entry: entry, action: stackActionRemove})
	}

	return plan, nil
}

// printStackPlan writes the cleanup plan to stdout so the user sees exactly
// what will be kept, removed, and skipped before any mutation occurs. In
// dry-run mode this is the only output for the stack step.
func printStackPlan(prefix string, plan stackCleanupPlan, dryRun bool) {
	total := len(plan.keep) + len(plan.remove) + len(plan.skipDirty) + len(plan.skipUnmerged)
	fmt.Printf("%s── Stack cleanup plan %-16s %s\n", prefix,
		ui.Value(plan.targetBranch), freshStepDim.Render(fmt.Sprintf("(%d worktree(s) found)", total)))

	if len(plan.keep) > 0 {
		fmt.Printf("%s   %s\n", prefix, freshStepDim.Render("keep:"))
		for _, p := range plan.keep {
			fmt.Printf("%s     %s %s\n", prefix, ui.Dim(filepath.Base(p.entry.Path)), freshStepDim.Render(p.entry.Branch))
		}
	}

	if len(plan.remove) > 0 {
		action := "remove"
		if dryRun {
			action = "would remove"
		}
		fmt.Printf("%s   %s\n", prefix, freshStepGreen.Render(action+":"))
		for _, p := range plan.remove {
			fmt.Printf("%s     %s %s\n", prefix, filepath.Base(p.entry.Path), freshStepDim.Render(p.entry.Branch))
		}
	}

	if len(plan.skipDirty) > 0 {
		fmt.Printf("%s   %s\n", prefix, ui.Warning("skip (dirty):"))
		for _, p := range plan.skipDirty {
			fmt.Printf("%s     %s %s\n", prefix, filepath.Base(p.entry.Path), freshStepDim.Render(p.entry.Branch))
		}
	}

	if len(plan.skipUnmerged) > 0 {
		fmt.Printf("%s   %s\n", prefix, ui.Warning("skip (not merged):"))
		for _, p := range plan.skipUnmerged {
			fmt.Printf("%s     %s %s\n", prefix, filepath.Base(p.entry.Path), freshStepDim.Render(p.entry.Branch))
		}
	}
}

// executeStackCleanup removes the planned worktrees and their local branches.
// It is called only when not in dry-run mode. Dirty and unmerged worktrees are
// never touched. Each removal is independent: a failure on one worktree does
// not abort the remaining removals.
func executeStackCleanup(ctx context.Context, plan stackCleanupPlan) (removed int, errs []string) {
	wt := worktree.NewGitWorktree(plan.projectPath)

	for _, p := range plan.remove {
		if err := wt.Remove(ctx, p.entry.Path, true); err != nil {
			errs = append(errs, fmt.Sprintf("remove worktree %s: %v", p.entry.Path, err))
			continue
		}

		// Delete the local branch now that its worktree is gone. Force-delete
		// because squash-merged branches are not ancestry-merged and git
		// branch -d would refuse them. The branch was confirmed merged into
		// the target branch via MergedBranchesFromRef above, so removal is
		// safe regardless of whether git sees it as ancestry-merged.
		if err := git.DeleteBranchForce(ctx, plan.projectPath, p.entry.Branch); err != nil {
			errs = append(errs, fmt.Sprintf("delete branch %s: %v", p.entry.Branch, err))
			continue
		}
		removed++
	}

	// Clean up stale worktree administrative refs after removals.
	if removed > 0 {
		if _, err := wt.Prune(ctx, false); err != nil {
			errs = append(errs, fmt.Sprintf("worktree prune: %v", err))
		}
	}

	return removed, errs
}

// runStackCleanup is the entry point for the --cleanup-stack mode. It plans,
// reports, and (when not dry-run) executes the removal of merged child
// worktrees for the selected target branch. Returns an error only when the
// plan cannot be computed; per-worktree removal failures are collected and
// reported as warnings, not fatal errors, so a single bad worktree does not
// abort the fresh cycle.
func runStackCleanup(ctx context.Context, name, path, targetBranch string, dryRun bool, prefix string) error {
	if strings.TrimSpace(targetBranch) == "" {
		return camperrors.New("--cleanup-stack requires --branch <existing-branch>")
	}

	plan, err := planStackCleanup(ctx, path, targetBranch)
	if err != nil {
		return err
	}

	printStackPlan(prefix, plan, dryRun)

	if dryRun {
		fmt.Printf("%s── Stack cleanup %-22s %s\n", prefix, "", freshStepDim.Render("(dry-run — no worktrees or branches changed)"))
		return nil
	}

	if len(plan.remove) == 0 {
		fmt.Printf("%s── Stack cleanup %-22s %s\n", prefix, "", freshStepDim.Render("nothing to remove"))
		return nil
	}

	removed, errs := executeStackCleanup(ctx, plan)

	if removed > 0 {
		detail := fmt.Sprintf("removed %d worktree(s)", removed)
		fmt.Printf("%s── Stack cleanup %-22s %s\n", prefix, "", freshStepGreen.Render(detail))
	}

	for _, msg := range errs {
		fmt.Printf("%s   %s %s\n", prefix, ui.WarningIcon(), ui.Warning(msg))
	}

	return nil
}
