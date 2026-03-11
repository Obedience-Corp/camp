package flow

import (
	"fmt"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/flow"
)

func newRunCommand() *cobra.Command {
	return &cobra.Command{
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
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			flowName := args[0]
			extraArgs := args[1:]

			campaignRoot, err := campaign.DetectCached(ctx)
			if err != nil {
				return camperrors.Wrap(err, "not in a campaign directory")
			}

			registry, err := flow.LoadRegistry(campaignRoot)
			if err != nil {
				return camperrors.Wrap(err, "loading flow registry")
			}

			flowDef, ok := registry.Get(flowName)
			if !ok {
				return fmt.Errorf("flow %q not found\n\nAvailable flows: %v", flowName, registry.List())
			}

			runner := flow.NewRunner(campaignRoot)
			if err := runner.Run(ctx, flowDef, extraArgs); err != nil {
				return camperrors.Wrapf(err, "flow %q failed", flowName)
			}

			return nil
		},
	}
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
