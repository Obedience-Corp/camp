package main

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/obediencecorp/camp/internal/config"
	"github.com/obediencecorp/camp/internal/intent"
	"github.com/obediencecorp/camp/internal/intent/tui"
	"github.com/obediencecorp/camp/internal/paths"
)

var intentExploreCmd = &cobra.Command{
	Use:   "explore",
	Short: "Interactive intent explorer",
	Long: `Browse, search, and filter intents in an interactive TUI.

The explorer provides a full-screen interface for managing intents with:
  - Keyboard navigation
  - Fuzzy search filtering
  - Status and type filtering
  - Quick actions (edit, move, archive)

NAVIGATION:
  j/↓         Move down
  k/↑         Move up
  Enter       Select/view intent
  /           Start filtering
  q/Esc       Quit explorer

Examples:
  camp intent explore          Open the intent explorer`,
	Args: cobra.NoArgs,
	RunE: runIntentExplore,
}

func init() {
	intentCmd.AddCommand(intentExploreCmd)
}

func runIntentExplore(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Find campaign root
	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return fmt.Errorf("not in a campaign directory: %w", err)
	}

	// Create path resolver and service
	resolver := paths.NewResolverFromConfig(campaignRoot, cfg)
	svc := intent.NewIntentService(campaignRoot, resolver.Intents())

	// Create and run the TUI
	model := tui.NewExplorerModel(ctx, svc)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("running explorer: %w", err)
	}

	return nil
}
