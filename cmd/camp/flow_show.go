package main

import (
	"context"
	"fmt"
	"os"

	"github.com/obediencecorp/camp/internal/workflow"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	flowShowTree   bool
	flowShowSchema bool
)

var flowShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show workflow structure",
	Long: `Display the workflow structure and configuration.

Shows the directories defined in the workflow, their descriptions,
and transition rules.

Use --schema to display the raw .workflow.yaml file.

Examples:
  camp flow show             Show workflow structure
  camp flow show --schema    Show raw schema file`,
	RunE: runFlowShow,
}

func init() {
	flowCmd.AddCommand(flowShowCmd)
	flowShowCmd.Flags().BoolVarP(&flowShowTree, "tree", "t", false, "display as tree")
	flowShowCmd.Flags().BoolVarP(&flowShowSchema, "schema", "s", false, "show raw schema file")
}

func runFlowShow(cmd *cobra.Command, args []string) error {
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

	if flowShowSchema {
		// Show raw schema
		data, err := os.ReadFile(cwd + "/.workflow.yaml")
		if err != nil {
			return err
		}
		fmt.Print(string(data))
		return nil
	}

	// Show formatted structure
	fmt.Printf("Workflow: %s\n", schema.Name)
	if schema.Description != "" {
		fmt.Printf("Description: %s\n", schema.Description)
	}
	fmt.Println()

	fmt.Println("Directories:")
	for name, dir := range schema.Directories {
		if dir.Nested {
			fmt.Printf("  %s/ (nested)\n", name)
			fmt.Printf("    %s\n", dir.Description)
			for childName, child := range dir.Children {
				fmt.Printf("    %s/%s/\n", name, childName)
				fmt.Printf("      %s\n", child.Description)
			}
		} else {
			fmt.Printf("  %s/\n", name)
			fmt.Printf("    %s\n", dir.Description)
			if len(dir.TransitionOpts) > 0 {
				fmt.Printf("    Transitions: %v\n", dir.TransitionOpts)
			}
		}
	}

	if schema.TrackHistory {
		fmt.Printf("\nHistory: enabled (%s)\n", schema.HistoryFile)
	}

	return nil
}

// schemaToYAML converts the schema to YAML for display.
func schemaToYAML(schema *workflow.Schema) (string, error) {
	data, err := yaml.Marshal(schema)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
