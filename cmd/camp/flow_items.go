package main

import (
	"context"
	"fmt"
	"time"

	"github.com/Obedience-Corp/camp/internal/workflow"
	"github.com/spf13/cobra"
)

var (
	flowItemsAll  bool
	flowItemsJSON bool
)

var flowItemsCmd = &cobra.Command{
	Use:   "items [status]",
	Short: "List items in a status directory",
	Long: `List items in a status directory.

If no status is specified, lists items in the default status (usually 'active').
Use --all to list items in all status directories.

Examples:
  camp flow items              List items in default status
  camp flow items active       List items in active/
  camp flow items dungeon/completed  List items in dungeon/completed/
  camp flow items --all        List items in all statuses`,
	Args: cobra.MaximumNArgs(1),
	RunE: runFlowItems,
}

func init() {
	flowCmd.AddCommand(flowItemsCmd)
	flowItemsCmd.Flags().BoolVarP(&flowItemsAll, "all", "a", false, "list all statuses")
	flowItemsCmd.Flags().BoolVar(&flowItemsJSON, "json", false, "output as JSON")
}

func runFlowItems(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	cwd, err := getCwd()
	if err != nil {
		return err
	}

	svc := workflow.NewService(cwd)
	if err := svc.LoadSchema(ctx); err != nil {
		return err
	}

	statuses := []string{}
	if flowItemsAll {
		statuses = svc.Schema().AllDirectories()
	} else if len(args) > 0 {
		statuses = []string{args[0]}
	} else {
		// Default to the schema's default status, or "active"
		status := svc.Schema().DefaultStatus
		if status == "" {
			status = "active"
		}
		statuses = []string{status}
	}

	for _, status := range statuses {
		result, err := svc.List(ctx, status, workflow.ListOptions{JSON: flowItemsJSON})
		if err != nil {
			fmt.Printf("Error listing %s: %v\n", status, err)
			continue
		}

		if flowItemsAll {
			fmt.Printf("\n%s/ (%d items)\n", status, len(result.Items))
		}

		for _, item := range result.Items {
			typeChar := " "
			if item.IsDir {
				typeChar = "d"
			}
			fmt.Printf("  %s %-30s %s\n", typeChar, item.Name, item.ModTime.Format(time.RFC3339))
		}
	}

	return nil
}
