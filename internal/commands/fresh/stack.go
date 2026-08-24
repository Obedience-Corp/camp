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
	// stackActionRemove targets a clean worktree whose branch landed on the
	// selected stack root (ancestry-merged or squash/patch-id equivalent)
	// and did not also land on the default branch.
	stackActionRemove
	// stackActionSkipDirty leaves a worktree in place because it has
	// uncommitted changes.
	stackActionSkipDirty
	// stackActionSkipUnmerged leaves a worktree in place because its branch
	// is neither ancestry-merged nor squash-equivalent to the stack root.
	stackActionSkipUnmerged
	// stackActionSkipOffStack leaves a worktree whose branch already landed
	// on the default branch. Those are not stack children of the aggregate.
	stackActionSkipOffStack
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
	skipOffStack []stackWorktreePlan
}

// planStackCleanup discovers linked worktrees for the project and classifies
// each against the target branch. The target branch's own worktree and the
// primary project path are always kept. Linked worktrees whose branches
// landed on the target (ancestry or squash/patch-id) and not also on the
// default branch are removal candidates; dirty worktrees are skipped.
func planStackCleanup(ctx context.Context, projectPath, targetBranch string, allowDefaultTarget bool) (stackCleanupPlan, error) {
	plan := stackCleanupPlan{
		targetBranch: targetBranch,
		projectPath:  projectPath,
		primaryPath:  filepath.Clean(projectPath),
	}
	if err := validateStackCleanupTarget(ctx, projectPath, targetBranch, allowDefaultTarget); err != nil {
		return plan, err
	}

	wt := worktree.NewGitWorktree(projectPath)
	entries, err := wt.List(ctx)
	if err != nil {
		return plan, camperrors.Wrap(err, "list project worktrees")
	}

	keep, pending := partitionStackEntries(ctx, projectPath, targetBranch, entries)
	plan.keep = keep
	if len(pending) == 0 {
		return plan, nil
	}

	defaultBranch := git.DefaultBranch(ctx, projectPath)
	scopeToStack := shouldScopeToStackChildren(targetBranch, defaultBranch, allowDefaultTarget)
	targetEq, defaultEq, err := stackEquivalence(ctx, projectPath, targetBranch, defaultBranch, scopeToStack, pending)
	if err != nil {
		return plan, err
	}

	remove, skipDirty, skipUnmerged, skipOffStack, err := classifyPendingWorktrees(ctx, pending, targetEq, defaultEq, scopeToStack)
	if err != nil {
		return plan, err
	}
	plan.remove = remove
	plan.skipDirty = skipDirty
	plan.skipUnmerged = skipUnmerged
	plan.skipOffStack = skipOffStack
	return plan, nil
}

// partitionStackEntries splits worktrees into always-keep (primary, target,
// detached, non-linked) and pending classification candidates.
func partitionStackEntries(ctx context.Context, projectPath, target string, entries []worktree.GitWorktreeEntry) (keep, pending []stackWorktreePlan) {
	primary := worktreeToplevel(ctx, projectPath)
	for _, entry := range entries {
		p := stackWorktreePlan{entry: entry, action: stackActionKeep}
		if !worktree.IsLinkedWorktree(projectPath, entry) ||
			sameWorktreePath(primary, worktreeToplevel(ctx, entry.Path)) ||
			entry.Branch == target ||
			entry.IsDetached {
			keep = append(keep, p)
			continue
		}
		pending = append(pending, stackWorktreePlan{entry: entry})
	}
	return keep, pending
}

// stackEquivalence computes target (and, when scoping, default-branch)
// merge-equivalence for pending worktree branches.
func stackEquivalence(ctx context.Context, projectPath, target, defaultBranch string, scopeToStack bool, pending []stackWorktreePlan) (targetEq, defaultEq map[string]struct{}, err error) {
	branches := uniquePlanBranches(pending)
	targetEq, err = git.BranchesEquivalentToRef(ctx, projectPath, target, branches)
	if err != nil {
		return nil, nil, camperrors.Wrapf(err, "detect branches equivalent to %s", target)
	}
	if !scopeToStack {
		return targetEq, nil, nil
	}
	base := stackScopingBase(ctx, projectPath, target, defaultBranch)
	if base == "" {
		return targetEq, nil, nil
	}
	defaultEq, err = git.BranchesEquivalentToRef(ctx, projectPath, base, branches)
	if err != nil {
		return nil, nil, camperrors.Wrapf(err, "detect branches equivalent to %s", base)
	}
	return targetEq, defaultEq, nil
}

func uniquePlanBranches(pending []stackWorktreePlan) []string {
	seen := make(map[string]struct{}, len(pending))
	out := make([]string, 0, len(pending))
	for _, p := range pending {
		b := p.entry.Branch
		if b == "" {
			continue
		}
		if _, ok := seen[b]; ok {
			continue
		}
		seen[b] = struct{}{}
		out = append(out, b)
	}
	return out
}

