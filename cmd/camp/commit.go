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

Examples:
  camp commit -m "Add new feature"
  camp commit --amend -m "Fix typo"
  camp commit -a -m "Stage and commit all"`,
	RunE: runCommit,
}

var (
	commitMessage string
	commitAll     bool
	commitAmend   bool
)

func init() {
	commitCmd.Flags().StringVarP(&commitMessage, "message", "m", "", "Commit message (required)")
	commitCmd.Flags().BoolVarP(&commitAll, "all", "a", true, "Stage all changes before committing")
	commitCmd.Flags().BoolVar(&commitAmend, "amend", false, "Amend the previous commit")

	rootCmd.AddCommand(commitCmd)
	commitCmd.GroupID = "project"
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

	// Create executor
	executor, err := git.NewExecutor(campRoot)
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
	showStagedSummary(ctx, campRoot)

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

	fmt.Println(ui.Success("✓ Changes committed successfully"))
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
