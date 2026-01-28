package main

import (
	"context"
	"fmt"

	"github.com/obediencecorp/camp/internal/ui"
	"github.com/obediencecorp/camp/internal/workflow"
	"github.com/spf13/cobra"
)

var (
	flowMigrateDryRun bool
	flowMigrateForce  bool
)

var flowMigrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migrate legacy dungeon to workflow",
	Long: `Migrate a legacy dungeon structure to the full workflow system.

Detects existing dungeon directories and creates a .workflow.yaml
configuration file while preserving all existing data.

Use --dry-run to preview the migration without making changes.
Use --force to skip confirmation prompts.

Examples:
  camp flow migrate            Migrate with confirmation
  camp flow migrate --dry-run  Preview migration
  camp flow migrate --force    Migrate without confirmation`,
	RunE: runFlowMigrate,
}

func init() {
	flowCmd.AddCommand(flowMigrateCmd)
	flowMigrateCmd.Flags().BoolVarP(&flowMigrateDryRun, "dry-run", "n", false, "preview migration without making changes")
	flowMigrateCmd.Flags().BoolVarP(&flowMigrateForce, "force", "f", false, "skip confirmation")
}

func runFlowMigrate(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	cwd, err := getCwd()
	if err != nil {
		return err
	}

	svc := workflow.NewService(cwd)

	// Check if already has workflow
	if svc.HasSchema() {
		fmt.Println("Workflow already initialized. No migration needed.")
		return nil
	}

	result, err := svc.Migrate(ctx, workflow.MigrateOptions{
		DryRun: flowMigrateDryRun,
		Force:  flowMigrateForce,
	})
	if err != nil {
		return err
	}

	if flowMigrateDryRun {
		fmt.Println("Dry run - no changes made")
		fmt.Println()
		fmt.Println("Would create:")
		for _, f := range result.Created {
			fmt.Printf("  %s\n", f)
		}
		fmt.Println("\nWould preserve:")
		for _, f := range result.Preserved {
			fmt.Printf("  %s\n", f)
		}
		return nil
	}

	ui.Success("Migration complete!")
	if len(result.Created) > 0 {
		fmt.Println("\nCreated:")
		for _, f := range result.Created {
			fmt.Printf("  %s\n", f)
		}
	}
	if len(result.Preserved) > 0 {
		fmt.Println("\nPreserved:")
		for _, f := range result.Preserved {
			fmt.Printf("  %s\n", f)
		}
	}

	return nil
}
