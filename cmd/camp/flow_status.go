package main

import (
	"context"
	"fmt"

	"github.com/obediencecorp/camp/internal/workflow"
	"github.com/spf13/cobra"
)

var flowStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show workflow statistics",
	Long: `Show workflow statistics including item counts per status.

Displays the workflow name, location, and counts for each status directory.

Examples:
  camp flow status            Show workflow statistics`,
	RunE: runFlowStatus,
}

func init() {
	flowCmd.AddCommand(flowStatusCmd)
}

func runFlowStatus(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	cwd, err := getCwd()
	if err != nil {
		return err
	}

	svc := workflow.NewService(cwd)
	if err := svc.LoadSchema(ctx); err != nil {
		return err
	}

	schema := svc.Schema()
	fmt.Printf("Workflow: %s\n", schema.Name)
	fmt.Printf("Location: %s\n", svc.Root())
	fmt.Println()

	// List each status with item count
	for _, status := range schema.AllDirectories() {
		result, err := svc.List(ctx, status, workflow.ListOptions{})
		if err != nil {
			continue
		}
		fmt.Printf("  %-20s %d items\n", status+"/", len(result.Items))
	}

	return nil
}
