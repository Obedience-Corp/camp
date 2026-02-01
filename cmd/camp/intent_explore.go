package main

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/obediencecorp/camp/internal/concept"
	"github.com/obediencecorp/camp/internal/config"
	"github.com/obediencecorp/camp/internal/intent"
	"github.com/obediencecorp/camp/internal/intent/tui/explorer"
	"github.com/obediencecorp/camp/internal/paths"
)

var intentExploreCmd = &cobra.Command{
	Use:   "explore",
	Short: "Interactive intent explorer",
	Long: `Launch the interactive Intent Explorer TUI.

The explorer provides a full-screen interface for browsing,
filtering, and managing intents with keyboard shortcuts.

NAVIGATION
  j/↓           Move down
  k/↑           Move up
  g             Go to top (preview)
  G             Go to bottom (preview)
  Enter/Space   Select/expand group
  Tab           Switch focus (list/preview)

ACTIONS
  e             Edit in $EDITOR
  o             Open with system handler
  O             Reveal in file manager
  n             New intent
  p             Promote to next status
  a             Archive intent
  d             Delete intent
  m             Move intent to status

GATHER (Multi-Select)
  Space         Toggle selection / enter gather mode
  Ctrl+g        Gather selected intents
  Escape        Exit multi-select mode

FILTERS
  /             Search intents (fuzzy)
  t             Filter by type
  s             Filter by status
  c             Filter by concept
  C             Clear concept filter
  Escape        Clear filter/cancel

VIEW
  v             Toggle preview pane
  ?             Show help overlay
  q             Quit explorer

Examples:
  camp intent explore          Launch the intent explorer`,
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

	// Create path resolver and services
	resolver := paths.NewResolverFromConfig(campaignRoot, cfg)
	intentsDir := resolver.Intents()
	svc := intent.NewIntentService(campaignRoot, intentsDir)
	conceptSvc := concept.NewService(campaignRoot, cfg.Concepts())

	// Create and run the TUI
	model := explorer.NewModel(ctx, svc, conceptSvc, intentsDir, campaignRoot, cfg.ID)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("running explorer: %w", err)
	}

	return nil
}
