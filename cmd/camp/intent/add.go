package intent

import (
	"context"
	"errors"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/concept"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/editor"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/intent/audit"
	"github.com/Obedience-Corp/camp/internal/intent/tui"
	"github.com/Obedience-Corp/camp/internal/paths"
)

var intentAddCmd = &cobra.Command{
	Use:   "add [title]",
	Short: "Create a new intent",
	Long: `Create a new intent with fast or deep capture mode.

CAPTURE MODES:
  Ultra-fast          Title provided as argument → immediate creation
  Fast TUI (default)  Step-through form (title, type, concept)
  Full TUI (--full)   Step-through form including body textarea
  Deep (--edit)       Full template in $EDITOR

Fast capture is optimized for speed - ideas are saved immediately.
Use --full when you want to add a body description in the form.
Use --edit when you need the complete template in your editor.

Examples:
  camp intent add "Add dark mode"        Ultra-fast capture
  camp intent add                        Fast TUI (3-step form)
  camp intent add --full                 Full TUI (includes body)
  camp intent add -e "Complex feature"   Deep capture with editor
  camp intent add -t feature "New API"   Set type explicitly`,
	Args: cobra.MaximumNArgs(1),
	RunE: runIntentAdd,
}

func init() {
	Cmd.AddCommand(intentAddCmd)

	flags := intentAddCmd.Flags()
	flags.StringP("type", "t", "idea", "Intent type (idea, feature, bug, research, chore)")
	flags.BoolP("edit", "e", false, "Open in $EDITOR for deep capture")
	flags.BoolP("full", "f", false, "Full TUI mode with body textarea")
	flags.Bool("no-commit", false, "Don't create a git commit")
}

func runIntentAdd(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Parse flags
	intentType, _ := cmd.Flags().GetString("type")
	useEditor, _ := cmd.Flags().GetBool("edit")
	fullMode, _ := cmd.Flags().GetBool("full")
	noCommit, _ := cmd.Flags().GetBool("no-commit")

	// Find campaign root
	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	// Create path resolver
	resolver := paths.NewResolverFromConfig(campaignRoot, cfg)

	// Create services
	svc := intent.NewIntentService(campaignRoot, resolver.Intents())
	conceptSvc := concept.NewService(campaignRoot, cfg.Concepts())

	// Ensure directories exist and migrate legacy layout
	if err := svc.EnsureDirectories(ctx); err != nil {
		return camperrors.Wrap(err, "ensuring intent directories")
	}

	// Ultra-fast path: title provided as argument (non-TUI = agent author)
	if len(args) > 0 {
		opts := intent.CreateOptions{
			Title:  args[0],
			Type:   intent.Type(intentType),
			Author: "agent",
		}

		// Deep capture overrides ultra-fast
		if useEditor {
			return runDeepCapture(ctx, svc, resolver.Intents(), cfg, campaignRoot, noCommit, opts)
		}

		return runFastCapture(ctx, svc, resolver.Intents(), cfg, campaignRoot, noCommit, opts)
	}

	// TUI path: use git config author (human)
	author := git.GetUserName(ctx)

	// Build navigation shortcuts map (key → path) for @ completion
	shortcuts := make(map[string]string)
	for key, sc := range cfg.Shortcuts() {
		if sc.HasPath() {
			shortcuts[key] = sc.Path
		}
	}

	// Run BubbleTea TUI
	model, err := runIntentAddTUI(ctx, conceptSvc, tui.AddOptions{
		DefaultType:  intentType,
		FullMode:     fullMode,
		Author:       author,
		CampaignRoot: campaignRoot,
		Shortcuts:    shortcuts,
	})
	if err != nil {
		return err
	}

	// Process intents saved via Ctrl-n during the session
	for _, saved := range model.SavedResults() {
		savedOpts := intent.CreateOptions{
			Title:   saved.Title,
			Type:    intent.Type(saved.Type),
			Concept: saved.Concept,
			Body:    saved.Body,
			Author:  saved.Author,
		}
		if err := runFastCapture(ctx, svc, resolver.Intents(), cfg, campaignRoot, noCommit, savedOpts); err != nil {
			return err
		}
	}

	// Process the final result (from normal save-and-quit)
	result := model.Result()
	if result == nil {
		if len(model.SavedResults()) > 0 {
			// User saved some intents via Ctrl-n, then cancelled the last one
			return nil
		}
		return fmt.Errorf("intent creation cancelled")
	}

	// Build create options from TUI result
	opts := intent.CreateOptions{
		Title:   result.Title,
		Type:    intent.Type(result.Type),
		Concept: result.Concept,
		Body:    result.Body,
		Author:  result.Author,
	}

	// Deep capture if requested
	if useEditor {
		return runDeepCapture(ctx, svc, resolver.Intents(), cfg, campaignRoot, noCommit, opts)
	}

	return runFastCapture(ctx, svc, resolver.Intents(), cfg, campaignRoot, noCommit, opts)
}

