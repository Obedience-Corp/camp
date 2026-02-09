package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/obediencecorp/camp/internal/ui"
	"github.com/obediencecorp/camp/internal/workflow"
)

var flowAddForce bool

var flowAddCmd = &cobra.Command{
	Use:     "add",
	Aliases: []string{"init"},
	Short:   "Add workflow tracking to current directory",
	Long: `Add workflow tracking to the current directory.

Creates a .workflow.yaml file and dungeon/ directory structure.
Uses workflow schema v2 (dungeon-centric model) where:
  - Root directory (.) = active work
  - dungeon/           = all other statuses

If dungeon/ already exists, only creates .workflow.yaml.
If both exist, displays a notice.

Use --force to overwrite an existing workflow configuration.

Note: Flows cannot be nested inside other flows. If you're inside a flow,
navigate to a directory outside of it before running this command.

Examples:
  camp flow add              Add workflow to current directory
  camp flow add --force      Overwrite existing workflow
  camp flow init             Alias for 'add'`,
	Annotations: map[string]string{
		"agent_allowed": "false",
		"agent_reason":  "Creates workflow structure, requires human decision",
	},
	RunE: runFlowAdd,
}

func init() {
	flowCmd.AddCommand(flowAddCmd)
	flowAddCmd.Flags().BoolVarP(&flowAddForce, "force", "f", false, "overwrite existing workflow")
}

func runFlowAdd(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	cwd, err := getCwd()
	if err != nil {
		return err
	}

	svc := workflow.NewService(cwd)
	result, err := svc.Init(ctx, workflow.InitOptions{
		Force:         flowAddForce,
		SchemaVersion: 2,
	})
	if err != nil {
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
			return nil
		}
		return err
	}

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
