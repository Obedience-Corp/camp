package flow

import (
	"context"
	"fmt"
	"os"

	"github.com/Obedience-Corp/camp/internal/workflow"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newShowCommand() *cobra.Command {
	var (
		flowShowTree   bool
		flowShowSchema bool
	)

	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show workflow structure",
		Long: `Display the workflow structure and configuration.

Shows the directories defined in the workflow, their descriptions,
and transition rules.

Use --schema to display the raw .workflow.yaml file.

Examples:
  camp flow show             Show workflow structure
  camp flow show --schema    Show raw schema file`,
		RunE: func(cmd *cobra.Command, args []string) error {
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
			fmt.Printf("Workflow: %s (v%d)\n", schema.Name, schema.Version)
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
					label := name + "/"
					if name == "." {
						label = ". (root = active work)"
					}
					fmt.Printf("  %s\n", label)
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
		},
	}

	cmd.Flags().BoolVarP(&flowShowTree, "tree", "t", false, "display as tree")
	cmd.Flags().BoolVarP(&flowShowSchema, "schema", "s", false, "show raw schema file")

	return cmd
}

// schemaToYAML converts the schema to YAML for display.
func schemaToYAML(schema *workflow.Schema) (string, error) {
	data, err := yaml.Marshal(schema)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
