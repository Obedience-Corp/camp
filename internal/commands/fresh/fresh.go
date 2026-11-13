package fresh

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/prune"
	"github.com/Obedience-Corp/camp/internal/ui"
)

// freshStepStyle defines styles for step output.
var (
	freshStepDim   = lipgloss.NewStyle().Foreground(ui.DimColor)
	freshStepGreen = lipgloss.NewStyle().Foreground(ui.SuccessColor)
)

type freshOptions struct {
	branch             string
	prune              bool
	pruneRemote        bool
	push               bool
	followUps          []config.FollowUpConfig
	dryRun             bool
	campRoot           string
	mergedWorkitems    string
	cleanupStack       bool
	allowDefaultTarget bool
	// holdSummary, when set, receives the closing line instead of printing it.
	// The single-project command prints that line after the campaign workitem
	// sweep, so the sweep is not an afterthought under "Fresh!".
	holdSummary *string
}

type freshSyncState struct {
	defaultBranch string
	baseRef       string
	displayRef    string
	// detached is true when this path must sync via origin/<default> rather
	// than checking out the local default branch (another worktree holds it
	// and could not be reclaimed).
	detached bool
	// worktreePath is the other worktree that held (or still holds) the
	// default branch, when known.
	worktreePath string
	// reclaimed is true when fresh detached that other worktree so the
	// default branch can be checked out here normally.
	reclaimed bool
}

