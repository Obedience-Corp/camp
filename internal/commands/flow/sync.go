package flow

import (
	"context"
	"fmt"

	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/workflow"
	"github.com/spf13/cobra"
)

func newSyncCommand() *cobra.Command {
	var flowSyncDryRun bool

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync directories with schema",
		Long: `Synchronize directories with the workflow schema.

Creates any directories defined in .workflow.yaml that don't exist yet.
Does not remove directories that aren't in the schema.

Use --dry-run to see what would be created without making changes.

Examples:
  camp flow sync              Create missing directories
  camp flow sync --dry-run    Preview changes without creating`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			cwd, err := getCwd()
			if err != nil {
				return err
			}

			svc := workflow.NewService(cwd)
			result, err := svc.Sync(ctx, workflow.SyncOptions{DryRun: flowSyncDryRun})
			if err != nil {
				return err
			}

			if flowSyncDryRun {
				fmt.Println("Dry run - no changes made")
				fmt.Println()
			}

			if len(result.Created) > 0 {
				if flowSyncDryRun {
					fmt.Println("Would create:")
				} else {
					fmt.Println("Created:")
				}
				for _, d := range result.Created {
					fmt.Printf("  %s/\n", d)
				}
			}

			// Check verbose from root command persistent flags
			verbose, _ := cmd.Root().PersistentFlags().GetBool("verbose")
			if len(result.Existing) > 0 && verbose {
				fmt.Println("\nAlready exist:")
				for _, d := range result.Existing {
					fmt.Printf("  %s/\n", d)
				}
			}

			if len(result.Created) == 0 {
				ui.Success("All directories already exist!")
			} else if !flowSyncDryRun {
				ui.Success("Sync complete!")
			}

			return nil
		},
	}

	cmd.Flags().BoolVarP(&flowSyncDryRun, "dry-run", "n", false, "preview changes without creating directories")

	return cmd
}
