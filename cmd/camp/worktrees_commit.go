package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/obediencecorp/camp/internal/campaign"
	"github.com/obediencecorp/camp/internal/config"
	"github.com/obediencecorp/camp/internal/git"
	"github.com/obediencecorp/camp/internal/paths"
	"github.com/obediencecorp/camp/internal/ui"
	"github.com/obediencecorp/camp/internal/worktree"
	"github.com/spf13/cobra"
)

var (
	wtCommitMessage string
	wtCommitAll     bool
	wtCommitAmend   bool
)

var worktreesCommitCmd = &cobra.Command{
	Use:   "commit",
	Short: "Commit changes in a worktree",
	Long: `Commit changes within the current worktree.

Auto-detects the worktree from your current directory.

Examples:
  # From within a worktree
  cd projects/worktrees/my-api/feature-auth
  camp worktrees commit -m "Add login feature"

  # With all changes staged
  camp worktrees commit -m "Update deps" --all

  # Amend previous commit
  camp worktrees commit --amend -m "Fix typo"`,
	RunE: runWorktreesCommit,
}

func init() {
	worktreesCmd.AddCommand(worktreesCommitCmd)

	worktreesCommitCmd.Flags().StringVarP(&wtCommitMessage, "message", "m", "",
		"Commit message")
	worktreesCommitCmd.Flags().BoolVarP(&wtCommitAll, "all", "a", true,
		"Stage all changes before committing")
	worktreesCommitCmd.Flags().BoolVar(&wtCommitAmend, "amend", false,
		"Amend the previous commit")
}

func runWorktreesCommit(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return fmt.Errorf("not in a campaign: %w", err)
	}

	cfg, err := config.LoadCampaignConfig(ctx, campRoot)
	if err != nil {
		return fmt.Errorf("failed to load campaign config: %w", err)
	}

	resolver := paths.NewResolver(campRoot, cfg.Paths())
	detector := worktree.NewDetector(resolver)

	// Detect worktree from cwd
	wtCtx, err := detector.DetectFromCwd()
	if err != nil {
		return fmt.Errorf("not inside a worktree: %w", err)
	}

	// Display which worktree
	fmt.Printf("Worktree: %s/%s\n", ui.Value(wtCtx.Project), ui.Value(wtCtx.WorktreeName))
	fmt.Printf("Branch:   %s\n", ui.Value(wtCtx.Branch))
	fmt.Println()

	// Create executor for the worktree
	executor, err := git.NewExecutor(wtCtx.WorktreePath)
	if err != nil {
		return fmt.Errorf("failed to initialize git: %w", err)
	}

	// Get commit message - prompt if not provided
	message := wtCommitMessage
	if message == "" && !wtCommitAmend {
		message, err = ui.PromptCommitMessageSimple(ctx, executor)
		if err != nil {
			return fmt.Errorf("prompt failed: %w", err)
		}
		if message == "" {
			return fmt.Errorf("commit cancelled")
		}
	}

	// Stage if requested
	if wtCommitAll {
		fmt.Println(ui.Info("Staging changes..."))
		if err := executor.StageAll(ctx); err != nil {
			return fmt.Errorf("failed to stage: %w", err)
		}
	}

	// Check for changes
	hasChanges, err := executor.HasChanges(ctx)
	if err != nil {
		return err
	}
	if !hasChanges && !wtCommitAmend {
		fmt.Println(ui.Success("Nothing to commit in worktree"))
		return nil
	}

	// Show what's staged
	showStagedSummary(ctx, wtCtx.WorktreePath)

	// Commit
	fmt.Println(ui.Info("Committing changes..."))
	opts := &git.CommitOptions{
		Message: message,
		Amend:   wtCommitAmend,
	}
	if err := executor.Commit(ctx, opts); err != nil {
		if errors.Is(err, git.ErrNoChanges) {
			fmt.Println(ui.Success("Nothing to commit"))
			return nil
		}
		return fmt.Errorf("commit failed: %w", err)
	}

	fmt.Println(ui.Success("Changes committed in worktree"))

	return nil
}
