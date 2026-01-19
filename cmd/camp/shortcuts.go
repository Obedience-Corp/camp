package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/obediencecorp/camp/internal/config"
	"github.com/obediencecorp/camp/internal/ui"
	"github.com/spf13/cobra"
)

var shortcutsCmd = &cobra.Command{
	Use:   "shortcuts",
	Short: "List all available shortcuts",
	Long: `List all navigation and command shortcuts from .campaign/campaign.yaml.

Navigation shortcuts (path-based):
  These shortcuts jump to directories within the campaign.
  Usage: camp go <shortcut>

Command shortcuts (command-based):
  These shortcuts execute commands from specified directories.
  Usage: camp run <shortcut> [args...]

Default shortcuts are added when you run 'camp init'.
You can customize shortcuts by editing .campaign/campaign.yaml.`,
	Example: `  camp shortcuts              # List all shortcuts
  camp go api                 # Use navigation shortcut
  camp run build              # Use command shortcut`,
	Aliases: []string{"sc"},
	RunE:    runShortcuts,
}

func init() {
	rootCmd.AddCommand(shortcutsCmd)
	shortcutsCmd.GroupID = "navigation"
}

func runShortcuts(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Load campaign config to get custom shortcuts
	cfg, campaignRoot, err := loadCampaignConfigSafe(ctx)
	if err != nil {
		// Not in a campaign - just show defaults
		return printDefaultShortcuts()
	}

	return printAllShortcuts(cfg, campaignRoot)
}

// loadCampaignConfigSafe loads campaign config but doesn't fail if we're not in a campaign.
func loadCampaignConfigSafe(ctx context.Context) (*config.CampaignConfig, string, error) {
	cfg, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return nil, "", err
	}
	return cfg, root, nil
}

func printDefaultShortcuts() error {
	fmt.Println(ui.Warning("Not in a campaign"))
	fmt.Println()
	fmt.Printf("Run %s to create a new campaign with default shortcuts.\n",
		ui.Accent("camp init"))

	return nil
}

func printAllShortcuts(cfg *config.CampaignConfig, _ string) error {
	fmt.Println(ui.Subheader("Shortcuts"))
	fmt.Printf("Campaign: %s\n", ui.Accent(cfg.Name))
	fmt.Println()

	if len(cfg.Shortcuts) == 0 {
		fmt.Println(ui.Dim("No shortcuts configured."))
		fmt.Println()
		fmt.Printf("To add shortcuts, edit %s:\n", ui.Accent(".campaign/campaign.yaml"))
		fmt.Println()
		fmt.Println(ui.Dim("  shortcuts:"))
		fmt.Println(ui.Dim("    api:"))
		fmt.Println(ui.Dim("      path: \"projects/api-service\""))
		fmt.Println(ui.Dim("      description: \"Jump to API\""))
		fmt.Println(ui.Dim("    build:"))
		fmt.Println(ui.Dim("      command: \"just build\""))
		fmt.Println(ui.Dim("      description: \"Build all\""))
		return nil
	}

	// Separate navigation and command shortcuts
	navShortcuts := make(map[string]config.ShortcutConfig)
	cmdShortcuts := make(map[string]config.ShortcutConfig)

	for key, sc := range cfg.Shortcuts {
		if sc.IsNavigation() {
			navShortcuts[key] = sc
		}
		if sc.IsCommand() {
			cmdShortcuts[key] = sc
		}
	}

	// Display navigation shortcuts
	if len(navShortcuts) > 0 {
		fmt.Printf("%s\n", ui.Info("Navigation (use with: camp go <shortcut>)"))
		keys := make([]string, 0, len(navShortcuts))
		for k := range navShortcuts {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, key := range keys {
			sc := navShortcuts[key]
			desc := ""
			if sc.Description != "" {
				desc = ui.Dim(" # " + sc.Description)
			}
			fmt.Printf("  %s %s %s%s\n",
				ui.Accent(fmt.Sprintf("%-10s", key)),
				ui.ArrowIcon(),
				ui.Value(sc.Path),
				desc)
		}
		fmt.Println()
	}

	// Display command shortcuts
	if len(cmdShortcuts) > 0 {
		fmt.Printf("%s\n", ui.Info("Commands (use with: camp run <shortcut>)"))
		keys := make([]string, 0, len(cmdShortcuts))
		for k := range cmdShortcuts {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, key := range keys {
			sc := cmdShortcuts[key]
			desc := ""
			if sc.Description != "" {
				desc = ui.Dim(" # " + sc.Description)
			}

			// Show command with workdir if specified
			cmdDisplay := sc.Command
			if sc.WorkDir != "" {
				cmdDisplay = fmt.Sprintf("[%s] %s", sc.WorkDir, sc.Command)
			}

			// Truncate long commands
			if len(cmdDisplay) > 50 {
				cmdDisplay = cmdDisplay[:47] + "..."
			}

			fmt.Printf("  %s %s %s%s\n",
				ui.Accent(fmt.Sprintf("%-10s", key)),
				ui.ArrowIcon(),
				ui.Value(cmdDisplay),
				desc)
		}
	}

	return nil
}

// formatShortcutList formats a list of shortcuts for display.
func formatShortcutList(shortcuts map[string]string) string {
	if len(shortcuts) == 0 {
		return "  (none)"
	}

	keys := make([]string, 0, len(shortcuts))
	for k := range shortcuts {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var lines []string
	for _, k := range keys {
		lines = append(lines, fmt.Sprintf("  %-4s -> %s", k, shortcuts[k]))
	}

	return strings.Join(lines, "\n")
}
