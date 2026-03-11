package flow

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/workflow"
)

func newStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show workflow statistics",
		Long: `Show workflow statistics including item counts per status.

Displays the workflow name, location, and counts for each status directory.

Examples:
  camp flow status            Show workflow statistics`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			cwd, err := getCwd()
			if err != nil {
				return err
			}

			svc := workflow.NewService(cwd)
			if err := svc.LoadSchema(ctx); err != nil {
				return err
			}

			schema := svc.Schema()
			fmt.Printf("Workflow: %s (v%d)\n", schema.Name, schema.Version)
			fmt.Printf("Location: %s\n", svc.Root())
			fmt.Println()

			for _, status := range schema.AllDirectories() {
				result, err := svc.List(ctx, status, workflow.ListOptions{})
				if err != nil {
					continue
				}

				label := status + "/"
				if status == "." {
					label = "active (root)"
				}

				fmt.Printf("  %-20s %d items\n", label, len(result.Items))
			}

			return nil
		},
	}
}
