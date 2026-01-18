package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/obediencecorp/camp/internal/config"
	"github.com/obediencecorp/camp/internal/nav"
	"github.com/spf13/cobra"
)

var shortcutsCmd = &cobra.Command{
	Use:   "shortcuts",
	Short: "List all available shortcuts",
	Long: `List all available navigation and command shortcuts.

Shows both built-in navigation shortcuts (p, f, c, etc.) and custom shortcuts
defined in .campaign/campaign.yaml under the 'shortcuts' key.

Navigation shortcuts (path-based):
  These shortcuts jump to directories within the campaign.
  Usage: camp go <shortcut>

Command shortcuts (command-based):
  These shortcuts execute commands from specified directories.
  Usage: camp run <shortcut> [args...]`,
	Example: `  camp shortcuts              # List all shortcuts
  camp go api                 # Use navigation shortcut
  camp run build              # Use command shortcut`,
	Aliases: []string{"sc"},
	RunE:    runShortcuts,
}

func init() {
	rootCmd.AddCommand(shortcutsCmd)
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
	fmt.Println("Built-in Navigation Shortcuts:")
	fmt.Println()

	// Sort shortcuts for consistent display
	shortcuts := make([][2]string, 0, len(nav.DefaultShortcuts))
	for key, cat := range nav.DefaultShortcuts {
		shortcuts = append(shortcuts, [2]string{key, cat.Dir()})
	}
	sort.Slice(shortcuts, func(i, j int) bool {
		return shortcuts[i][0] < shortcuts[j][0]
	})

	for _, s := range shortcuts {
		fmt.Printf("  %-4s -> %s/\n", s[0], s[1])
	}

	fmt.Println()
	fmt.Println("No custom shortcuts defined (not in a campaign or no shortcuts configured)")
	fmt.Println()
	fmt.Println("To add custom shortcuts, edit .campaign/campaign.yaml and add a 'shortcuts' section.")

	return nil
}

func printAllShortcuts(cfg *config.CampaignConfig, campaignRoot string) error {
	fmt.Println("Built-in Navigation Shortcuts:")
	fmt.Println()

	// Sort and display default shortcuts
	shortcuts := make([][2]string, 0, len(nav.DefaultShortcuts))
	for key, cat := range nav.DefaultShortcuts {
		shortcuts = append(shortcuts, [2]string{key, cat.Dir()})
	}
	sort.Slice(shortcuts, func(i, j int) bool {
		return shortcuts[i][0] < shortcuts[j][0]
	})

	for _, s := range shortcuts {
		fmt.Printf("  %-4s -> %s/\n", s[0], s[1])
	}

	// Display custom shortcuts
	if len(cfg.Shortcuts) > 0 {
		fmt.Println()
		fmt.Println("Custom Shortcuts:")
		fmt.Println()

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
			fmt.Println("  Navigation (use with: camp go <shortcut>):")
			keys := make([]string, 0, len(navShortcuts))
			for k := range navShortcuts {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			for _, key := range keys {
				sc := navShortcuts[key]
				desc := ""
				if sc.Description != "" {
					desc = fmt.Sprintf(" # %s", sc.Description)
				}
				fmt.Printf("    %-10s -> %s%s\n", key, sc.Path, desc)
			}
			fmt.Println()
		}

		// Display command shortcuts
		if len(cmdShortcuts) > 0 {
			fmt.Println("  Commands (use with: camp run <shortcut>):")
			keys := make([]string, 0, len(cmdShortcuts))
			for k := range cmdShortcuts {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			for _, key := range keys {
				sc := cmdShortcuts[key]
				desc := ""
				if sc.Description != "" {
					desc = fmt.Sprintf(" # %s", sc.Description)
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

				fmt.Printf("    %-10s -> %s%s\n", key, cmdDisplay, desc)
			}
		}
	} else {
		fmt.Println()
		fmt.Println("No custom shortcuts defined.")
		fmt.Println()
		fmt.Println("To add shortcuts, edit .campaign/campaign.yaml:")
		fmt.Println()
		fmt.Println("  shortcuts:")
		fmt.Println("    api:")
		fmt.Println("      path: \"projects/api-service\"")
		fmt.Println("      description: \"Jump to API\"")
		fmt.Println("    build:")
		fmt.Println("      command: \"just build\"")
		fmt.Println("      description: \"Build all\"")
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
