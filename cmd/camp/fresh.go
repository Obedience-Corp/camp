//go:build dev

package main

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
	"github.com/Obedience-Corp/camp/internal/ui"
)

var freshCmd = &cobra.Command{
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
	RunE:              runFresh,
	ValidArgsFunction: completeProjectName,
}

var (
	freshBranch      string
	freshNoBranch    bool
	freshNoPush      bool
	freshNoPrune     bool
	freshDryRun      bool
	freshProjectFlag string
)

func init() {
	// Persistent flags are inherited by subcommands (e.g. fresh all --dry-run)
	freshCmd.PersistentFlags().StringVarP(&freshBranch, "branch", "b", "", "Branch to create after syncing (overrides config)")
	freshCmd.PersistentFlags().BoolVar(&freshNoBranch, "no-branch", false, "Skip branch creation even if configured")
	freshCmd.PersistentFlags().BoolVar(&freshNoPush, "no-push", false, "Skip pushing the new branch upstream")
	freshCmd.PersistentFlags().BoolVar(&freshNoPrune, "no-prune", false, "Skip pruning merged branches")
	freshCmd.PersistentFlags().BoolVarP(&freshDryRun, "dry-run", "n", false, "Preview without making changes")
	freshCmd.Flags().StringVarP(&freshProjectFlag, "project", "p", "", "Project name (auto-detected from cwd)")
	freshCmd.RegisterFlagCompletionFunc("project", completeProjectName)

	rootCmd.AddCommand(freshCmd)
	freshCmd.GroupID = "git"
}

// freshStepStyle defines styles for step output.
var (
	freshStepDim   = lipgloss.NewStyle().Foreground(ui.DimColor)
	freshStepGreen = lipgloss.NewStyle().Foreground(ui.SuccessColor)
	freshStepRed   = lipgloss.NewStyle().Foreground(ui.ErrorColor)
)

func runFresh(cmd *cobra.Command, args []string) error {
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
}

type freshOptions struct {
	branch      string
	prune       bool
	pruneRemote bool
	push        bool
	dryRun      bool
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
	if currentBranch == defaultBranch {
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

	// Step 3: Prune merged branches
	if opts.prune {
		if opts.dryRun {
			merged, _ := git.MergedBranches(ctx, path)
			if len(merged) > 0 {
				fmt.Printf("%s── Would prune %d branch(es)        %s\n", prefix,
					len(merged), freshStepDim.Render(strings.Join(merged, ", ")))
			} else {
				fmt.Printf("%s── Prune                           %s\n", prefix, freshStepDim.Render("nothing to prune"))
			}
		} else {
			pruneOpts := PruneOptions{
				Force:  true, // Skip confirmation — fresh is deliberate
				Remote: opts.pruneRemote,
			}
			pr := executePrune(ctx, name, path, pruneOpts)
			deleted := 0
			var deletedNames []string
			for _, r := range pr.Results {
				if r.Status == pruneStatusDeleted {
					deleted++
					deletedNames = append(deletedNames, r.Branch)
				}
			}
			if deleted > 0 {
				fmt.Printf("%s── Prune merged branches           %s\n", prefix,
					freshStepGreen.Render(fmt.Sprintf("deleted: %s", strings.Join(deletedNames, ", "))))
			} else {
				fmt.Printf("%s── Prune merged branches           %s\n", prefix, freshStepDim.Render("nothing to prune"))
			}
			if pr.Pruned > 0 {
				fmt.Printf("%s── Prune remote tracking refs      %s\n", prefix,
					freshStepGreen.Render(fmt.Sprintf("%d stale refs", pr.Pruned)))
			}
		}
	}

	// Step 4: Create branch (optional)
	branchCreated := false
	if opts.branch != "" {
		if git.BranchExists(ctx, path, opts.branch) {
			fmt.Fprintf(os.Stderr, "%s── Branch %-25s %s\n", prefix, opts.branch,
				freshStepDim.Render(fmt.Sprintf("already exists, staying on %s", defaultBranch)))
		} else if opts.dryRun {
			fmt.Printf("%s── Would create branch %s\n", prefix, opts.branch)
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
	} else {
		fmt.Printf("  %s Synced to %s.\n", freshStepGreen.Render("Fresh!"), ui.Value(defaultBranch))
	}

	return nil
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
	if isRebaseInProgress(ctx, path) {
		return fmt.Errorf("rebase in progress — complete or abort first")
	}

	// Check for uncommitted changes
	hasChanges, err := git.HasChanges(ctx, path)
	if err != nil {
		return camperrors.Wrap(err, "checking for uncommitted changes")
	}
	if hasChanges {
		return fmt.Errorf("uncommitted changes — commit or stash first")
	}

	return nil
}
