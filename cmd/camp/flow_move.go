package main

import (
	"context"
	"fmt"

	"github.com/obediencecorp/camp/internal/ui"
	"github.com/obediencecorp/camp/internal/workflow"
	"github.com/spf13/cobra"
)

var (
	flowMoveReason string
	flowMoveForce  bool
)

var flowMoveCmd = &cobra.Command{
	Use:   "move <item> <status>",
	Short: "Move an item to a new status",
	Long: `Move an item from its current status to a new status.

The item is moved from wherever it currently exists to the specified status.
Transitions are validated against the workflow schema unless --force is used.

Examples:
  camp flow move project-1 ready             Move to ready/
  camp flow move old-project dungeon/completed   Move to dungeon/completed/
  camp flow move project-1 ready --reason "Ready for review"
  camp flow move project-1 active --force    Force move (skip validation)`,
	Args: cobra.ExactArgs(2),
	RunE: runFlowMove,
}

func init() {
	flowCmd.AddCommand(flowMoveCmd)
	flowMoveCmd.Flags().StringVarP(&flowMoveReason, "reason", "r", "", "reason for the move")
	flowMoveCmd.Flags().BoolVarP(&flowMoveForce, "force", "f", false, "force move (skip transition validation)")
}

func runFlowMove(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	item := args[0]
	to := args[1]

	cwd, err := getCwd()
	if err != nil {
		return err
	}

	svc := workflow.NewService(cwd)
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

	return nil
}
