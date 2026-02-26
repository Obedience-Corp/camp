package main

import (
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/flow"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
)

var flowCmd = &cobra.Command{
	Use:     "flow",
	Aliases: []string{"workflow", "wf"},
	Short:   "Manage status workflows for organizing work",
	Long: `Manage status workflows for organizing work.

A workflow defines status directories that items can move between,
with optional transition rules and history tracking. The workflow is
configured via a .workflow.yaml file.

GETTING STARTED:
  camp flow init              Initialize a new workflow
  camp flow sync              Create missing directories from schema
  camp flow status            Show workflow statistics

MANAGING ITEMS:
  camp flow list              List registered flows
  camp flow items             List items in a status directory
  camp flow move <item> <to>  Move an item to a new status

RUNNING FLOWS:
  camp flow run <name>        Execute a registered flow
  camp flow                   Interactive flow picker

OTHER COMMANDS:
  camp flow show              Display workflow structure
  camp flow history           View transition history
  camp flow migrate           Upgrade legacy dungeon structure

DEFAULT STRUCTURE:
  active/                Work in progress
  ready/                 Prepared for action
  dungeon/
    completed/           Successfully finished
    archived/            Preserved but inactive
    someday/             Maybe later

Customize by editing .workflow.yaml and running 'camp flow sync'.`,
	Annotations: map[string]string{
		"agent_allowed": "false",
		"agent_reason":  "Interactive TUI picker",
		"interactive":   "true",
	},
	RunE: runFlowPicker,
}

func init() {
	rootCmd.AddCommand(flowCmd)
	flowCmd.GroupID = "planning"
}

func runFlowPicker(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	campaignRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return fmt.Errorf("not in a campaign directory: %w", err)
	}

	registry, err := flow.LoadRegistry(campaignRoot)
	if err != nil {
		return fmt.Errorf("loading flow registry: %w", err)
	}

	if len(registry.Flows) == 0 {
		fmt.Println("No flows registered.")
		fmt.Printf("\nRegistry location: %s\n", flow.RegistryPath(campaignRoot))
		return nil
	}

	names := registry.List()

	var options []huh.Option[string]
	for _, name := range names {
		flowDef := registry.Flows[name]
		desc := flowDef.Description
		if desc == "" {
			desc = flowDef.Command
		}
		options = append(options, huh.NewOption(
			fmt.Sprintf("%s - %s", name, desc),
			name,
		))
	}

	var selected string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select a flow to run").
				Options(options...).
				Value(&selected),
		),
	)

	if err := theme.RunForm(ctx, form); err != nil {
		return nil
	}

	if selected == "" {
		return nil
	}

	flowDef, _ := registry.Get(selected)
	runner := flow.NewRunner(campaignRoot)

	fmt.Printf("\n=== Running flow: %s ===\n\n", selected)

	if err := runner.Run(ctx, flowDef, nil); err != nil {
		return fmt.Errorf("running flow %q: %w", selected, err)
	}

	return nil
}
