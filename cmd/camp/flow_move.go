//go:build dev

package main

import (
	"context"
	"fmt"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"path/filepath"

	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/workflow"
	"github.com/spf13/cobra"
)

var (
	flowMoveReason   string
	flowMoveForce    bool
	flowMoveCommit   bool
	flowMoveNoCommit bool
)

var flowMoveCmd = &cobra.Command{
	Use:   "move <item> <status>",
	Short: "Move an item to a new status",
	Long: `Move an item from its current status to a new status.

The item is moved from wherever it currently exists to the specified status.
Transitions are validated against the workflow schema unless --force is used.

Auto-commit behavior is controlled by .workflow.yaml auto_commit settings.
Use --commit to force a commit or --no-commit to skip it.

Examples:
  camp flow move project-1 ready             Move to ready/
  camp flow move old-project dungeon/completed   Move to dungeon/completed/
  camp flow move project-1 ready --reason "Ready for review"
  camp flow move project-1 active --force    Force move (skip validation)
  camp flow move project-1 ready --commit    Force auto-commit`,
	Args: cobra.ExactArgs(2),
	RunE: runFlowMove,
}

func init() {
	flowCmd.AddCommand(flowMoveCmd)
	flowMoveCmd.Flags().StringVarP(&flowMoveReason, "reason", "r", "", "reason for the move")
	flowMoveCmd.Flags().BoolVarP(&flowMoveForce, "force", "f", false, "force move (skip transition validation)")
	flowMoveCmd.Flags().BoolVar(&flowMoveCommit, "commit", false, "force auto-commit after move")
	flowMoveCmd.Flags().BoolVar(&flowMoveNoCommit, "no-commit", false, "skip auto-commit even if enabled in config")
}

func runFlowMove(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	item := args[0]
	to := args[1]

	cwd, err := getCwd()
	if err != nil {
		return err
	}

	svc := workflow.NewService(cwd)
	if err := svc.LoadSchema(ctx); err != nil {
		return err
	}

	// V2 shortcut: "active" → "." for root directory
	if svc.Schema().Version == 2 && to == "active" {
		to = "."
	}

	result, err := svc.Move(ctx, item, to, workflow.MoveOptions{
		Reason: flowMoveReason,
		Force:  flowMoveForce,
	})
	if err != nil {
		return err
	}

	ui.Success(fmt.Sprintf("Moved %s: %s → %s", result.Item, result.From, result.To))
	if result.Reason != "" {
		fmt.Printf("Reason: %s\n", result.Reason)
	}

	// Auto-commit logic
	if err := maybeAutoCommit(ctx, svc, cwd, result); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: auto-commit failed: %v\n", err)
	}

	return nil
}

// maybeAutoCommit handles the auto-commit logic after a successful move.
func maybeAutoCommit(ctx context.Context, svc *workflow.Service, cwd string, result *workflow.MoveResult) error {
	if flowMoveNoCommit {
		return nil
	}

	shouldCommit := flowMoveCommit
	if !shouldCommit && !flowMoveNoCommit {
		// Ensure schema is loaded
		_ = svc.LoadSchema(ctx)
		if schema := svc.Schema(); schema != nil {
			shouldCommit = schema.AutoCommit.ShouldAutoCommit(result.From, result.To)
		}
	}

	if !shouldCommit {
		return nil
	}

	transition := workflow.Transition{
		Item: result.Item,
		From: result.From,
		To:   result.To,
	}

	// Stage the old and new paths
	oldPath := filepath.Join(cwd, result.From, result.Item)
	newPath := filepath.Join(cwd, result.To, result.Item)

	if err := git.Stage(ctx, cwd, []string{oldPath, newPath}); err != nil {
		return camperrors.Wrap(err, "stage files")
	}

	if err := git.Commit(ctx, cwd, &git.CommitOptions{
		Message: transition.CommitMessage(),
	}); err != nil {
		return camperrors.Wrap(err, "commit")
	}

	fmt.Printf("Committed: %s\n", transition.CommitMessage())
	return nil
}