func executeFresh(ctx context.Context, name, path string, opts freshOptions) error {
	if opts.cleanupStack {
		if err := validateStackCleanupTarget(ctx, path, opts.branch, opts.allowDefaultTarget); err != nil {
			return err
		}
	}

	if opts.dryRun {
		fmt.Printf("  %s %s\n", ui.Info(name), freshStepDim.Render("(dry-run)"))
	} else {
		fmt.Printf("  %s\n", ui.Info(name))
	}

	// Step 0: Safety checks
	if err := freshSafetyChecks(ctx, path, opts.dryRun); err != nil {
		return err
	}

	// Step 1: Checkout default branch
	defaultBranch := git.DefaultBranch(ctx, path)
	if defaultBranch == "" {
		return camperrors.Newf("could not determine default branch for %s", name)
	}

	currentBranch := git.CurrentBranch(ctx, path)
	syncState, err := resolveFreshSyncState(ctx, path, defaultBranch)
	if err != nil {
		return camperrors.Wrap(err, "prepare fresh sync state")
	}

	// When another worktree holds the default branch, free it if that tree is
	// clean so this project path can check out main/master normally. Leaving
	// main stuck on a finished feature worktree is the failure mode after
	// camp project worktree add --start-point main.
	freshHeading("Sync", "")
	if err := maybeReclaimDefaultBranch(ctx, &syncState, opts.dryRun); err != nil {
		return err
	}

	if !syncState.detached && currentBranch == defaultBranch && !syncState.reclaimed {
		freshRow("Checkout "+defaultBranch, "already on it", ui.StatusMuted)
	} else if !syncState.detached && opts.dryRun {
		freshRow("Would checkout "+defaultBranch,
			fmt.Sprintf("currently on %s", emptyBranchLabel(currentBranch)), ui.StatusMuted)
	} else if !syncState.detached {
		if err := git.Checkout(ctx, path, defaultBranch); err != nil {
			// Surface a clearer message than git's raw "already used by worktree".
			if isBranchInUseByWorktree(err) {
				return camperrors.Wrapf(err,
					"checkout %s: another worktree holds this branch; "+
						"detach it with `git -C <worktree> checkout --detach` then re-run camp fresh",
					defaultBranch)
			}
			return camperrors.Wrapf(err, "checkout %s", defaultBranch)
		}
		freshRow("Checkout "+defaultBranch, "done", ui.StatusSuccess)
	}

	// Capture the sync base SHA before fetching so the tier-2 backstop can
	// scan commits newly reachable from the default branch after prune (the
	// pruned branch refs themselves are gone by the time prune returns).
	beforeSHAOut, beforeSHAErr := git.Output(ctx, path, "rev-parse", syncState.baseRef)
	beforeSHA := strings.TrimSpace(beforeSHAOut)
	if beforeSHAErr != nil || beforeSHA == "" {
		// Best-effort: without a pre-pull HEAD the tier-2 commit-tag scan is
		// skipped for this project (worktree-link matching still works). Log
		// rather than fail the fresh cycle.
		fmt.Fprintln(os.Stderr, ui.Dim(fmt.Sprintf(
			"  merged-branch backstop: could not capture pre-pull HEAD for %s; commit-tag scan skipped (%v)", name, beforeSHAErr)))
	}

	// Step 2: Fetch once, then either sync detached at origin/<default> or
	// reconcile the checked-out local default branch. When local commits exist,
	// reconciliation first creates a recovery branch and only then resets.
	fetchStatus := "done"
	if opts.dryRun {
		fetchStatus = "done (remote refs only)"
	}
	fetchStep := newFreshLive(os.Stdout, "Fetch origin", "fetching", "")
	fetchStep.Begin()
	if err := fetchFreshRemote(ctx, path, opts.prune); err != nil {
		fetchStep.Finish("failed", ui.StatusError)
		return camperrors.Wrap(err, "fetch origin")
	}
	fetchStep.Finish(fetchStatus, ui.StatusSuccess)

	if syncState.detached {
		note := freshSyncWorktreeNote(syncState)
		if opts.dryRun {
			freshRow("Would use "+syncState.displayRef, note, ui.StatusMuted)
		} else {
			if err := git.CheckoutDetached(ctx, path, syncState.baseRef); err != nil {
				return camperrors.Wrapf(err, "checkout detached %s", syncState.baseRef)
			}
			freshRow("Checkout "+syncState.displayRef, "done", ui.StatusSuccess)
			freshDetail(note)
		}
	} else {
		if err := reconcileFreshDefault(ctx, path, defaultBranch, opts.dryRun); err != nil {
			return err
		}
	}

	// Step 3: Prune merged and gone-upstream branches.
	// Step 2 already refreshed and pruned remote tracking refs, so prune does
	// not perform a second network fetch.
	if opts.prune {
		freshHeading("Prune", "")
		pruneStep := newFreshLive(os.Stdout, "Prune merged branches", "pruning", "")
		pruneStep.Begin()
		// Record branch → worktree path before prune deletes the branch and
		// detaches/removes the worktree. The tier-2 backstop uses this map
		// instead of guessing the branch from the worktree directory name.
		campRoot := backstopRoot(ctx, opts)
		var branchPaths map[string]string
		var branchPathsErr error
		if campRoot != "" {
			branchPaths, branchPathsErr = listWorktreeBranchPaths(ctx, campRoot, path)
		}
		pruneOpts := prune.Options{
			DryRun:        opts.dryRun,
			Force:         true,  // Skip confirmation — fresh is deliberate
			DiscardDirty:  false, // fresh never destroys uncommitted work
			SaveDirty:     true,  // a merged branch's dirty worktree is removed after its edits are saved
			MergedPRs:     prune.GitHubMergedPRHeads,
			Remote:        opts.pruneRemote,
			BaseRef:       syncState.pruneBaseRef(opts.dryRun),
			RefreshRemote: false,
		}
		// A dry-run has not checked out or synced the default branch. Preview
		// the pass from where the sync step would leave this project, so the
		// plan names exactly what the real run removes.
		pruneOpts.PreviewPrimaryAtBase = opts.dryRun
		// Reclaiming the default branch detaches its former worktree. Preserve
		// that exact worktree during this prune pass: fresh created the detached
		// state as a safe branch handoff, so it must not immediately classify
		// the worktree as merged and remove it.
		if syncState.reclaimed && syncState.worktreePath != "" {
			pruneOpts.PreserveDetachedWorktrees = []string{syncState.worktreePath}
		}
		pr := prune.Execute(ctx, name, path, pruneOpts)
		if pr.Error != "" {
			pruneStep.Finish("failed", ui.StatusError)
			return camperrors.Wrapf(errors.New(pr.Error), "prune merged branches")
		}

		deletedNames := pruneResultNames(pr.Results, prune.StatusDeleted, prune.StatusWouldDelete)
		removedWorktrees := pruneResultCount(pr.Results, prune.StatusWorktreeRemoved, prune.StatusWorktreeWouldRemove)

		switch {
		case len(deletedNames) > 0 || removedWorktrees > 0:
			status, names := pruneChecklistStatus(deletedNames, removedWorktrees, opts.dryRun)
			tone := ui.StatusSuccess
			if opts.dryRun {
				tone = ui.StatusMuted
			}
			pruneStep.Finish(status, tone)
			for _, name := range names {
				freshDetail(name)
			}
		default:
			pruneStep.Finish("nothing to prune", ui.StatusMuted)
		}
		for _, note := range pr.Notes {
			freshDetail(note)
		}
		for _, r := range pr.Results {
			switch r.Status {
			case prune.StatusEditsSaved:
				freshDetail(fmt.Sprintf("saved uncommitted edits from %s; %s", r.Branch, r.Detail))
			case prune.StatusEditsWouldSave:
				freshDetail(fmt.Sprintf("would save uncommitted edits from %s before removing its worktree", r.Branch))
			}
		}
		if pr.Pruned > 0 {
			detail := fmt.Sprintf("%d stale refs", pr.Pruned)
			tone := ui.StatusSuccess
			if opts.dryRun {
				detail = "would prune stale refs"
				tone = ui.StatusMuted
			}
			freshRow("Prune remote tracking refs", detail, tone)
		}

		// Tier-2 merged-branch backstop: per project, right after prune, using
		// this project's just-deleted branches and the pre-pull beforeSHA. This
		// is inference evidence, so it only reports or prompts, never
		// auto-promotes. A dry-run downgrades to report-only (mirroring the
		// tier-1 sweep) so it never reaches the prompt/promote path, matching
		// the tier-1 dry-run contract.
		handleMergedBackstop(ctx, os.Stdout, campRoot, path,
			deletedNames, beforeSHA, resolveBackstopMode(opts.mergedWorkitems, opts.dryRun), branchPaths, branchPathsErr)
	}

	// Step 4: Create or checkout branch (optional)
	//
	// --cleanup-stack changes the semantics of --branch: instead of creating a
	// new branch, it switches to an existing target branch and then removes
	// child worktrees whose branches landed on that target (step 4a below),
	// by ancestry or squash/patch-id equivalence. This is the post-stack-merge
	// cleanup flow: the child PRs are already merged into the aggregate, and
	// the stale child worktrees need to be torn down. Default-branch targets
	// are refused unless --allow-default-target.
	branchCheckedOut := false
	branchCreated := false
	if opts.branch != "" {
		freshHeading("Branch", "")
		if opts.cleanupStack {
			if !git.BranchExists(ctx, path, opts.branch) {
				return camperrors.Newf("--cleanup-stack: branch %q does not exist", opts.branch)
			}
			currentBranch := git.CurrentBranch(ctx, path)
			if currentBranch == opts.branch {
				freshRow("Checkout "+opts.branch, "already on it", ui.StatusMuted)
				branchCheckedOut = true
			} else if opts.dryRun {
				freshRow("Would checkout "+opts.branch,
					fmt.Sprintf("currently on %s", emptyBranchLabel(currentBranch)), ui.StatusMuted)
				branchCheckedOut = true
			} else {
				if err := git.Checkout(ctx, path, opts.branch); err != nil {
					return camperrors.Wrapf(err, "checkout %s", opts.branch)
				}
				freshRow("Checkout "+opts.branch, "done", ui.StatusSuccess)
				branchCheckedOut = true
			}
		} else if git.BranchExists(ctx, path, opts.branch) {
			fmt.Fprintf(os.Stderr, "  %sBranch %s %s\n", ui.ChecklistMark(ui.StatusMuted), opts.branch,
				freshStepDim.Render(fmt.Sprintf("already exists, staying on %s", syncState.displayRef)))
		} else if opts.dryRun {
			freshRow("Would create branch "+opts.branch, "from "+syncState.baseRef, ui.StatusMuted)
		} else {
			if err := git.CreateBranch(ctx, path, opts.branch); err != nil {
				return camperrors.Wrapf(err, "create branch %s", opts.branch)
			} else {
				branchCreated = true
				freshRow("Create branch "+opts.branch, "done", ui.StatusSuccess)
			}
		}
	}

	// Step 4a: Stack cleanup (only when --cleanup-stack is set and the target
	// branch was checked out). Discovers linked worktrees whose branches
	// landed on the target (ancestry or squash) and not also on the default
	// branch, then removes them, skipping dirty or unmerged ones. Dry-run
	// prints the plan without acting.
	if opts.cleanupStack && branchCheckedOut {
		if err := runStackCleanup(ctx, name, path, opts.branch, opts.dryRun, opts.allowDefaultTarget); err != nil {
			return camperrors.Wrapf(err, "stack cleanup for %s", opts.branch)
		}
	}

	// Step 5: Push upstream (optional)
	if branchCreated && opts.push {
		if opts.dryRun {
			freshRow("Would push "+opts.branch, "to origin", ui.StatusMuted)
		} else {
			if err := git.PushSetUpstream(ctx, path, opts.branch); err != nil {
				return camperrors.Wrapf(err, "push %s to origin", opts.branch)
			} else {
				freshRow("Push "+opts.branch+" -> origin", "done", ui.StatusSuccess)
			}
		}
	}

	// Step 6: Run configured follow-up command workflows. Only reachable once
	// every prior step has succeeded, so a failed sync/prune/branch cycle
	// never triggers follow-ups.
	if err := runFreshFollowUps(ctx, path, opts.followUps, opts.dryRun); err != nil {
		return err
	}

	summary := freshSummaryLine(opts, syncState, defaultBranch, branchCreated, branchCheckedOut)
	if opts.holdSummary != nil {
		*opts.holdSummary = summary
	} else {
		printFreshSummary(summary)
	}

	return nil
}

