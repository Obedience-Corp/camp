package main

import (
	"fmt"

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
  camp status             Show campaign status

NAVIGATION (using cgo shell function):
  cgo                     Navigate to campaign root
  cgo p                   Navigate to projects directory
  cgo f                   Navigate to festivals directory
  cgo <name>              Fuzzy find and navigate to any target

Run 'camp --help' for detailed command information.`,
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
	return rootCmd.Execute()
}

func init() {
	// Global persistent flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ~/.config/campaign/config.yaml)")
	rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "disable colored output")
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "enable verbose output")

	// Subcommands will be added here as they are implemented
}
