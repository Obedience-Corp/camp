package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/obediencecorp/camp/internal/campaign"
	"github.com/obediencecorp/camp/internal/config"
	"github.com/obediencecorp/camp/internal/ui/theme"
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
	RunE:    runSwitch,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		ctx := cmd.Context()
		reg, err := config.LoadRegistry(ctx)
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		toComplete = strings.ToLower(toComplete)
		var names []string
		for _, c := range reg.ListAll() {
			lower := strings.ToLower(c.Name)
			if strings.HasPrefix(lower, toComplete) {
				names = append(names, c.Name)
			}
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	rootCmd.AddCommand(switchCmd)
	switchCmd.GroupID = "navigation"
	switchCmd.Flags().Bool("print", false, "Print path only (for shell integration)")
}

func runSwitch(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	printOnly, _ := cmd.Flags().GetBool("print")

	reg, err := config.LoadRegistry(ctx)
	if err != nil {
		return fmt.Errorf("load registry: %w", err)
	}
	if reg.Len() == 0 {
		return fmt.Errorf("no campaigns registered (use 'camp init' to create one)")
	}

	var selected config.RegisteredCampaign

	if len(args) == 1 {
		c, ok := reg.Get(args[0])
		if !ok {
			return fmt.Errorf("campaign %q not found in registry", args[0])
		}
		selected = c
	} else {
		c, err := pickCampaign(cmd, reg)
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

func pickCampaign(cmd *cobra.Command, reg *config.Registry) (config.RegisteredCampaign, error) {
	ctx := cmd.Context()
	all := reg.ListAll()

	// Sort by last access descending (most recent first)
	sort.Slice(all, func(i, j int) bool {
		return all[i].LastAccess.After(all[j].LastAccess)
	})

	// Detect current campaign for highlighting
	currentPath, _ := campaign.DetectCached(ctx)

	options := make([]huh.Option[string], 0, len(all))
	for _, c := range all {
		label := c.Name
		if c.Path == currentPath {
			label = "* " + label
		}
		options = append(options, huh.NewOption(label, c.ID))
	}

	var selectedID string
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("Switch Campaign").
			Description("Select a campaign to switch to").
			Options(options...).
			Value(&selectedID),
	))

	if err := theme.RunForm(ctx, form); err != nil {
		if theme.IsCancelled(err) {
			return config.RegisteredCampaign{}, fmt.Errorf("cancelled")
		}
		return config.RegisteredCampaign{}, err
	}

	c, ok := reg.GetByID(selectedID)
	if !ok {
		return config.RegisteredCampaign{}, fmt.Errorf("selected campaign not found")
	}
	return c, nil
}