func freshSummaryLine(opts freshOptions, syncState freshSyncState, defaultBranch string, branchCreated, branchCheckedOut bool) string {
	if opts.dryRun {
		return freshStepDim.Render("  (dry-run — remote refs refreshed; no local branches or files changed)")
	}
	if opts.cleanupStack && branchCheckedOut {
		return fmt.Sprintf("  %s On %s — stack cleaned.", freshStepGreen.Render("Fresh!"), ui.Value(opts.branch))
	}
	if branchCreated {
		return fmt.Sprintf("  %s Ready to work on %s.", freshStepGreen.Render("Fresh!"), ui.Value(opts.branch))
	}
	if syncState.detached {
		return fmt.Sprintf("  %s Synced to %s.", freshStepGreen.Render("Fresh!"), ui.Value(syncState.displayRef))
	}
	return fmt.Sprintf("  %s Synced to %s.", freshStepGreen.Render("Fresh!"), ui.Value(defaultBranch))
}

func printFreshSummary(summary string) {
	if summary == "" {
		return
	}
	fmt.Println()
	fmt.Println(summary)
}

func pruneResultNames(results []prune.Result, statuses ...prune.Status) []string {
	allowed := make(map[prune.Status]struct{}, len(statuses))
	for _, status := range statuses {
		allowed[status] = struct{}{}
	}

	var names []string
	for _, result := range results {
		if _, ok := allowed[result.Status]; ok {
			names = append(names, result.Branch)
		}
	}
	return names
}

