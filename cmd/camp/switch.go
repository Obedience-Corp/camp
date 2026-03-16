package main

import (
	"fmt"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/nav/fuzzy"
	"github.com/Obedience-Corp/camp/internal/nav/tui"
)

var switchCmd = &cobra.Command{
	Use:   "switch [campaign]",
	Short: "Switch to a different campaign",
	Long: `Switch to a registered campaign by name or ID.

Without arguments, opens an interactive picker to select a campaign.
With an argument, looks up the campaign by name or ID prefix.

Use with the cgo shell function for instant navigation:
  cgo switch                 # Interactive campaign picker
  cgo switch my-campaign     # Switch by name
  cgo switch a1b2             # Switch by ID prefix

The --print flag outputs just the path for shell integration:
  cd "$(camp switch --print)"`,
	Example: `  camp switch                    # Interactive picker
  camp switch obey-campaign      # Switch by name
  camp switch a1b2               # Switch by ID prefix
  camp switch --print            # Picker, output path only`,
	Aliases: []string{"sw"},
	Args:    cobra.MaximumNArgs(1),
	Annotations: map[string]string{
		"agent_allowed": "true",
		"agent_reason":  "Agents use: camp switch <name> --print",
		"interactive":   "true",
	},
	RunE: runSwitch,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		ctx := cmd.Context()
		reg, err := config.LoadRegistry(ctx)
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		names := reg.List()
		if toComplete == "" {
			return names, cobra.ShellCompDirectiveNoFileComp
		}
		matches := fuzzy.Filter(names, toComplete)
		return matches.Targets(), cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	rootCmd.AddCommand(switchCmd)
	switchCmd.GroupID = "global"
	switchCmd.Flags().Bool("print", false, "Print path only (for shell integration)")
}

func runSwitch(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	printOnly, _ := cmd.Flags().GetBool("print")

	reg, err := config.LoadRegistry(ctx)
	if err != nil {
		return camperrors.Wrap(err, "load registry")
	}
	if reg.Len() == 0 {
		return fmt.Errorf("no campaigns registered (use 'camp init' to create one)")
	}

	var selected config.RegisteredCampaign

	if len(args) == 1 {
		c, err := cmdutil.ResolveCampaignSelection(args[0], reg, cmd.ErrOrStderr())
		if err != nil {
			return err
		}
		selected = c
	} else {
		if !tui.IsTerminal() {
			return fmt.Errorf("campaign name required in non-interactive mode\n       Usage: camp switch <name> --print")
		}
		c, err := cmdutil.PickCampaign(ctx, reg)
		if err != nil {
			return err
		}
		selected = c
	}

	// Update last access
	reg.UpdateLastAccess(selected.ID)
	_ = config.SaveRegistry(ctx, reg)

	if printOnly {
		fmt.Println(selected.Path)
	} else {
		fmt.Printf("cd %s\n", selected.Path)
	}
	return nil
}
