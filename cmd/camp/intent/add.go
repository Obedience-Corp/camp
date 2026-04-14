package intent

import (
	"context"
	"errors"
	"fmt"
	"io"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/Obedience-Corp/camp/internal/concept"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/editor"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/intent/audit"
	"github.com/Obedience-Corp/camp/internal/intent/tui"
	navtui "github.com/Obedience-Corp/camp/internal/nav/tui"
	"github.com/Obedience-Corp/camp/internal/paths"
)

// noOptCampaign is the NoOptDefVal for the --campaign flag. Cobra requires a
// non-empty string to allow a flag without a value (bare --campaign). The
// resolver treats this sentinel identically to "" so no real campaign name is
// reserved.
const noOptCampaign = "\x00pick"

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

PROGRAMMATIC (agent) FLAGS:
  --body              Set intent body from a literal string
  --body-file         Read intent body from a file (- for stdin)
  --concept           Set the concept field (e.g., "projects/camp")
  --author            Override the default author attribution

  --body and --body-file are mutually exclusive.
  --full + body flags is a usage error.
  --edit + body flags pre-fills the editor template.

Examples:
  camp intent add "Add dark mode"        Ultra-fast capture
  camp intent add -c obey-campaign "Add dark mode"
  camp intent add                        Fast TUI (3-step form)
  camp intent add --campaign             Pick a target campaign interactively
  camp intent add --full                 Full TUI (includes body)
  camp intent add -e "Complex feature"   Deep capture with editor
  camp intent add -t feature "New API"   Set type explicitly
  camp intent add "Fix login" --body "The login page returns 500"
  camp intent add "Migrate DB" --body-file spec.md --concept projects/camp
  echo "body" | camp intent add "Idea" --body-file -`,
	Args: validateIntentAddArgs,
	RunE: runIntentAdd,
}

func init() {
	Cmd.AddCommand(intentAddCmd)

	flags := intentAddCmd.Flags()
	flags.StringP("type", "t", "idea", "Intent type (idea, feature, bug, research, chore)")
	flags.BoolP("edit", "e", false, "Open in $EDITOR for deep capture")
	flags.BoolP("full", "f", false, "Full TUI mode with body textarea")
	flags.StringP("campaign", "c", "", "Target campaign by name or ID; omit value to pick interactively")
	flags.Bool("no-commit", false, "Don't create a git commit")
	flags.Lookup("campaign").NoOptDefVal = noOptCampaign

	// Programmatic (agent) flags
	flags.String("body", "", "Set intent body as a literal string")
	flags.String("body-file", "", "Read intent body from file (- for stdin, 10 MiB cap)")
	flags.String("concept", "", "Set the concept field (e.g., projects/camp)")
	flags.String("author", "", "Override the default author attribution")
}

func runIntentAdd(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Parse flags
	intentType, _ := cmd.Flags().GetString("type")
	useEditor, _ := cmd.Flags().GetBool("edit")
	fullMode, _ := cmd.Flags().GetBool("full")
	targetCampaign, _ := cmd.Flags().GetString("campaign")
	noCommit, _ := cmd.Flags().GetBool("no-commit")
	targetCampaign, args = normalizeIntentAddCampaignArgs(args, targetCampaign)
	conceptFlag, _ := cmd.Flags().GetString("concept")
	authorFlag, _ := cmd.Flags().GetString("author")

	// Resolve body from --body / --body-file (mutual exclusivity checked inside)
	body, bodySet, err := resolveBody(cmd)
	if err != nil {
		return err
	}

	// --full + body flags = usage error (body in TUI conflicts with programmatic body)
	if fullMode && bodySet {
		return fmt.Errorf("--full and --body/--body-file are mutually exclusive")
	}

	campaignResolver := newIntentAddCampaignResolver(cmd.ErrOrStderr())
	cfg, campaignRoot, err := campaignResolver.resolve(ctx, targetCampaign, cmd.Flags().Changed("campaign"))
	if err != nil {
		return err
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

	// Ultra-fast path: title provided as argument
	if len(args) > 0 {
		// Default author for CLI-with-title is "agent", but --author overrides
		author := "agent"
		if cmd.Flags().Changed("author") {
			author = authorFlag
		}

		opts := intent.CreateOptions{
			Title:   args[0],
			Type:    intent.Type(intentType),
			Author:  author,
			Body:    body,
			Concept: conceptFlag,
		}

		// Deep capture overrides ultra-fast; body flags pre-fill the template
		if useEditor {
			return runDeepCapture(ctx, svc, resolver.Intents(), cfg, campaignRoot, noCommit, opts)
		}

		return runFastCapture(ctx, svc, resolver.Intents(), cfg, campaignRoot, noCommit, opts)
	}

	// No title argument: non-TTY always requires a title (can't launch TUI)
	// Programmatic flags like --body/--concept/--author supplement a title,
	// they don't replace it.
	if !navtui.IsTerminal() {
		return fmt.Errorf("title argument required in non-interactive mode\n       Usage: camp intent add <title> [flags]")
	}

	// TUI path: use git config author (human), unless --author overrides
	author := git.GetUserName(ctx)
	if cmd.Flags().Changed("author") {
		author = authorFlag
	}

	// Build navigation shortcuts map (key -> path) for @ completion
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

func validateIntentAddArgs(cmd *cobra.Command, args []string) error {
	maxArgs := 1

	targetCampaign, _ := cmd.Flags().GetString("campaign")
	if targetCampaign == noOptCampaign {
		maxArgs = 2
	}

	return cobra.MaximumNArgs(maxArgs)(cmd, args)
}

func normalizeIntentAddCampaignArgs(args []string, targetCampaign string) (string, []string) {
	if targetCampaign != noOptCampaign {
		return targetCampaign, args
	}
	if len(args) > 1 {
		return args[0], args[1:]
	}
	return "", args
}

type intentAddCampaignResolver struct {
	stderr        io.Writer
	isInteractive func() bool
	loadCurrent   func(context.Context) (*config.CampaignConfig, string, error)
	loadRegistry  func(context.Context) (*config.Registry, error)
	loadCampaign  func(context.Context, string) (*config.CampaignConfig, error)
	saveRegistry  func(context.Context, *config.Registry) error
	pickCampaign  func(context.Context, *config.Registry) (config.RegisteredCampaign, error)
}

func newIntentAddCampaignResolver(stderr io.Writer) intentAddCampaignResolver {
	return intentAddCampaignResolver{
		stderr:        stderr,
		isInteractive: navtui.IsTerminal,
		loadCurrent:   config.LoadCampaignConfigFromCwd,
		loadRegistry:  config.LoadRegistry,
		loadCampaign:  config.LoadCampaignConfig,
		saveRegistry:  config.SaveRegistry,
		pickCampaign:  cmdutil.PickCampaign,
	}
}

func (r intentAddCampaignResolver) resolve(ctx context.Context, targetCampaign string, targetChanged bool) (*config.CampaignConfig, string, error) {
	// Normalize the Cobra NoOptDefVal sentinel — bare --campaign means "pick interactively".
	if targetCampaign == noOptCampaign {
		targetCampaign = ""
	}

	if !targetChanged {
		cfg, campaignRoot, err := r.loadCurrent(ctx)
		if err != nil {
			return nil, "", camperrors.Wrap(err, "not in a campaign directory")
		}
		return cfg, campaignRoot, nil
	}

	reg, err := r.loadRegistry(ctx)
	if err != nil {
		return nil, "", camperrors.Wrap(err, "load registry")
	}
	if reg.Len() == 0 {
		return nil, "", fmt.Errorf("no campaigns registered (use 'camp init' to create one)")
	}

	var selected config.RegisteredCampaign
	if targetCampaign == "" {
		if !r.isInteractive() {
			return nil, "", fmt.Errorf("campaign name required in non-interactive mode\n       Usage: camp intent add --campaign <name> [title]")
		}
		selected, err = r.pickCampaign(ctx, reg)
		if err != nil {
			return nil, "", err
		}
	} else {
		selected, err = cmdutil.ResolveCampaignSelection(targetCampaign, reg, r.stderr)
		if err != nil {
			return nil, "", err
		}
	}

	reg.UpdateLastAccess(selected.ID)
	if r.saveRegistry != nil {
		_ = r.saveRegistry(ctx, reg)
	}

	cfg, err := r.loadCampaign(ctx, selected.Path)
	if err != nil {
		return nil, "", camperrors.Wrapf(err, "load target campaign %s", selected.Path)
	}

	return cfg, selected.Path, nil
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

	return finalizeCreatedIntent(ctx, result, intentsDir, cfg, campaignRoot, noCommit)
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

	return finalizeCreatedIntent(ctx, result, intentsDir, cfg, campaignRoot, noCommit)
}

func finalizeCreatedIntent(ctx context.Context, result *intent.Intent, intentsDir string, cfg *config.CampaignConfig, campaignRoot string, noCommit bool) error {
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
			IntentTitle: result.Title,
		})
		if commitResult.Message != "" {
			fmt.Printf("  %s\n", commitResult.Message)
		}
	}

	return nil
}
