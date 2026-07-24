package fresh

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	"github.com/Obedience-Corp/camp/internal/worktree"
)

// freshStepStyle defines styles for step output.
var (
	freshStepDim   = lipgloss.NewStyle().Foreground(ui.DimColor)
	freshStepGreen = lipgloss.NewStyle().Foreground(ui.SuccessColor)
)

// NewFreshCommand creates and returns the fresh cobra command with all subcommands.
func NewFreshCommand() *cobra.Command {
	var (
		freshBranch      string
		freshNoBranch    bool
		freshNoPush      bool
		freshNoPrune     bool
		freshNoFollowUp  bool
		freshDryRun      bool
		freshProjectFlag string
		freshList        []string
	)

	freshCmd := &cobra.Command{
		Use:   "fresh [project-name]",
		Short: "Post-merge branch cycling: sync to default branch and optionally create a new working branch",
		Long: `Reset one or more projects to a fresh state after merging a PR.

Performs the post-merge cycle: checkout default branch, pull latest,
prune merged branches, and optionally create a new working branch.

Auto-detects the current project from your working directory, or accepts a
single project name. Use --list to cycle a specific set of projects in one
run, or 'camp fresh all' to cycle every project submodule in the campaign.

Without configuration, syncs to the default branch and prunes.
Configure .campaign/settings/fresh.yaml to set a default working branch, or
follow-up command workflows (install, build, bootstrap, ...) to run once the
cycle succeeds. Manage those with 'camp fresh configure'. Inspect the resolved
sequence with 'camp fresh show-workflow [project-name]'.

Examples:
  camp fresh                            # Sync current project (checkout default, pull, prune)
  camp fresh --branch develop           # Sync and create develop branch
  camp fresh camp -b feat/new-thing     # Sync camp project, create feature branch
  camp fresh --list camp,fest,festival  # Sync a specific set of projects
  camp fresh --no-prune                 # Sync without pruning
  camp fresh --no-follow-up             # Sync without running configured follow-ups
  camp fresh --dry-run                  # Preview what would happen (follow-ups listed, not run)`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeProjectName,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			campRoot, err := campaign.DetectCached(ctx)
			if err != nil {
				return camperrors.Wrap(err, "not in a campaign")
			}

			// Load fresh config
			cfg, err := config.LoadFreshConfig(ctx, campRoot)
			if err != nil {
				return camperrors.Wrap(err, "loading fresh config")
			}

			flags := freshFlagSet{
				branch:     freshBranch,
				noBranch:   freshNoBranch,
				noPush:     freshNoPush,
				noPrune:    freshNoPrune,
				noFollowUp: freshNoFollowUp,
				dryRun:     freshDryRun,
			}

			// --list runs a batch across an explicit set of projects. It is
			// mutually exclusive with a positional project name (the -p/--project
			// flag is guarded by MarkFlagsMutuallyExclusive below).
			list := cleanProjectList(freshList)
			if len(list) > 0 {
				if len(args) > 0 {
					return camperrors.New("specify a project with a positional name or --list, not both")
				}

				// Resolve all names up front (fail fast on a bad name), then
				// run the batch cycle with an aggregate summary.
				targets, err := resolveFreshTargets(ctx, campRoot, list)
				if err != nil {
					return err
				}
				header := fmt.Sprintf("Running fresh across %d project(s)...", len(targets))
				return runFreshBatch(ctx, cfg, targets, flags, header)
			}

			// Single project: positional name > --project > cwd auto-detect.
			projectName := freshProjectFlag
			if len(args) > 0 {
				projectName = args[0]
			}

			result, err := project.Resolve(ctx, campRoot, projectName)
			if err != nil {
				var notFound *project.ProjectNotFoundError
				if errors.As(err, &notFound) {
					fmt.Println(ui.Dim("\n" + project.FormatProjectList(notFound.AvailableProjects())))
				}
				return err
			}

			// Resolve settings
			branch := cfg.ResolveFreshBranch(freshBranch, freshNoBranch, result.Name)
			doPrune := !freshNoPrune && cfg.ResolveFreshPrune()
			doPush := !freshNoPush && cfg.ResolveFreshPushUpstream(result.Name)
			followUps := resolveFreshFollowUps(cfg, result.Name, freshNoFollowUp)

			if err := executeFresh(ctx, result.Name, result.Path, freshOptions{
				branch:      branch,
				prune:       doPrune,
				pruneRemote: cfg.ResolveFreshPruneRemote(),
				push:        doPush,
				followUps:   followUps,
				dryRun:      freshDryRun,
			}); err != nil {
				return err
			}

			// Campaign-root workitem sweep runs once, after the project's
			// git-hygiene cycle, never inside executeFresh.
			runCampaignWorkitemSweep(ctx, cfg, freshDryRun)
			return nil
		},
	}

	// Persistent flags are inherited by subcommands (e.g. fresh all --dry-run)
	freshCmd.PersistentFlags().StringVarP(&freshBranch, "branch", "b", "", "Branch to create after syncing (overrides config)")
	freshCmd.PersistentFlags().BoolVar(&freshNoBranch, "no-branch", false, "Skip branch creation even if configured")
	freshCmd.PersistentFlags().BoolVar(&freshNoPush, "no-push", false, "Skip pushing the new branch upstream")
	freshCmd.PersistentFlags().BoolVar(&freshNoPrune, "no-prune", false, "Skip pruning merged branches")
	freshCmd.PersistentFlags().BoolVar(&freshNoFollowUp, "no-follow-up", false, "Skip configured follow-up command workflows")
	freshCmd.PersistentFlags().BoolVarP(&freshDryRun, "dry-run", "n", false, "Preview without making changes")
	freshCmd.Flags().StringVarP(&freshProjectFlag, "project", "p", "", "Project name (auto-detected from cwd)")
	freshCmd.RegisterFlagCompletionFunc("project", completeProjectName)
	freshCmd.Flags().StringSliceVar(&freshList, "list", nil, "Comma-separated set of projects to cycle in one run")
	_ = freshCmd.RegisterFlagCompletionFunc("list", completeProjectName)
	freshCmd.MarkFlagsMutuallyExclusive("project", "list")

	// Add subcommands
	freshCmd.AddCommand(newAllCommand(freshCmd))
	freshCmd.AddCommand(newConfigureCommand())
	freshCmd.AddCommand(newShowWorkflowCommand())

	return freshCmd
}

