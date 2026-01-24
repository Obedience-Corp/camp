package main

import (
	"context"
	"fmt"
	"os"

	"github.com/obediencecorp/camp/internal/campaign"
	"github.com/obediencecorp/camp/internal/config"
	"github.com/obediencecorp/camp/internal/ui"
	"github.com/obediencecorp/camp/internal/version"
	"github.com/spf13/cobra"
)

var (
	// Global flags
	cfgFile string
	noColor bool
	verbose bool
)

var rootCmd = &cobra.Command{
	Use:   "camp",
	Short: "Campaign management CLI for multi-project AI workspaces",
	Long: `Camp manages multi-project AI workspaces with fast navigation.

Camp provides structure and navigation for AI-powered development workflows.
It creates standardized campaign directories, manages git submodules as projects,
and enables lightning-fast navigation through category shortcuts and TUI fuzzy finding.

GETTING STARTED:
  camp init               Initialize a new campaign in the current directory
  camp project list       List all projects in the campaign
  camp list               Show all registered campaigns

NAVIGATION (using cgo shell function):
  cgo                     Navigate to campaign root
  cgo p                   Navigate to projects directory
  cgo f                   Navigate to festivals directory
  cgo <name>              Fuzzy find and navigate to any target

COMMON WORKFLOWS:
  camp project add <url>  Add a git repo as a project submodule
  camp run <command>      Run command from campaign root directory
  camp shortcuts          View all available navigation shortcuts

Run 'camp shell-init' to enable the cgo navigation function.`,
	Version: fmt.Sprintf("%s (built %s, commit %s)", version.Version, version.BuildDate, version.Commit),
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Wire up the no-color flag
		ui.SetNoColor(noColor)
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		// When invoked without subcommand, show help
		return cmd.Help()
	},
}

// Execute runs the root command
func Execute() error {
	// Expand shortcuts before command execution
	expandShortcuts()
	return rootCmd.Execute()
}

// expandShortcuts expands shortcut aliases in os.Args before cobra parses them.
// For example, "camp p list" becomes "camp project list" if "p" maps to "project".
func expandShortcuts() {
	// Need at least 2 args (program name + subcommand)
	if len(os.Args) < 2 {
		return
	}

	// Find the first non-flag argument after the program name
	argIndex := 1
	for argIndex < len(os.Args) {
		arg := os.Args[argIndex]
		if len(arg) > 0 && arg[0] == '-' {
			// Skip flags
			if arg == "--" {
				// Everything after -- is positional
				argIndex++
				break
			}
			argIndex++
			continue
		}
		break
	}

	if argIndex >= len(os.Args) {
		return
	}

	firstArg := os.Args[argIndex]

	// Skip certain commands that shouldn't be expanded
	if firstArg == "help" || firstArg == "completion" {
		return
	}

	// Try to load shortcuts from campaign config
	shortcutMap := loadShortcutsForExpansion()
	if shortcutMap == nil {
		return
	}

	// Check if first arg is a shortcut with concept
	sc, ok := shortcutMap[firstArg]
	if !ok || sc.Concept == "" {
		return
	}

	// Expand the shortcut
	os.Args[argIndex] = sc.Concept
}

// loadShortcutsForExpansion loads shortcuts from campaign config if available.
// It merges campaign shortcuts with defaults, prioritizing concept mappings from defaults
// when the user's shortcuts don't specify a concept.
func loadShortcutsForExpansion() map[string]config.ShortcutConfig {
	ctx := context.Background()
	defaults := config.DefaultNavigationShortcuts()

	// Try to detect campaign root
	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		// Not in a campaign - use default shortcuts only
		return defaults
	}

	// Load campaign config
	cfg, err := config.LoadCampaignConfig(ctx, campRoot)
	if err != nil {
		// Fall back to defaults
		return defaults
	}

	// Get campaign shortcuts
	campaignShortcuts := cfg.Shortcuts()

	// Merge: campaign shortcuts override defaults, but inherit Concept from defaults
	// if not specified in campaign config
	result := make(map[string]config.ShortcutConfig)

	// Start with defaults
	for k, v := range defaults {
		result[k] = v
	}

	// Merge campaign shortcuts, preserving Concept from defaults when not set
	for k, v := range campaignShortcuts {
		if defaultSc, hasDefault := defaults[k]; hasDefault && v.Concept == "" {
			// Inherit Concept from default if campaign doesn't specify one
			v.Concept = defaultSc.Concept
		}
		result[k] = v
	}

	return result
}

func init() {
	// Define command groups for organized help output
	rootCmd.AddGroup(
		&cobra.Group{ID: "setup", Title: "Setup Commands:"},
		&cobra.Group{ID: "navigation", Title: "Navigation Commands:"},
		&cobra.Group{ID: "registry", Title: "Registry Commands:"},
		&cobra.Group{ID: "project", Title: "Project Commands:"},
		&cobra.Group{ID: "planning", Title: "Planning Commands:"},
		&cobra.Group{ID: "system", Title: "System Commands:"},
	)

	// Global persistent flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ~/.config/campaign/config.yaml)")
	rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "disable colored output")
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "enable verbose output")
}