func pruneResultCount(results []prune.Result, statuses ...prune.Status) int {
	return len(pruneResultNames(results, statuses...))
}

// freshSafetyChecks verifies the repo is in a safe state for fresh.
func freshSafetyChecks(ctx context.Context, path string, dryRun bool) error {
	if dryRun {
		return nil
	}

	// Check for merge in progress
	if git.IsMergeInProgress(ctx, path) {
		return camperrors.Newf("merge in progress — complete or abort first")
	}

	// Check for rebase in progress
	if git.IsRebaseInProgress(ctx, path) {
		return camperrors.Newf("rebase in progress — complete or abort first")
	}

	// Check for uncommitted changes
	hasChanges, err := git.HasNonSubmoduleChanges(ctx, path)
	if err != nil {
		return camperrors.Wrap(err, "checking for uncommitted changes")
	}
	if hasChanges {
		return camperrors.Newf("uncommitted changes — commit or stash first")
	}

	return nil
}

// completeProjectName provides tab completion for project names.
func completeProjectName(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	ctx := cmd.Context()

	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	projects, err := project.List(ctx, campRoot)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	var names []string
	for _, p := range projects {
		if strings.HasPrefix(p.Name, toComplete) {
			names = append(names, p.Name)
		}
	}

	return names, cobra.ShellCompDirectiveNoFileComp
}
