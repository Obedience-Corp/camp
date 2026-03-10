//go:build dev

package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/workflow"
)

var (
	flowMigrateDryRun bool
	flowMigrateForce  bool
)

var flowMigrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migrate workflow to latest schema version",
	Long: `Migrate a workflow to the latest schema version.

Supports two migration paths:
  - Legacy dungeon → v1 workflow (creates .workflow.yaml)
  - v1 → v2 (dungeon-centric model)

For v1→v2 migration:
  - active/ items move to root directory
  - ready/ items move to dungeon/ready/
  - Empty active/ and ready/ directories are removed
  - Schema is updated to version 2

Use --dry-run to preview changes without applying them.
Use --force to skip confirmation prompts.

Examples:
  camp flow migrate            Migrate with confirmation
  camp flow migrate --dry-run  Preview migration
  camp flow migrate --force    Migrate without confirmation`,
	Annotations: map[string]string{
		"agent_allowed": "false",
		"agent_reason":  "Migrates workflow structure, destructive operation",
	},
	RunE: runFlowMigrate,
}

func init() {
	flowCmd.AddCommand(flowMigrateCmd)
	flowMigrateCmd.Flags().BoolVarP(&flowMigrateDryRun, "dry-run", "n", false, "preview migration without making changes")
	flowMigrateCmd.Flags().BoolVarP(&flowMigrateForce, "force", "f", false, "skip confirmation")
}

func runFlowMigrate(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	cwd, err := getCwd()
	if err != nil {
		return err
	}

	svc := workflow.NewService(cwd)

	// Check if workflow schema exists
	if !svc.HasSchema() {
		// Legacy dungeon → v1 migration
		return runLegacyMigrate(ctx, svc)
	}

	// Load schema to check version
	if err := svc.LoadSchema(ctx); err != nil {
		return err
	}

	schema := svc.Schema()
	if schema.Version == 2 {
		fmt.Println("Workflow is already v2. No migration needed.")
		return nil
	}

	// v1 → v2 migration
	return runV1ToV2Migrate(ctx, svc)
}

func runLegacyMigrate(ctx context.Context, svc *workflow.Service) error {
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
		if len(result.Created) > 0 {
			fmt.Println("Would create:")
			for _, f := range result.Created {
				fmt.Printf("  %s\n", f)
			}
		}
		if len(result.Preserved) > 0 {
			fmt.Println("\nWould preserve:")
			for _, f := range result.Preserved {
				fmt.Printf("  %s\n", f)
			}
		}
		return nil
	}

	ui.Success("Legacy migration complete!")
	if len(result.Created) > 0 {
		fmt.Println("\nCreated:")
		for _, f := range result.Created {
			fmt.Printf("  %s\n", f)
		}
	}
	return nil
}

func runV1ToV2Migrate(ctx context.Context, svc *workflow.Service) error {
	result, err := svc.MigrateV1ToV2(ctx, flowMigrateDryRun)
	if err != nil {
		return err
	}

	if flowMigrateDryRun {
		fmt.Println("Dry run - v1 → v2 migration preview")
		fmt.Println()
	}

	if len(result.MovedItems) > 0 {
		label := "Would move:"
		if !flowMigrateDryRun {
			label = "Moved:"
		}
		fmt.Println(label)
		for _, item := range result.MovedItems {
			fmt.Printf("  %s\n", item)
		}
	}

	if len(result.RemovedDirs) > 0 {
		label := "\nWould remove:"
		if !flowMigrateDryRun {
			label = "\nRemoved:"
		}
		fmt.Println(label)
		for _, dir := range result.RemovedDirs {
			fmt.Printf("  %s\n", dir)
		}
	}

	if result.SchemaUpdate {
		if flowMigrateDryRun {
			fmt.Println("\nWould update schema to v2")
		} else {
			fmt.Println("\nSchema updated to v2")
		}
	}

	if flowMigrateDryRun {
		fmt.Println("\nDry run complete. No changes made.")
	} else {
		fmt.Println()
		ui.Success("Migration to v2 complete!")
	}

	return nil
}
