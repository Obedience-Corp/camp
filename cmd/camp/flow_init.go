package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/obediencecorp/camp/internal/ui"
	"github.com/obediencecorp/camp/internal/workflow"
	"github.com/spf13/cobra"
)

var flowInitForce bool

var flowInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new workflow",
	Long: `Initialize a new workflow with the default structure.

Creates a .workflow.yaml configuration file and the following directories:
  active/      Work in progress
  ready/       Prepared for action
  dungeon/
    completed/ Successfully finished
    archived/  Preserved but inactive
    someday/   Maybe later

Use --force to overwrite an existing workflow configuration.

Note: Flows cannot be nested inside other flows. If you're inside a flow,
navigate to a directory outside of it before running this command.

Examples:
  camp flow init              Initialize workflow in current directory
  camp flow init --force      Overwrite existing workflow`,
	Annotations: map[string]string{
		"agent_allowed": "false",
		"agent_reason":  "Creates workflow structure, requires human decision",
	},
	RunE: runFlowInit,
}

func init() {
	flowCmd.AddCommand(flowInitCmd)
	flowInitCmd.Flags().BoolVarP(&flowInitForce, "force", "f", false, "overwrite existing workflow")
}

func runFlowInit(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Get current directory
	cwd, err := getCwd()
	if err != nil {
		return err
	}

	svc := workflow.NewService(cwd)
	result, err := svc.Init(ctx, workflow.InitOptions{Force: flowInitForce})
	if err != nil {
		// Handle flow nesting error with clear formatting
		var nestedErr *workflow.FlowNestedError
		if errors.As(err, &nestedErr) {
			ui.Error("Cannot create flow inside existing flow")
			fmt.Println()
			fmt.Printf("Found parent flow at: %s\n", nestedErr.ParentSchemaPath)
			fmt.Println()
			fmt.Println("Flows cannot be nested because:")
			fmt.Println("  - Path resolution becomes ambiguous")
			fmt.Println("  - Active work tracking is complicated")
			fmt.Println("  - Status directories would conflict")
			fmt.Println()
			fmt.Println("To create a new flow, navigate outside the current flow first.")
			return nil // Return nil to avoid double-printing error
		}
		return err
	}

	// Print results
	ui.Success("Workflow initialized!")
	fmt.Println()

	if len(result.CreatedFiles) > 0 {
		fmt.Println("Created files:")
		for _, f := range result.CreatedFiles {
			fmt.Printf("  %s\n", f)
		}
	}

	if len(result.CreatedDirs) > 0 {
		fmt.Println("\nCreated directories:")
		for _, d := range result.CreatedDirs {
			fmt.Printf("  %s/\n", d)
		}
	}

	fmt.Println("\nNext steps:")
	fmt.Println("  - Edit .workflow.yaml to customize your workflow")
	fmt.Println("  - Run 'camp flow sync' to create any new directories")
	fmt.Println("  - Run 'camp flow status' to see your workflow")

	return nil
}
