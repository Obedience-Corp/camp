package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/nav"
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
  cd "$(camp switch --print)"

Use campaign@tab to navigate to a specific location in the target campaign:
  camp switch obey-campaign@p    # Switch and navigate to projects/
  camp switch obey-campaign@f    # Switch and navigate to festivals/`,
	Example: `  camp switch                        # Interactive picker
  camp switch obey-campaign          # Switch by name
  camp switch a1b2                   # Switch by ID prefix
  camp switch --print                # Picker, output path only
  camp switch obey-campaign@p        # Switch and navigate to projects/`,
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

		if at := strings.IndexByte(toComplete, '@'); at >= 0 {
			campaignQuery := toComplete[:at]
			tabPrefix := toComplete[at+1:]
			tabs := completeSwitchTabs(ctx, reg, campaignQuery, tabPrefix)
			completions := make([]string, len(tabs))
			for i, t := range tabs {
				completions[i] = campaignQuery + "@" + t
			}
			return completions, cobra.ShellCompDirectiveNoFileComp
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

func resolveTabInCampaign(ctx context.Context, c config.RegisteredCampaign, tabKey string) (string, error) {
	cfg, err := config.LoadCampaignConfig(ctx, c.Path)
	if err != nil {
		return "", camperrors.Wrapf(err, "loading campaign config for %s", c.Name)
	}
	resolved := nav.ResolveConfiguredTarget(cfg, []string{tabKey})
	if !resolved.Matched {
		return "", camperrors.New(fmt.Sprintf("tab %q not found in campaign %s", tabKey, c.Name))
	}
	relativePath := resolved.RelativePath
	if relativePath == "" && resolved.Category != nav.CategoryAll {
		relativePath = resolved.Category.Dir()
	}
	if relativePath == "" {
		return "", camperrors.New(fmt.Sprintf("tab %q resolved to campaign root in %s", tabKey, c.Name))
	}
	return filepath.Join(c.Path, relativePath), nil
}

func completeSwitchTabs(ctx context.Context, reg *config.Registry, campaignQuery, tabPrefix string) []string {
	c, ok := reg.GetByName(campaignQuery)
	if !ok {
		names := reg.List()
		matches := fuzzy.Filter(names, campaignQuery)
		if len(matches) == 0 {
			return nil
		}
		c, ok = reg.GetByName(matches[0].Target)
		if !ok {
			return nil
		}
	}

	cfg, err := config.LoadCampaignConfig(ctx, c.Path)
	if err != nil {
		return nil
	}

	all := nav.TopLevelNavigationNames(cfg)
	if tabPrefix == "" {
		return all
	}

	var filtered []string
	for _, name := range all {
		if strings.HasPrefix(name, tabPrefix) {
			filtered = append(filtered, name)
		}
	}
	return filtered
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
		arg := args[0]
		if at := strings.IndexByte(arg, '@'); at >= 0 {
			campaignQuery := arg[:at]
			tabKey := arg[at+1:]
			c, err := cmdutil.ResolveCampaignSelection(campaignQuery, reg, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			tabPath, err := resolveTabInCampaign(ctx, c, tabKey)
			if err != nil {
				return err
			}
			if printOnly {
				fmt.Println(tabPath)
			} else {
				fmt.Printf("cd %s\n", tabPath)
			}
			return nil
		}
		c, err := cmdutil.ResolveCampaignSelection(arg, reg, cmd.ErrOrStderr())
		if err != nil {
			return err
		}
		selected = c
	} else {
		if !tui.IsTerminal() {
			return camperrors.New("campaign name required in non-interactive mode (use 'camp switch <name>' or run interactively)")
		}
		c, err := cmdutil.PickCampaign(ctx, reg)
		if err != nil {
			return err
		}
		selected = c
	}

	if err := config.UpdateRegistry(ctx, func(reg *config.Registry) error {
		reg.UpdateLastAccess(selected.ID)
		return nil
	}); err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "camp: warning: failed to update last access: %v\n", err)
	}

	if printOnly {
		fmt.Println(selected.Path)
	} else {
		fmt.Printf("cd %s\n", selected.Path)
	}
	return nil
}
