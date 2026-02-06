package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/ktr0731/go-fuzzyfinder"
	"github.com/spf13/cobra"

	"github.com/obediencecorp/camp/internal/campaign"
	"github.com/obediencecorp/camp/internal/config"
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

	idx, err := fuzzyfinder.Find(
		all,
		func(i int) string {
			c := all[i]
			if c.Path == currentPath {
				return "* " + c.Name
			}
			return "  " + c.Name
		},
		fuzzyfinder.WithPreviewWindow(func(i, w, h int) string {
			if i < 0 || i >= len(all) {
				return ""
			}
			c := all[i]
			preview := fmt.Sprintf("  Name: %s\n  Path: %s", c.Name, c.Path)
			if !c.LastAccess.IsZero() {
				preview += fmt.Sprintf("\n  Last: %s", c.LastAccess.Format("Jan 2 15:04"))
			}
			if c.Path == currentPath {
				preview += "\n\n  (current)"
			}
			return preview
		}),
		fuzzyfinder.WithPromptString("Switch to: "),
		fuzzyfinder.WithHeader("  ↑/↓ navigate • type to filter • esc cancel"),
		fuzzyfinder.WithContext(ctx),
	)
	if err != nil {
		if errors.Is(err, fuzzyfinder.ErrAbort) {
			return config.RegisteredCampaign{}, fmt.Errorf("cancelled")
		}
		return config.RegisteredCampaign{}, fmt.Errorf("picker: %w", err)
	}

	return all[idx], nil
}
