package intent

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/concept"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/intent/tui/explorer"
	"github.com/Obedience-Corp/camp/internal/paths"
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
  ga            Gather selected intents
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
	Cmd.AddCommand(intentExploreCmd)
}

func runIntentExplore(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Find campaign root
	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	// Create path resolver and services
	resolver := paths.NewResolverFromConfig(campaignRoot, cfg)
	intentsDir := resolver.Intents()
	svc := intent.NewIntentService(campaignRoot, intentsDir)
	conceptSvc := concept.NewService(campaignRoot, cfg.Concepts())

	// Ensure directories exist and migrate legacy layout
	if err := svc.EnsureDirectories(ctx); err != nil {
		return camperrors.Wrap(err, "ensuring intent directories")
	}

	// Get author from git config
	author := git.GetUserName(ctx)

	// Build navigation shortcuts for @ completion
	shortcuts := make(map[string]string)
	for key, sc := range cfg.Shortcuts() {
		if sc.HasPath() {
			shortcuts[key] = sc.Path
		}
	}

	// Route slog output away from stderr for the duration of the TUI session.
	// Without this, INFO-level slog calls from anywhere in the call chain (notably
	// the git auto-commit retry / lock subsystem) write to the same TTY that
	// bubbletea is painting, corrupting the rendered frame and making the TUI
	// appear frozen. Restore the previous default on exit so non-TUI commands in
	// the same process keep their normal behavior.
	restoreLogger, err := quietSlogDuringTUI(campaignRoot)
	if err != nil {
		return camperrors.Wrap(err, "configuring TUI logger")
	}
	defer restoreLogger()

	// Create and run the TUI
	model := explorer.NewModel(ctx, svc, conceptSvc, intentsDir, campaignRoot, cfg.ID, author, shortcuts)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		return camperrors.Wrap(err, "running explorer")
	}

	return nil
}

// quietSlogDuringTUI swaps slog.Default with a handler that writes to a log
// file under <campaignRoot>/.campaign/logs/ instead of stderr. It returns a
// restore function that reinstalls the previous default and closes the log
// file. If the log directory cannot be created or the file cannot be opened,
// it falls back to discarding slog output entirely so the TUI is still safe.
func quietSlogDuringTUI(campaignRoot string) (restore func(), err error) {
	previous := slog.Default()
	restore = func() { slog.SetDefault(previous) }

	logDir := filepath.Join(campaignRoot, ".campaign", "logs")
	if mkErr := os.MkdirAll(logDir, 0o755); mkErr != nil {
		// Fallback: discard slog output entirely. The TUI is still protected.
		slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
		return restore, nil
	}

	f, openErr := os.OpenFile(
		filepath.Join(logDir, "intent-explore.log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0o644,
	)
	if openErr != nil {
		// Fallback: discard slog output entirely.
		slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
		return restore, nil
	}

	previousRestore := restore
	restore = func() {
		previousRestore()
		_ = f.Close()
	}

	// Debug level captures everything (including the auto-commit retry logs
	// that were demoted from Info to Debug in this same change set) so a
	// post-mortem can always recover what happened.
	handler := slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(handler))
	return restore, nil
}
