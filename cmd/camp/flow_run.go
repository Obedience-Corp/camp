package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/obediencecorp/camp/internal/campaign"
	"github.com/obediencecorp/camp/internal/flow"
)

var flowRunCmd = &cobra.Command{
	Use:   "run <name> [-- extra-args...]",
	Short: "Execute a registered flow by name",
	Long: `Execute a registered flow from .campaign/flows/registry.yaml.

Extra arguments after -- are appended to the flow's command.

Examples:
  camp flow run build
  camp flow run test -- --verbose
  camp flow run deploy -- production`,
	Args:              cobra.MinimumNArgs(1),
	ValidArgsFunction: completeFlowNames,
	RunE:              runFlowRun,
}

func init() {
	flowCmd.AddCommand(flowRunCmd)
}

func runFlowRun(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	flowName := args[0]
	extraArgs := args[1:]

	campaignRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return fmt.Errorf("not in a campaign directory: %w", err)
	}

	registry, err := flow.LoadRegistry(campaignRoot)
	if err != nil {
		return fmt.Errorf("loading flow registry: %w", err)
	}

	flowDef, ok := registry.Get(flowName)
	if !ok {
		return fmt.Errorf("flow %q not found\n\nAvailable flows: %v", flowName, registry.List())
	}

	runner := flow.NewRunner(campaignRoot)
	if err := runner.Run(ctx, flowDef, extraArgs); err != nil {
		return fmt.Errorf("flow %q failed: %w", flowName, err)
	}

	return nil
}

func completeFlowNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	ctx := cmd.Context()
	campaignRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	registry, err := flow.LoadRegistry(campaignRoot)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	return registry.List(), cobra.ShellCompDirectiveNoFileComp
}