type freshOptions struct {
	branch      string
	prune       bool
	pruneRemote bool
	push        bool
	followUps   []config.FollowUpConfig
	dryRun      bool
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
	prefix := "  "
	if opts.dryRun {
		fmt.Printf("  %s %s\n", ui.Value(name), freshStepDim.Render("(dry-run)"))
	} else {
		fmt.Printf("  %s\n", ui.Value(name))
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
	if err := maybeReclaimDefaultBranch(ctx, &syncState, opts.dryRun, prefix); err != nil {
		return err
	}

	if syncState.detached {
		note := freshSyncWorktreeNote(syncState)
		if opts.dryRun {
			fmt.Printf("%s── Would fetch %-22s %s\n", prefix, "origin",
				freshStepDim.Render(fmt.Sprintf("(for %s)", syncState.baseRef)))
			fmt.Printf("%s── Would use %-24s %s\n", prefix, syncState.displayRef,
				freshStepDim.Render(note))
		} else {
			if err := git.FetchRemote(ctx, path, "origin"); err != nil {
				return camperrors.Wrap(err, "fetch origin")
			}
			fmt.Printf("%s── Fetch %-28s %s\n", prefix, "origin", freshStepGreen.Render("done"))

			if err := git.CheckoutDetached(ctx, path, syncState.baseRef); err != nil {
				return camperrors.Wrapf(err, "checkout detached %s", syncState.baseRef)
			}
			fmt.Printf("%s── Checkout %-25s %s\n", prefix, syncState.displayRef,
				freshStepGreen.Render("done")+" "+freshStepDim.Render(note))
		}
	} else if currentBranch == defaultBranch && !syncState.reclaimed {
		fmt.Printf("%s── Checkout %-24s %s\n", prefix, defaultBranch, freshStepDim.Render("already on it"))
	} else if opts.dryRun {
		fmt.Printf("%s── Would checkout %-19s %s\n", prefix, defaultBranch,
			freshStepDim.Render(fmt.Sprintf("(currently on %s)", emptyBranchLabel(currentBranch))))
	} else {
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
		fmt.Printf("%s── Checkout %-24s %s\n", prefix, defaultBranch, freshStepGreen.Render("done"))
	}

	// Step 2: Pull (ff-only)
	if !syncState.detached {
		if opts.dryRun {
			fmt.Printf("%s── Would pull (ff-only)\n", prefix)
		} else {
			output, err := git.PullFFOnly(ctx, path)
			if err != nil {
				return camperrors.Wrapf(err, "pull failed — resolve manually")
			}
			detail := "up-to-date"
			if !strings.Contains(output, "Already up to date") {
				detail = "updated"
			}
			fmt.Printf("%s── Pull (ff-only)                  %s\n", prefix, freshStepGreen.Render(detail))
		}
	}

	// Step 3: Prune merged and gone-upstream branches.
	// Prune flow itself refreshes remote tracking (RefreshRemote below),
	// so squash-merged PRs show up here without requiring the user to run
	// 'git fetch --prune' first.
	if opts.prune {
		pruneOpts := prune.Options{
			DryRun:       opts.dryRun,
			Force:        true,  // Skip confirmation — fresh is deliberate
			DiscardDirty: false, // preserve dirty worktrees (new guard); fresh should not destroy uncommitted work
			Remote:       opts.pruneRemote,
			// Refresh even on dry-run: 'git fetch --prune' only updates
			// remote-tracking refs, not the worktree, so the dry-run
			// preview must include it or squash-merged branches stay
			// invisible until the user fetches manually.
			BaseRef:       syncState.baseRef,
			RefreshRemote: true,
		}
		// Reclaiming the default branch detaches its former worktree. Preserve
		// that exact worktree during this prune pass: fresh created the detached
		// state as a safe branch handoff, so it must not immediately classify
		// the worktree as merged and remove it.
		if syncState.reclaimed && syncState.worktreePath != "" {
			pruneOpts.PreserveDetachedWorktrees = []string{syncState.worktreePath}
		}
		pr := prune.Execute(ctx, name, path, pruneOpts)
		if pr.Error != "" {
			return camperrors.Wrapf(errors.New(pr.Error), "prune merged branches")
		}

		deletedNames := pruneResultNames(pr.Results, prune.StatusDeleted, prune.StatusWouldDelete)
		removedWorktrees := pruneResultCount(pr.Results, prune.StatusWorktreeRemoved, prune.StatusWorktreeWouldRemove)

		switch {
		case len(deletedNames) > 0 || removedWorktrees > 0:
			action := "deleted"
			worktreeAction := "removed"
			style := freshStepGreen
			if opts.dryRun {
				action = "would delete"
				worktreeAction = "would remove"
				style = freshStepDim
			}
			detail := fmt.Sprintf("%s: %s", action, strings.Join(deletedNames, ", "))
			if len(deletedNames) == 0 {
				detail = "removed merged detached worktrees"
				if opts.dryRun {
					detail = "would remove merged detached worktrees"
				}
			}
			if removedWorktrees > 0 {
				detail = fmt.Sprintf("%s; %s %d worktree(s)", detail, worktreeAction, removedWorktrees)
			}
			fmt.Printf("%s── Prune merged branches           %s\n", prefix, style.Render(detail))
		default:
			fmt.Printf("%s── Prune merged branches           %s\n", prefix, freshStepDim.Render("nothing to prune"))
		}
		if pr.Pruned > 0 {
			detail := fmt.Sprintf("%d stale refs", pr.Pruned)
			style := freshStepGreen
			if opts.dryRun {
				detail = "would prune stale refs"
				style = freshStepDim
			}
			fmt.Printf("%s── Prune remote tracking refs      %s\n", prefix, style.Render(detail))
		}
	}

	// Step 4: Create branch (optional)
	branchCreated := false
	if opts.branch != "" {
		if git.BranchExists(ctx, path, opts.branch) {
			fmt.Fprintf(os.Stderr, "%s── Branch %-25s %s\n", prefix, opts.branch,
				freshStepDim.Render(fmt.Sprintf("already exists, staying on %s", syncState.displayRef)))
		} else if opts.dryRun {
			fmt.Printf("%s── Would create branch %-12s %s\n", prefix, opts.branch,
				freshStepDim.Render(fmt.Sprintf("(from %s)", syncState.baseRef)))
		} else {
			if err := git.CreateBranch(ctx, path, opts.branch); err != nil {
				return camperrors.Wrapf(err, "create branch %s", opts.branch)
			} else {
				branchCreated = true
				fmt.Printf("%s── Create branch %-19s %s\n", prefix, opts.branch, freshStepGreen.Render("done"))
			}
		}
	}

	// Step 5: Push upstream (optional)
	if branchCreated && opts.push {
		if opts.dryRun {
			fmt.Printf("%s── Would push %s -> origin\n", prefix, opts.branch)
		} else {
			if err := git.PushSetUpstream(ctx, path, opts.branch); err != nil {
				return camperrors.Wrapf(err, "push %s to origin", opts.branch)
			} else {
				fmt.Printf("%s── Push %-28s %s\n", prefix, opts.branch+" -> origin", freshStepGreen.Render("done"))
			}
		}
	}

	// Step 6: Run configured follow-up command workflows. Only reachable once
	// every prior step has succeeded, so a failed sync/prune/branch cycle
	// never triggers follow-ups.
	if err := runFreshFollowUps(ctx, path, opts.followUps, opts.dryRun); err != nil {
		return err
	}

	// Summary
	fmt.Println()
	if opts.dryRun {
		fmt.Println(freshStepDim.Render("  (dry-run — no changes made)"))
	} else if branchCreated {
		fmt.Printf("  %s Ready to work on %s.\n", freshStepGreen.Render("Fresh!"), ui.Value(opts.branch))
	} else if syncState.detached {
		fmt.Printf("  %s Synced to %s.\n", freshStepGreen.Render("Fresh!"), ui.Value(syncState.displayRef))
	} else {
		fmt.Printf("  %s Synced to %s.\n", freshStepGreen.Render("Fresh!"), ui.Value(defaultBranch))
	}

	return nil
}

func resolveFreshSyncState(ctx context.Context, path, defaultBranch string) (freshSyncState, error) {
	state := freshSyncState{
		defaultBranch: defaultBranch,
		baseRef:       defaultBranch,
		displayRef:    defaultBranch,
	}

	// Scan for another worktree holding the default branch. When this path is
	// detached or on a feature branch, main often sits stuck on a finished
	// worktree left over from camp project worktree add --start-point main.
	entry, err := findBranchInOtherWorktree(ctx, path, defaultBranch)
	if err != nil {
		return state, err
	}
	if entry == nil {
		return state, nil
	}

	// Prefer reclaim + normal checkout; until reclaim succeeds, plan for a
	// detached origin/<default> sync so fresh does not hard-fail.
	state.baseRef = "origin/" + defaultBranch
	state.displayRef = state.baseRef + " (detached)"
	state.detached = true
	state.worktreePath = entry.Path

	return state, nil
}

// maybeReclaimDefaultBranch frees the default branch from another clean
// worktree when possible. Dry-run still inspects dirtiness (status is
// non-mutating) so the printed plan matches a real run. On success, mutates
// state so the primary path checks out the real default branch; on dirty
// skip, leaves the detached-origin fallback in place.
func maybeReclaimDefaultBranch(ctx context.Context, state *freshSyncState, dryRun bool, prefix string) error {
	if state == nil || state.worktreePath == "" {
		return nil
	}

	var (
		reclaimed bool
		err       error
	)
	if dryRun {
		reclaimed, err = canReclaimDefaultBranchWorktree(ctx, state.worktreePath)
	} else {
		reclaimed, err = reclaimDefaultBranchWorktree(ctx, state.worktreePath)
	}
	if err != nil {
		return err
	}
	if reclaimed {
		applyReclaimDecision(state)
	}
	printReclaimStep(prefix, *state, reclaimed, dryRun)
	return nil
}

// applyReclaimDecision flips a detached-fallback plan to a normal default-branch
// checkout after the occupying worktree has been (or would be) freed.
func applyReclaimDecision(state *freshSyncState) {
	if state == nil {
		return
	}
	state.reclaimed = true
	state.detached = false
	state.baseRef = state.defaultBranch
	state.displayRef = state.defaultBranch
}

// reclaimStepDetail returns the step label and dim detail for a free/reclaim
// line. Pure so dry-run and real paths cannot drift and unit tests can lock copy.
func reclaimStepDetail(branch, worktreePath string, reclaimed, dryRun bool) (label, detail string) {
	base := filepath.Base(worktreePath)
	if dryRun {
		label = fmt.Sprintf("Would free %-23s", branch)
		if reclaimed {
			detail = fmt.Sprintf("(detach clean worktree %s)", base)
		} else {
			detail = fmt.Sprintf("skipped · %s has uncommitted changes; would sync detached", base)
		}
		return label, detail
	}
	label = fmt.Sprintf("Free %-28s", branch)
	if reclaimed {
		detail = fmt.Sprintf("(detached %s so %s is free here)", base, branch)
	} else {
		detail = fmt.Sprintf("skipped · %s has uncommitted changes; syncing detached", base)
	}
	return label, detail
}

func printReclaimStep(prefix string, state freshSyncState, reclaimed, dryRun bool) {
	label, detail := reclaimStepDetail(state.defaultBranch, state.worktreePath, reclaimed, dryRun)
	if dryRun {
		fmt.Printf("%s── %s %s\n", prefix, label, freshStepDim.Render(detail))
		return
	}
	if reclaimed {
		fmt.Printf("%s── %s %s\n", prefix, label,
			freshStepGreen.Render("done")+" "+freshStepDim.Render(detail))
		return
	}
	// Dirty worktree: keep detached-sync fallback, but make the reason obvious.
	fmt.Printf("%s── %s %s\n", prefix, label, freshStepDim.Render(detail))
}

// findBranchInOtherWorktree returns the first worktree (other than path) that
// has branch checked out. path is the project working tree being freshened.
func findBranchInOtherWorktree(ctx context.Context, path, branch string) (*worktree.GitWorktreeEntry, error) {
	entries, err := worktree.NewGitWorktree(path).List(ctx)
	if err != nil {
		return nil, err
	}

	self := worktreeToplevel(ctx, path)
	for _, entry := range entries {
		if entry.Branch != branch {
			continue
		}
		// Skip the worktree that is path itself (path form can differ for
		// submodules: projects/camp vs .git/modules/projects/camp).
		if sameWorktreePath(self, worktreeToplevel(ctx, entry.Path)) {
			continue
		}
		entryCopy := entry
		return &entryCopy, nil
	}

	return nil, nil
}

// canReclaimDefaultBranchWorktree reports whether occupyingPath is clean
// enough to detach without losing work (used by dry-run planning).
func canReclaimDefaultBranchWorktree(ctx context.Context, occupyingPath string) (bool, error) {
	dirty, err := git.HasNonSubmoduleChanges(ctx, occupyingPath)
	if err != nil {
		return false, camperrors.Wrapf(err, "check worktree %s for uncommitted changes", occupyingPath)
	}
	return !dirty, nil
}

// reclaimDefaultBranchWorktree detaches a clean worktree so its branch ref
// can be checked out elsewhere. Returns (false, nil) when the worktree has
// uncommitted changes and must not be touched; (true, nil) after a successful
// detach.
func reclaimDefaultBranchWorktree(ctx context.Context, occupyingPath string) (bool, error) {
	ok, err := canReclaimDefaultBranchWorktree(ctx, occupyingPath)
	if err != nil || !ok {
		return ok, err
	}
	// Detach at HEAD: keeps the same commit checked out, frees the branch name.
	if err := git.CheckoutDetached(ctx, occupyingPath, "HEAD"); err != nil {
		return false, camperrors.Wrapf(err, "detach worktree %s to free its branch", occupyingPath)
	}
	return true, nil
}

func worktreeToplevel(ctx context.Context, path string) string {
	out, err := git.Output(ctx, path, "rev-parse", "--show-toplevel")
	if err != nil || strings.TrimSpace(out) == "" {
		abs, absErr := filepath.Abs(path)
		if absErr != nil {
			return filepath.Clean(path)
		}
		return abs
	}
	return filepath.Clean(strings.TrimSpace(out))
}

func sameWorktreePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if a == b {
		return true
	}
	// Resolve symlinks when possible (submodule / worktree path forms).
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	if errA == nil && errB == nil {
		return ra == rb
	}
	return false
}

func emptyBranchLabel(branch string) string {
	if branch == "" {
		return "detached HEAD"
	}
	return branch
}

func isBranchInUseByWorktree(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already used by worktree") ||
		strings.Contains(msg, "is already checked out at")
}

func freshSyncWorktreeNote(state freshSyncState) string {
	if state.worktreePath == "" {
		return ""
	}
	return fmt.Sprintf("(%s still in %s)", state.defaultBranch, filepath.Base(state.worktreePath))
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
