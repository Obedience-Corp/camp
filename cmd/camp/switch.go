package main

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/ktr0731/go-fuzzyfinder"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/campaign"
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
		return fmt.Errorf("load registry: %w", err)
	}
	if reg.Len() == 0 {
		return fmt.Errorf("no campaigns registered (use 'camp init' to create one)")
	}

	var selected config.RegisteredCampaign

	if len(args) == 1 {
		c, ok := reg.Get(args[0])
		if !ok {
			// Fuzzy matching fallback (4th strategy after exact ID, ID prefix, name)
			names := reg.List()
			matches := fuzzy.Filter(names, args[0])
			if len(matches) == 0 {
				return fmt.Errorf("campaign %q not found in registry", args[0])
			}

			// Use best match (first result has highest score)
			bestName := matches[0].Target
			c, ok = reg.GetByName(bestName)
			if !ok {
				return fmt.Errorf("campaign %q not found in registry", args[0])
			}

			// Inform user of fuzzy match on stderr
			fmt.Fprintf(os.Stderr, "Matched: %s -> %s\n", args[0], c.Name)
		}
		selected = c
	} else {
		if !tui.IsTerminal() {
			return fmt.Errorf("campaign name required in non-interactive mode\n       Usage: camp switch <name> --print")
		}
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

	// Cache loaded configs to avoid re-reading on every preview render
	cfgCache := map[string]*config.CampaignConfig{}
	loadConfig := func(path string) *config.CampaignConfig {
		if cfg, ok := cfgCache[path]; ok {
			return cfg
		}
		cfg, err := config.LoadCampaignConfig(ctx, path)
		if err != nil {
			cfgCache[path] = nil
			return nil
		}
		cfgCache[path] = cfg
		return cfg
	}

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
			cfg := loadConfig(c.Path)
			return formatSwitchPreview(c, cfg, currentPath, w)
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

// formatSwitchPreview renders the preview pane for a campaign in the switch picker.
// If cfg is non-nil, rich details (mission, description, projects) are shown.
// Falls back to registry-only data when cfg is nil.
func formatSwitchPreview(c config.RegisteredCampaign, cfg *config.CampaignConfig, currentPath string, w int) string {
	var b strings.Builder
	pad := "  "

	b.WriteString(fmt.Sprintf("%s%s", pad, c.Name))
	if c.Type != "" {
		b.WriteString(fmt.Sprintf("  (%s)", c.Type))
	}
	b.WriteByte('\n')

	if cfg != nil && cfg.Mission != "" {
		b.WriteString(fmt.Sprintf("%s%s\n", pad, cfg.Mission))
	}

	if cfg != nil && cfg.Description != "" {
		b.WriteString(fmt.Sprintf("%s%s\n", pad, cfg.Description))
	}

	if cfg != nil && len(cfg.Projects) > 0 {
		b.WriteByte('\n')
		b.WriteString(fmt.Sprintf("%sProjects: (%d)\n", pad, len(cfg.Projects)))
		// Wrap project names to fit preview width
		lineWidth := w - 6 // account for padding + indent
		if lineWidth < 20 {
			lineWidth = 20
		}
		line := pad + "  "
		for i, p := range cfg.Projects {
			name := p.Name
			if i < len(cfg.Projects)-1 {
				name += ", "
			}
			if len(line)+len(name) > lineWidth && line != pad+"  " {
				b.WriteString(line + "\n")
				line = pad + "  "
			}
			line += name
		}
		if line != pad+"  " {
			b.WriteString(line + "\n")
		}
	}

	b.WriteByte('\n')
	b.WriteString(fmt.Sprintf("%sPath: %s\n", pad, c.Path))

	if !c.LastAccess.IsZero() {
		b.WriteString(fmt.Sprintf("%sLast: %s\n", pad, c.LastAccess.Format("Jan 2 15:04")))
	}

	if c.Path == currentPath {
		b.WriteString(fmt.Sprintf("\n%s(current)\n", pad))
	}

	return b.String()
}