func classifyPendingWorktrees(ctx context.Context, pending []stackWorktreePlan, targetEq, defaultEq map[string]struct{}, scopeToStack bool) (remove, skipDirty, skipUnmerged, skipOffStack []stackWorktreePlan, err error) {
	for _, p := range pending {
		if err := ctx.Err(); err != nil {
			return nil, nil, nil, nil, err
		}
		if _, ok := targetEq[p.entry.Branch]; !ok {
			p.action = stackActionSkipUnmerged
			skipUnmerged = append(skipUnmerged, p)
			continue
		}
		if scopeToStack {
			if _, onDefault := defaultEq[p.entry.Branch]; onDefault {
				p.action = stackActionSkipOffStack
				skipOffStack = append(skipOffStack, p)
				continue
			}
		}
		hasChanges, err := git.HasChanges(ctx, p.entry.Path)
		if err != nil || hasChanges {
			p.action = stackActionSkipDirty
			skipDirty = append(skipDirty, p)
			continue
		}
		p.action = stackActionRemove
		remove = append(remove, p)
	}
	return remove, skipDirty, skipUnmerged, skipOffStack, nil
}

// printStackPlan writes the cleanup plan to stdout so the user sees exactly
// what will be kept, removed, and skipped before any mutation occurs. In
// dry-run mode this is the only output for the stack step.
func printStackPlan(prefix string, plan stackCleanupPlan, dryRun bool) {
	total := len(plan.keep) + len(plan.remove) + len(plan.skipDirty) + len(plan.skipUnmerged) + len(plan.skipOffStack)
	fmt.Printf("%s── Stack cleanup plan %-16s %s\n", prefix,
		ui.Value(plan.targetBranch), freshStepDim.Render(fmt.Sprintf("(%d worktree(s) found)", total)))

	printPlanGroup(prefix, "keep:", plan.keep, false)
	removeLabel := "remove:"
	if dryRun {
		removeLabel = "would remove:"
	}
	printPlanGroup(prefix, removeLabel, plan.remove, true)
	printPlanGroup(prefix, "skip (dirty):", plan.skipDirty, false)
	printPlanGroup(prefix, "skip (not merged):", plan.skipUnmerged, false)
	printPlanGroup(prefix, "skip (not stacked):", plan.skipOffStack, false)
}

func printPlanGroup(prefix, label string, entries []stackWorktreePlan, highlight bool) {
	if len(entries) == 0 {
		return
	}
	rendered := freshStepDim.Render(label)
	if highlight {
		rendered = freshStepGreen.Render(label)
	} else if strings.HasPrefix(label, "skip") {
		rendered = ui.Warning(label)
	}
	fmt.Printf("%s   %s\n", prefix, rendered)
	for _, p := range entries {
		name := filepath.Base(p.entry.Path)
		if highlight {
			fmt.Printf("%s     %s %s\n", prefix, name, freshStepDim.Render(p.entry.Branch))
			continue
		}
		fmt.Printf("%s     %s %s\n", prefix, ui.Dim(name), freshStepDim.Render(p.entry.Branch))
	}
}

// executeStackCleanup removes the planned worktrees and their local branches.
// It is called only when not in dry-run mode. Dirty, unmerged, and off-stack
// worktrees are never touched. Each removal is independent: a failure on one
// worktree does not abort the remaining removals.
func executeStackCleanup(ctx context.Context, plan stackCleanupPlan) (removed int, errs []string) {
	wt := worktree.NewGitWorktree(plan.projectPath)

	for _, p := range plan.remove {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err.Error())
			return removed, errs
		}
		if err := wt.Remove(ctx, p.entry.Path, true); err != nil {
			errs = append(errs, fmt.Sprintf("remove worktree %s: %v", p.entry.Path, err))
			continue
		}

		// Force-delete: squash-equivalent branches are not ancestry-merged,
		// so git branch -d refuses them. Removal is still safe — the branch
		// was confirmed equivalent to the target (ancestor or cumulative
		// patch-id match) during planning.
		if err := git.DeleteBranchForce(ctx, plan.projectPath, p.entry.Branch); err != nil {
			errs = append(errs, fmt.Sprintf("delete branch %s: %v", p.entry.Branch, err))
			continue
		}
		removed++
	}

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
func runStackCleanup(ctx context.Context, name, path, targetBranch string, dryRun, allowDefaultTarget bool, prefix string) error {
	if strings.TrimSpace(targetBranch) == "" {
		return camperrors.New("--cleanup-stack requires --branch <existing-branch>")
	}

	plan, err := planStackCleanup(ctx, path, targetBranch, allowDefaultTarget)
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
