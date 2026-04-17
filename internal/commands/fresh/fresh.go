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
	freshStepRed   = lipgloss.NewStyle().Foreground(ui.ErrorColor)
)

// NewFreshCommand creates and returns the fresh cobra command with all subcommands.
func NewFreshCommand() *cobra.Command {
	var (
		freshBranch      string
		freshNoBranch    bool
		freshNoPush      bool
		freshNoPrune     bool
		freshDryRun      bool
		freshProjectFlag string
	)

	freshCmd := &cobra.Command{
		Use:   "fresh [project-name]",
		Short: "Post-merge branch cycling: sync to default branch and optionally create a new working branch",
		Long: `Reset a project to a fresh state after merging a PR.

Performs the post-merge cycle: checkout default branch, pull latest,
prune merged branches, and optionally create a new working branch.

Auto-detects the current project from your working directory,
or accepts a project name as a positional argument.

Without configuration, syncs to the default branch and prunes.
Configure .campaign/settings/fresh.yaml to set a default working branch.

Examples:
  camp fresh                         # Sync current project (checkout default, pull, prune)
  camp fresh --branch develop        # Sync and create develop branch
  camp fresh camp -b feat/new-thing  # Sync camp project, create feature branch
  camp fresh --no-prune              # Sync without pruning
  camp fresh --dry-run               # Preview what would happen`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeProjectName,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			campRoot, err := campaign.DetectCached(ctx)
			if err != nil {
				return camperrors.Wrap(err, "not in a campaign")
			}

			// Resolve project: positional arg > flag > cwd
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

			// Load fresh config
			cfg, err := config.LoadFreshConfig(ctx, campRoot)
			if err != nil {
				return camperrors.Wrap(err, "loading fresh config")
			}

			// Resolve settings
			branch := cfg.ResolveFreshBranch(freshBranch, freshNoBranch, result.Name)
			doPrune := !freshNoPrune && cfg.ResolveFreshPrune()
			doPush := !freshNoPush && cfg.ResolveFreshPushUpstream(result.Name)

			return executeFresh(ctx, result.Name, result.Path, freshOptions{
				branch:      branch,
				prune:       doPrune,
				pruneRemote: cfg.ResolveFreshPruneRemote(),
				push:        doPush,
				dryRun:      freshDryRun,
			})
		},
	}

	// Persistent flags are inherited by subcommands (e.g. fresh all --dry-run)
	freshCmd.PersistentFlags().StringVarP(&freshBranch, "branch", "b", "", "Branch to create after syncing (overrides config)")
	freshCmd.PersistentFlags().BoolVar(&freshNoBranch, "no-branch", false, "Skip branch creation even if configured")
	freshCmd.PersistentFlags().BoolVar(&freshNoPush, "no-push", false, "Skip pushing the new branch upstream")
	freshCmd.PersistentFlags().BoolVar(&freshNoPrune, "no-prune", false, "Skip pruning merged branches")
	freshCmd.PersistentFlags().BoolVarP(&freshDryRun, "dry-run", "n", false, "Preview without making changes")
	freshCmd.Flags().StringVarP(&freshProjectFlag, "project", "p", "", "Project name (auto-detected from cwd)")
	freshCmd.RegisterFlagCompletionFunc("project", completeProjectName)

	// Add subcommand
	freshCmd.AddCommand(newAllCommand(freshCmd))

	return freshCmd
}

type freshOptions struct {
	branch      string
	prune       bool
	pruneRemote bool
	push        bool
	dryRun      bool
}

type freshSyncState struct {
	defaultBranch string
	baseRef       string
	displayRef    string
	detached      bool
	worktreePath  string
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
		return fmt.Errorf("could not determine default branch for %s", name)
	}

	currentBranch := git.CurrentBranch(ctx, path)
	syncState, err := resolveFreshSyncState(ctx, path, currentBranch, defaultBranch)
	if err != nil {
		return camperrors.Wrap(err, "prepare fresh sync state")
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
	} else if currentBranch == defaultBranch {
		fmt.Printf("%s── Checkout %-24s %s\n", prefix, defaultBranch, freshStepDim.Render("already on it"))
	} else if opts.dryRun {
		fmt.Printf("%s── Would checkout %-19s %s\n", prefix, defaultBranch,
			freshStepDim.Render(fmt.Sprintf("(currently on %s)", currentBranch)))
	} else {
		if err := git.Checkout(ctx, path, defaultBranch); err != nil {
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

	// Step 3: Prune merged branches
	if opts.prune {
		pruneOpts := prune.Options{
			DryRun:  opts.dryRun,
			Force:   true, // Skip confirmation — fresh is deliberate
			Remote:  opts.pruneRemote,
			BaseRef: syncState.baseRef,
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

func resolveFreshSyncState(ctx context.Context, path, currentBranch, defaultBranch string) (freshSyncState, error) {
	state := freshSyncState{
		defaultBranch: defaultBranch,
		baseRef:       defaultBranch,
		displayRef:    defaultBranch,
	}

	entry, err := findBranchInOtherWorktree(ctx, path, currentBranch, defaultBranch)
	if err != nil {
		return state, err
	}
	if entry == nil {
		return state, nil
	}

	state.baseRef = "origin/" + defaultBranch
	state.displayRef = state.baseRef + " (detached)"
	state.detached = true
	state.worktreePath = entry.Path

	return state, nil
}

func findBranchInOtherWorktree(ctx context.Context, path, currentBranch, branch string) (*worktree.GitWorktreeEntry, error) {
	if currentBranch == branch {
		return nil, nil
	}

	entries, err := worktree.NewGitWorktree(path).List(ctx)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.Branch != branch {
			continue
		}
		entryCopy := entry
		return &entryCopy, nil
	}

	return nil, nil
}

func freshSyncWorktreeNote(state freshSyncState) string {
	if state.worktreePath == "" {
		return ""
	}
	return fmt.Sprintf("(%s in %s)", state.defaultBranch, filepath.Base(state.worktreePath))
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
		return fmt.Errorf("merge in progress — complete or abort first")
	}

	// Check for rebase in progress
	if git.IsRebaseInProgress(ctx, path) {
		return fmt.Errorf("rebase in progress — complete or abort first")
	}

	// Check for uncommitted changes
	hasChanges, err := git.HasNonSubmoduleChanges(ctx, path)
	if err != nil {
		return camperrors.Wrap(err, "checking for uncommitted changes")
	}
	if hasChanges {
		return fmt.Errorf("uncommitted changes — commit or stash first")
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

// getFreshFlags extracts persistent flags from the fresh command.
// These are stored on the parent command (freshCmd) and accessed here.
func getFreshFlags(freshCmd *cobra.Command) (branch string, noBranch, noPush, noPrune, dryRun bool) {
	branch, _ = freshCmd.PersistentFlags().GetString("branch")
	noBranch, _ = freshCmd.PersistentFlags().GetBool("no-branch")
	noPush, _ = freshCmd.PersistentFlags().GetBool("no-push")
	noPrune, _ = freshCmd.PersistentFlags().GetBool("no-prune")
	dryRun, _ = freshCmd.PersistentFlags().GetBool("dry-run")
	return
}
