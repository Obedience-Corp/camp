package intent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	wkcmd "github.com/Obedience-Corp/camp/internal/commands/workitem"
	"github.com/Obedience-Corp/camp/internal/concept"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/editor"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/intent/audit"
	"github.com/Obedience-Corp/camp/internal/intent/tui"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/ledger"
	navtui "github.com/Obedience-Corp/camp/internal/nav/tui"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/pathutil"
)

// noOptCampaign is the NoOptDefVal for the --campaign flag. Cobra requires a
// non-empty string to allow a flag without a value (bare --campaign). The
// resolver treats this sentinel identically to "" so no real campaign name is
// reserved.
const noOptCampaign = "\x00pick"

var intentAddCmd = newIntentAddCommand()

func newIntentAddCommand() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
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
  --note              Create a note instead of a lifecycle intent
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
  camp intent add --note                 Note TUI (title + body, no type/concept)
  camp intent add --note "Meeting note" --body "Follow up next week"
  camp intent add -e "Complex feature"   Deep capture with editor
  camp intent add -t feature "New API"   Set type explicitly
  camp intent add "Fix login" --body "The login page returns 500"
  camp intent add "Migrate DB" --body-file spec.md --concept projects/camp
  echo "body" | camp intent add "Idea" --body-file -`,
	}
	jsonRequested := func() bool { return jsonOut }
	cmd.Args = jsoncontract.Args(IntentJSONVersion, jsonRequested, validateIntentAddArgs)
	cmd.RunE = jsoncontract.RunE(IntentJSONVersion, jsonRequested, runIntentAdd)
	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(IntentJSONVersion, jsonRequested))

	flags := cmd.Flags()
	flags.StringP("type", "t", "idea", "Intent type (idea, feature, bug, research, chore)")
	flags.BoolP("edit", "e", false, "Open in $EDITOR for deep capture")
	flags.Bool("full", false, "Full TUI mode with body textarea")
	flags.StringP("campaign", "c", "", "Target campaign by name or ID; omit value to pick interactively")
	flags.Bool("no-commit", false, "Don't create a git commit")
	flags.Lookup("campaign").NoOptDefVal = noOptCampaign
	flags.BoolVar(&jsonOut, "json", false, "emit a structured JSON result")

	// Programmatic (agent) flags
	flags.String("body", "", "Set intent body as a literal string")
	flags.String("body-file", "", "Read intent body from file (- for stdin, 10 MiB cap)")
	flags.String("concept", "", "Set the concept field (e.g., projects/camp)")
	flags.Bool("note", false, "Create a note instead of a lifecycle intent")
	flags.String("author", "", "Override the default author attribution")
	flags.StringArray("tag", nil, "Add a tag (repeatable)")

	return cmd
}

func init() {
	Cmd.AddCommand(intentAddCmd)
}

func runIntentAdd(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Parse flags
	intentType, _ := cmd.Flags().GetString("type")
	useEditor, _ := cmd.Flags().GetBool("edit")
	fullMode, _ := cmd.Flags().GetBool("full")
	jsonOut, _ := cmd.Flags().GetBool("json")
	targetCampaign, _ := cmd.Flags().GetString("campaign")
	noCommit, _ := cmd.Flags().GetBool("no-commit")
	createNote, _ := cmd.Flags().GetBool("note")
	targetCampaign, args = normalizeIntentAddCampaignArgs(args, targetCampaign)
	conceptFlag, _ := cmd.Flags().GetString("concept")
	authorFlag, _ := cmd.Flags().GetString("author")
	tagsFlag, _ := cmd.Flags().GetStringArray("tag")

	if createNote && cmd.Flags().Changed("concept") {
		return camperrors.Wrap(camperrors.ErrInvalidInput, "--note cannot be combined with --concept")
	}
	if createNote && cmd.Flags().Changed("type") {
		return camperrors.Wrap(camperrors.ErrInvalidInput, "--note cannot be combined with --type")
	}
	if fullMode && jsonOut {
		return camperrors.Wrap(camperrors.ErrInvalidInput, "--json and --full are mutually exclusive")
	}
	if jsonOut && len(args) == 0 {
		return camperrors.Wrap(camperrors.ErrInvalidInput, "--json requires a title argument")
	}

	// Resolve body from --body / --body-file (mutual exclusivity checked inside)
	body, bodySet, err := resolveBody(cmd)
	if err != nil {
		return err
	}

	// --full + body flags = usage error (body in TUI conflicts with programmatic body)
	if fullMode && bodySet {
		return camperrors.Wrap(camperrors.ErrInvalidInput, "--full and --body/--body-file are mutually exclusive")
	}

	campaignResolver := newIntentAddCampaignResolver(cmd.ErrOrStderr())
	cfg, campaignRoot, err := campaignResolver.resolve(ctx, targetCampaign, cmd.Flags().Changed("campaign"))
	if err != nil {
		return err
	}
	campaignRoot, err = pathutil.ResolveRoot(campaignRoot)
	if err != nil {
		return camperrors.Wrap(err, "resolving campaign root")
	}

	// Create path resolver
	resolver := paths.NewResolverFromConfig(campaignRoot, cfg)

	// Create services
	svc := intent.NewIntentService(campaignRoot, resolver.Intents())
	svc.SetLedger(ledger.NewFromRoot(ctx, campaignRoot, ledger.WarnTo(cmd.ErrOrStderr())))
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
			Tags:    tagsFlag,
		}

		if createNote {
			if useEditor {
				return runDeepNoteCapture(ctx, svc, resolver.Intents(), cfg, campaignRoot, noCommit, opts)
			}
			return runNoteCapture(ctx, svc, resolver.Intents(), cfg, campaignRoot, noCommit, opts)
		}

		// Deep capture overrides ultra-fast; body flags pre-fill the template
		if useEditor {
			return runDeepCaptureWithOutput(ctx, svc, resolver.Intents(), cfg, campaignRoot, noCommit, opts, cmd.OutOrStdout(), jsonOut)
		}

		return runFastCaptureWithOutput(ctx, svc, resolver.Intents(), cfg, campaignRoot, noCommit, opts, cmd.OutOrStdout(), jsonOut)
	}

	// No title argument: non-TTY always requires a title (can't launch TUI)
	// Programmatic flags like --body/--concept/--author supplement a title,
	// they don't replace it.
	if !navtui.IsTerminal() {
		return camperrors.Wrap(camperrors.ErrInvalidInput, "title argument required in non-interactive mode (use 'camp intent add <title>')")
	}

	// TUI path: use git config author (human), unless --author overrides
	author := git.GetUserName(ctx)
	if cmd.Flags().Changed("author") {
		author = authorFlag
	}

	// Build navigation shortcuts map (key -> path) for @ completion
	shortcuts := navigationShortcuts(cfg)

	// Run BubbleTea TUI
	model, err := runIntentAddTUI(ctx, conceptSvc, tui.AddOptions{
		DefaultType:   intentType,
		FullMode:      fullMode,
		NoteMode:      createNote,
		Author:        author,
		CampaignRoot:  campaignRoot,
		Shortcuts:     shortcuts,
		AvailableTags: cfg.IntentTags(),
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
			Tags:    mergeTags(tagsFlag, saved.Tags),
		}
		if createNote {
			if err := runNoteCapture(ctx, svc, resolver.Intents(), cfg, campaignRoot, noCommit, savedOpts); err != nil {
				return err
			}
			continue
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
		return intent.ErrCancelled
	}

	// Build create options from TUI result
	opts := intent.CreateOptions{
		Title:   result.Title,
		Type:    intent.Type(result.Type),
		Concept: result.Concept,
		Body:    result.Body,
		Author:  result.Author,
		Tags:    mergeTags(tagsFlag, result.Tags),
	}

	if createNote {
		return runNoteCapture(ctx, svc, resolver.Intents(), cfg, campaignRoot, noCommit, opts)
	}

	// Deep capture if requested
	if useEditor {
		return runDeepCaptureWithOutput(ctx, svc, resolver.Intents(), cfg, campaignRoot, noCommit, opts, cmd.OutOrStdout(), jsonOut)
	}

	return runFastCaptureWithOutput(ctx, svc, resolver.Intents(), cfg, campaignRoot, noCommit, opts, cmd.OutOrStdout(), jsonOut)
}

func navigationShortcuts(cfg *config.CampaignConfig) map[string]string {
	shortcuts := make(map[string]string)
	for key, sc := range cfg.Shortcuts() {
		if sc.HasPath() {
			shortcuts[key] = sc.Path
		}
	}
	return shortcuts
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
	updateAccess  func(context.Context, string) error
	pickCampaign  func(context.Context, *config.Registry) (config.RegisteredCampaign, error)
}

func newIntentAddCampaignResolver(stderr io.Writer) intentAddCampaignResolver {
	return intentAddCampaignResolver{
		stderr:        stderr,
		isInteractive: navtui.IsTerminal,
		loadCurrent:   config.LoadCampaignConfigFromCwd,
		loadRegistry:  config.LoadRegistry,
		loadCampaign:  config.LoadCampaignConfig,
		updateAccess:  updateIntentAddRegistryLastAccess,
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
		return nil, "", camperrors.Wrap(camperrors.ErrNotInitialized, "no campaigns registered (use 'camp init' to create one)")
	}

	var selected config.RegisteredCampaign
	if targetCampaign == "" {
		if !r.isInteractive() {
			return nil, "", camperrors.Wrap(camperrors.ErrInvalidInput, "campaign name required in non-interactive mode (use 'camp intent add --campaign <name> [title]')")
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

	if r.updateAccess != nil {
		_ = r.updateAccess(ctx, selected.ID)
	}

	cfg, err := r.loadCampaign(ctx, selected.Path)
	if err != nil {
		return nil, "", camperrors.Wrapf(err, "load target campaign %s", selected.Path)
	}

	return cfg, selected.Path, nil
}

func updateIntentAddRegistryLastAccess(ctx context.Context, id string) error {
	return config.UpdateRegistry(ctx, func(reg *config.Registry) error {
		reg.UpdateLastAccess(id)
		return nil
	})
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
		return nil, camperrors.Wrapf(camperrors.ErrInvalidInput, "unexpected model type: %T", finalModel)
	}

	return &m, nil
}

// runFastCapture creates intent file directly without editor.
func runFastCapture(ctx context.Context, svc *intent.IntentService, intentsDir string, cfg *config.CampaignConfig, campaignRoot string, noCommit bool, opts intent.CreateOptions) error {
	return runFastCaptureWithOutput(ctx, svc, intentsDir, cfg, campaignRoot, noCommit, opts, os.Stdout, false)
}

func runFastCaptureWithOutput(ctx context.Context, svc *intent.IntentService, intentsDir string, cfg *config.CampaignConfig, campaignRoot string, noCommit bool, opts intent.CreateOptions, output io.Writer, jsonOut bool) error {
	result, err := svc.CreateDirect(ctx, opts)
	if err != nil {
		return camperrors.Wrap(err, "failed to create intent")
	}

	return finalizeCreatedIntentWithOutput(ctx, result, intentsDir, cfg, campaignRoot, noCommit, output, jsonOut)
}

// runDeepCapture opens editor for full template expansion.
func runDeepCapture(ctx context.Context, svc *intent.IntentService, intentsDir string, cfg *config.CampaignConfig, campaignRoot string, noCommit bool, opts intent.CreateOptions) error {
	return runDeepCaptureWithOutput(ctx, svc, intentsDir, cfg, campaignRoot, noCommit, opts, os.Stdout, false)
}

func runDeepCaptureWithOutput(ctx context.Context, svc *intent.IntentService, intentsDir string, cfg *config.CampaignConfig, campaignRoot string, noCommit bool, opts intent.CreateOptions, output io.Writer, jsonOut bool) error {
	// Use editor function from editor package
	editorFn := func(ctx context.Context, path string) error {
		return editor.Edit(ctx, path)
	}

	result, err := svc.CreateWithEditor(ctx, opts, editorFn)
	if err != nil {
		if errors.Is(err, camperrors.ErrCancelled) {
			return intent.ErrCancelled
		}
		return camperrors.Wrap(err, "failed to create intent")
	}

	return finalizeCreatedIntentWithOutput(ctx, result, intentsDir, cfg, campaignRoot, noCommit, output, jsonOut)
}

// runDeepNoteCapture opens the note template in $EDITOR and saves it to notes/.
func runDeepNoteCapture(ctx context.Context, svc *intent.IntentService, intentsDir string, cfg *config.CampaignConfig, campaignRoot string, noCommit bool, opts intent.CreateOptions) error {
	editorFn := func(ctx context.Context, path string) error {
		return editor.Edit(ctx, path)
	}
	opts.Category = intent.CategoryNote
	opts.Type = ""
	opts.Concept = ""

	result, err := svc.CreateWithEditor(ctx, opts, editorFn)
	if err != nil {
		if errors.Is(err, camperrors.ErrCancelled) {
			return intent.ErrCancelled
		}
		return camperrors.Wrap(err, "failed to create note")
	}

	return finalizeCreatedNote(ctx, result, intentsDir, cfg, campaignRoot, noCommit)
}

func finalizeCreatedIntent(ctx context.Context, result *intent.Intent, intentsDir string, cfg *config.CampaignConfig, campaignRoot string, noCommit bool) error {
	return finalizeCreatedIntentWithOutput(ctx, result, intentsDir, cfg, campaignRoot, noCommit, os.Stdout, false)
}

func finalizeCreatedIntentWithOutput(ctx context.Context, result *intent.Intent, intentsDir string, cfg *config.CampaignConfig, campaignRoot string, noCommit bool, output io.Writer, jsonOut bool) error {
	if err := appendIntentAuditEvent(ctx, intentsDir, audit.Event{
		Type:  audit.EventCreate,
		ID:    result.ID,
		Title: result.Title,
		To:    string(result.Status),
	}); err != nil {
		return err
	}

	if jsonOut {
		if err := outputIntentAddPayload(output, campaignRoot, result); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintf(output, "✓ Intent created: %s\n", result.Path); err != nil {
			return err
		}
	}

	// Auto-commit (unless --no-commit)
	if !noCommit {
		opts := wkcmd.AmbientCommitOptions(ctx, campaignRoot, cfg.ID, os.Stderr)
		opts.Files = commit.NormalizeFiles(campaignRoot, result.Path, audit.FilePath(intentsDir))
		opts.SelectiveOnly = true
		commitResult := commit.Intent(ctx, commit.IntentOptions{
			Options:     opts,
			Action:      commit.IntentCreate,
			IntentTitle: result.Title,
		})
		if !jsonOut && commitResult.Message != "" {
			if _, err := fmt.Fprintf(output, "  %s\n", commitResult.Message); err != nil {
				return err
			}
		}
		commit.WarnIfSkipped(os.Stderr, commitResult)
	}

	return nil
}
