package main

import (
	"encoding/json"
	"errors"
	"fmt"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"io"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/nav/tui"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
	"github.com/Obedience-Corp/camp/internal/workflow"
)

var (
	flowAddForce       bool
	flowAddName        string
	flowAddDescription string
	flowAddJSON        string
)

var flowAddCmd = &cobra.Command{
	Use:     "add",
	Aliases: []string{"init"},
	Short:   "Add workflow tracking to current directory",
	Long: `Add workflow tracking to the current directory.

Creates a .workflow.yaml file, dungeon/ directory structure, and root OBEY.md.
Uses workflow schema v2 (dungeon-centric model) where:
  - Root directory (.) = active work
  - dungeon/           = all other statuses

If dungeon/ already exists, only creates .workflow.yaml.
If both exist, displays a notice.

Use --force to overwrite an existing workflow configuration.

Provide name/description via flags, JSON, or interactive TUI:
  --name/-n and --description/-d   Set via flags
  --json/-j '{"name":"...","description":"..."}'  Set via JSON
  --json -   Read JSON from stdin (for piping)

Note: Flows cannot be nested inside other flows. If you're inside a flow,
navigate to a directory outside of it before running this command.

Examples:
  camp flow add                                      Interactive TUI
  camp flow add --name "API" --description "API dev" Via flags
  camp flow add --json '{"name":"API","description":"API development"}'
  echo '{"name":"X","description":"Y"}' | camp flow add --json -
  camp flow add --force                              Overwrite existing`,
	Annotations: map[string]string{
		"agent_allowed": "true",
		"agent_reason":  "Agents can use --json or --name/--description flags",
	},
	RunE: runFlowAdd,
}

func init() {
	flowCmd.AddCommand(flowAddCmd)
	flowAddCmd.Flags().BoolVarP(&flowAddForce, "force", "f", false, "overwrite existing workflow")
	flowAddCmd.Flags().StringVarP(&flowAddName, "name", "n", "", "workflow name")
	flowAddCmd.Flags().StringVarP(&flowAddDescription, "description", "d", "", "workflow description/purpose")
	flowAddCmd.Flags().StringVarP(&flowAddJSON, "json", "j", "", `JSON input (use "-" for stdin)`)
}

// flowAddInput holds the collected name/description for flow init.
type flowAddInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func runFlowAdd(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	cwd, err := getCwd()
	if err != nil {
		return err
	}

	// Collect name/description: JSON > Flags > TUI
	input, err := collectFlowAddInput(cmd)
	if err != nil {
		return err
	}

	svc := workflow.NewService(cwd)
	result, err := svc.Init(ctx, workflow.InitOptions{
		Force:         flowAddForce,
		SchemaVersion: 2,
		Name:          input.Name,
		Description:   input.Description,
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

// collectFlowAddInput gathers name/description from JSON, flags, or TUI.
// Priority: JSON > Flags > TUI.
func collectFlowAddInput(cmd *cobra.Command) (*flowAddInput, error) {
	ctx := cmd.Context()

	// 1. JSON mode
	if flowAddJSON != "" {
		return parseFlowAddJSON(flowAddJSON)
	}

	// 2. Flags mode — if both provided, use directly
	if flowAddName != "" && flowAddDescription != "" {
		return &flowAddInput{Name: flowAddName, Description: flowAddDescription}, nil
	}

	// 3. Check if we have partial flags in non-interactive mode
	hasPartial := flowAddName != "" || flowAddDescription != ""
	isInteractive := tui.IsTerminal()

	if !isInteractive {
		if hasPartial {
			if flowAddName == "" {
				return nil, fmt.Errorf("--name is required in non-interactive mode\n       Use -n/--name flag, or run in an interactive terminal")
			}
			return nil, fmt.Errorf("--description is required in non-interactive mode\n       Use -d/--description flag, or run in an interactive terminal")
		}
		return nil, fmt.Errorf("--name and --description are required in non-interactive mode\n       Use -n/--name and -d/--description flags, --json, or run in an interactive terminal")
	}

	// 4. TUI mode — pre-fill from any partial flags
	name := flowAddName
	description := flowAddDescription

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Workflow Name").
				Description("A short name for this workflow").
				Placeholder("e.g., API Development").
				Value(&name),
			huh.NewText().
				Title("Description").
				Description("What is this workflow for?").
				Placeholder("Describe the purpose of this workflow...").
				CharLimit(500).
				Value(&description),
		),
	)

	if err := theme.RunForm(ctx, form); err != nil {
		if theme.IsCancelled(err) {
			return nil, fmt.Errorf("initialization cancelled")
		}
		return nil, camperrors.Wrap(err, "failed to collect workflow info")
	}

	if name == "" {
		return nil, fmt.Errorf("workflow name is required")
	}
	if description == "" {
		return nil, fmt.Errorf("workflow description is required")
	}

	return &flowAddInput{Name: name, Description: description}, nil
}

// parseFlowAddJSON parses JSON input from a string or stdin (when value is "-").
func parseFlowAddJSON(value string) (*flowAddInput, error) {
	var data []byte
	var err error

	if value == "-" {
		data, err = io.ReadAll(os.Stdin)
		if err != nil {
			return nil, camperrors.Wrap(err, "failed to read JSON from stdin")
		}
	} else {
		data = []byte(value)
	}

	var input flowAddInput
	if err := json.Unmarshal(data, &input); err != nil {
		return nil, camperrors.Wrap(err, "invalid JSON")
	}

	if input.Name == "" {
		return nil, fmt.Errorf("JSON input requires \"name\" field")
	}
	if input.Description == "" {
		return nil, fmt.Errorf("JSON input requires \"description\" field")
	}

	return &input, nil
}