// runIntentAddTUI runs the BubbleTea intent creation form.
// Returns the final model so callers can access both Result() and SavedResults().
func runIntentAddTUI(ctx context.Context, conceptSvc concept.Service, opts tui.AddOptions) (*tui.IntentAddModel, error) {
	model := tui.NewIntentAddModel(ctx, conceptSvc, opts)

	p := tea.NewProgram(model, tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		return nil, camperrors.Wrap(err, "TUI error")
	}

	m, ok := finalModel.(tui.IntentAddModel)
	if !ok {
		return nil, fmt.Errorf("unexpected model type: %T", finalModel)
	}

	return &m, nil
}

// runFastCapture creates intent file directly without editor.
func runFastCapture(ctx context.Context, svc *intent.IntentService, intentsDir string, cfg *config.CampaignConfig, campaignRoot string, noCommit bool, opts intent.CreateOptions) error {
	result, err := svc.CreateDirect(ctx, opts)
	if err != nil {
		return camperrors.Wrap(err, "failed to create intent")
	}

	if err := appendIntentAuditEvent(ctx, intentsDir, audit.Event{
		Type:  audit.EventCreate,
		ID:    result.ID,
		Title: result.Title,
		To:    string(result.Status),
	}); err != nil {
		return err
	}

	fmt.Printf("✓ Intent created: %s\n", result.Path)

	// Auto-commit (unless --no-commit)
	if !noCommit {
		files := commit.NormalizeFiles(campaignRoot, result.Path, audit.FilePath(intentsDir))
		commitResult := commit.Intent(ctx, commit.IntentOptions{
			Options: commit.Options{
				CampaignRoot:  campaignRoot,
				CampaignID:    cfg.ID,
				Files:         files,
				SelectiveOnly: true,
			},
			Action:      commit.IntentCreate,
			IntentTitle: opts.Title,
		})
		if commitResult.Message != "" {
			fmt.Printf("  %s\n", commitResult.Message)
		}
	}

	return nil
}

// runDeepCapture opens editor for full template expansion.
func runDeepCapture(ctx context.Context, svc *intent.IntentService, intentsDir string, cfg *config.CampaignConfig, campaignRoot string, noCommit bool, opts intent.CreateOptions) error {
	// Use editor function from editor package
	editorFn := func(ctx context.Context, path string) error {
		return editor.Edit(ctx, path)
	}

	result, err := svc.CreateWithEditor(ctx, opts, editorFn)
	if err != nil {
		if errors.Is(err, camperrors.ErrCancelled) {
			return fmt.Errorf("intent creation cancelled")
		}
		return camperrors.Wrap(err, "failed to create intent")
	}

	if err := appendIntentAuditEvent(ctx, intentsDir, audit.Event{
		Type:  audit.EventCreate,
		ID:    result.ID,
		Title: result.Title,
		To:    string(result.Status),
	}); err != nil {
		return err
	}

	fmt.Printf("✓ Intent created: %s\n", result.Path)

	// Auto-commit (unless --no-commit)
	if !noCommit {
		files := commit.NormalizeFiles(campaignRoot, result.Path, audit.FilePath(intentsDir))
		commitResult := commit.Intent(ctx, commit.IntentOptions{
			Options: commit.Options{
				CampaignRoot:  campaignRoot,
				CampaignID:    cfg.ID,
				Files:         files,
				SelectiveOnly: true,
			},
			Action:      commit.IntentCreate,
			IntentTitle: opts.Title,
		})
		if commitResult.Message != "" {
			fmt.Printf("  %s\n", commitResult.Message)
		}
	}

	return nil
}
