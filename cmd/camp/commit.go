package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"

	"github.com/obediencecorp/camp/internal/campaign"
	"github.com/obediencecorp/camp/internal/git"
	"github.com/obediencecorp/camp/internal/ui"
	"github.com/spf13/cobra"
)

var commitCmd = &cobra.Command{
	Use:   "commit",
	Short: "Commit changes in the campaign root",
	Long: `Commit changes in the campaign root directory.

Automatically stages all changes and creates a commit. Handles
stale lock files from crashed processes.

Use --sub to commit in the submodule detected from your current directory.
Use -p/--project to commit in a specific project (e.g., -p projects/camp).

Examples:
  camp commit -m "Add new feature"
  camp commit --amend -m "Fix typo"
  camp commit -a -m "Stage and commit all"
  camp commit --sub -m "Commit in current submodule"
  camp commit -p projects/camp -m "Commit in camp project"`,
	RunE: runCommit,
}

var (
	commitMessage string
	commitAll     bool
	commitAmend   bool
	commitSub     bool
	commitProject string
)

func init() {
	commitCmd.Flags().StringVarP(&commitMessage, "message", "m", "", "Commit message (required)")
	commitCmd.Flags().BoolVarP(&commitAll, "all", "a", true, "Stage all changes before committing")
	commitCmd.Flags().BoolVar(&commitAmend, "amend", false, "Amend the previous commit")
	commitCmd.Flags().BoolVar(&commitSub, "sub", false, "Operate on the submodule detected from current directory")
	commitCmd.Flags().StringVarP(&commitProject, "project", "p", "", "Operate on a specific project/submodule path")

	rootCmd.AddCommand(commitCmd)
	commitCmd.GroupID = "campaign"

	// Register completion for --project flag
	commitCmd.RegisterFlagCompletionFunc("project", completeProjectFlag)
}

// completeProjectFlag provides tab completion for the --project flag.
func completeProjectFlag(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	paths, err := git.ListSubmodulePathsFiltered(ctx, campRoot, toComplete)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	return paths, cobra.ShellCompDirectiveNoFileComp
}

func runCommit(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	// Find campaign root
	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return fmt.Errorf("not in a campaign: %w", err)
	}

	// Resolve target repository
	target, err := git.ResolveTarget(ctx, campRoot, commitSub, commitProject)
	if err != nil {
		return fmt.Errorf("failed to resolve target: %w", err)
	}

	if target.IsSubmodule {
		fmt.Println(ui.Info(fmt.Sprintf("Operating on submodule: %s", target.Name)))
	}

	// Create executor
	executor, err := git.NewExecutor(target.Path)
	if err != nil {
		return fmt.Errorf("failed to initialize git: %w", err)
	}

	// Get commit message - prompt if not provided
	message := commitMessage
	if message == "" && !commitAmend {
		var promptErr error
		message, promptErr = ui.PromptCommitMessageSimple(ctx, executor)
		if promptErr != nil {
			return fmt.Errorf("prompt failed: %w", promptErr)
		}
		if message == "" {
			return fmt.Errorf("commit cancelled")
		}
	}

	// Stage if requested
	if commitAll {
		fmt.Println(ui.Info("Staging changes..."))
		if err := executor.StageAll(ctx); err != nil {
			return fmt.Errorf("failed to stage: %w", err)
		}
	}

	// Show what will be committed
	showStagedSummary(ctx, target.Path)

	// Check for changes
	hasChanges, err := executor.HasChanges(ctx)
	if err != nil {
		return err
	}
	if !hasChanges && !commitAmend {
		fmt.Println(ui.Success("Nothing to commit, working tree clean"))
		return nil
	}

	// Perform commit
	fmt.Println(ui.Info("Committing changes..."))
	opts := &git.CommitOptions{
		Message: message,
		Amend:   commitAmend,
	}

	if err := executor.Commit(ctx, opts); err != nil {
		if errors.Is(err, git.ErrNoChanges) {
			fmt.Println(ui.Success("Nothing to commit"))
			return nil
		}
		return fmt.Errorf("commit failed: %w", err)
	}

	fmt.Println(ui.Success("Changes committed successfully"))
	return nil
}

// showStagedSummary displays what will be committed.
func showStagedSummary(ctx context.Context, repoPath string) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath,
		"diff", "--cached", "--stat")
	output, err := cmd.Output()
	if err != nil {
		return // Non-fatal
	}

	if len(output) > 0 {
		fmt.Println("\nChanges to be committed:")
		fmt.Print(string(output))
		fmt.Println()
	}
}
