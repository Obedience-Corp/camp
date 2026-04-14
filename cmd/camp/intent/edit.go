package intent

import (
	"context"
	"fmt"

	"github.com/ktr0731/go-fuzzyfinder"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/editor"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/intent/audit"
	navtui "github.com/Obedience-Corp/camp/internal/nav/tui"
	"github.com/Obedience-Corp/camp/internal/paths"
)

var intentEditCmd = &cobra.Command{
	Use:   "edit [id]",
	Short: "Edit an existing intent",
	Long: `Edit an intent in your preferred editor or programmatically via flags.

If no programmatic flags are given, opens the intent in $EDITOR.
If any programmatic flag is present, applies the update directly and
emits an audit event — no editor is launched.

PICKER / EDITOR PATH:
  If ID is provided, opens the intent directly (supports partial matching).
  If no ID is provided, shows a fuzzy picker to select an intent.

PROGRAMMATIC FLAGS (skip $EDITOR):
  --title            Set a new title
  --body             Replace the body with a literal string
  --body-file        Replace the body from a file (- for stdin)
  --append-body      Append text to the existing body
  --append-body-file Append text from a file (- for stdin)
  --set-type         Change the intent type
  --set-status       Change the intent status
  --set-concept      Change the concept field
  --priority         Change priority (low, medium, high)
  --horizon          Change horizon (now, next, later, someday)
  --author           Override the author attribution

MUTUAL EXCLUSIVITY:
  --body vs --body-file
  --append-body vs --append-body-file
  --body/--body-file vs --append-body/--append-body-file (replace vs append)

FILTER FLAGS (for picker only, not update targets):
  -s/--status        Filter picker by status
  -t/--type          Filter picker by type
  -p/--project       Filter picker by project/concept

Examples:
  camp intent edit                                Interactive picker + $EDITOR
  camp intent edit retry-logic                    Direct edit by partial ID
  camp intent edit --status active                Picker filtered by status
  camp intent edit retry --title "Retry with backoff"
  camp intent edit retry --body "New description"
  camp intent edit retry --append-body "Additional note"
  camp intent edit retry --set-type feature --priority high
  echo "details" | camp intent edit retry --body-file -`,
	Args: cobra.MaximumNArgs(1),
	RunE: runIntentEdit,
}

func init() {
	Cmd.AddCommand(intentEditCmd)

	flags := intentEditCmd.Flags()

	// Filter flags (picker only)
	flags.StringP("status", "s", "", "Filter picker by status")
	flags.StringP("type", "t", "", "Filter picker by type")
	flags.StringP("project", "p", "", "Filter picker by project")

	// Programmatic update flags
	flags.String("title", "", "Set a new title")
	flags.String("body", "", "Replace the intent body")
	flags.String("body-file", "", "Replace body from file (- for stdin, 10 MiB cap)")
	flags.String("append-body", "", "Append text to the existing body")
	flags.String("append-body-file", "", "Append text from file (- for stdin, 10 MiB cap)")
	flags.String("set-type", "", "Change the intent type (idea, feature, bug, research, chore)")
	flags.String("set-status", "", "Change the intent status")
	flags.String("set-concept", "", "Change the concept field")
	flags.String("priority", "", "Change priority (low, medium, high)")
	flags.String("horizon", "", "Change horizon (now, next, later, someday)")
	flags.String("author", "", "Override the author attribution")
	flags.Bool("no-commit", false, "Don't create a git commit")
}

func runIntentEdit(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Parse filter flags
	statusFilter, _ := cmd.Flags().GetString("status")
	typeFilter, _ := cmd.Flags().GetString("type")
	projectFilter, _ := cmd.Flags().GetString("project")

	// Validate mutual exclusivity of body flags
	if err := validateEditBodyFlags(cmd); err != nil {
		return err
	}

	// Find campaign root
	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	// Create path resolver and service
	resolver := paths.NewResolverFromConfig(campaignRoot, cfg)
	svc := intent.NewIntentService(campaignRoot, resolver.Intents())
	if err := svc.EnsureDirectories(ctx); err != nil {
		return camperrors.Wrap(err, "failed to ensure intent directories")
	}

	// Determine if we're in programmatic mode
	programmatic := hasProgrammaticEditFlags(cmd)
	noCommit, _ := cmd.Flags().GetBool("no-commit")

	var selectedIntent *intent.Intent

	if len(args) > 0 {
		// Direct ID provided - resolve
		partialID := args[0]
		selectedIntent, err = resolveIntentByPartialID(ctx, svc, partialID)
		if err != nil {
			return err
		}
	} else {
		// No ID + programmatic flags + no TTY = deterministic error
		if programmatic && !navtui.IsTerminal() {
			return camperrors.Wrap(camperrors.ErrInvalidInput, "intent ID required in non-interactive mode")
		}
		// No ID - show fuzzy picker
		selectedIntent, err = pickIntent(ctx, svc, statusFilter, typeFilter, projectFilter)
		if err != nil {
			if camperrors.Is(err, fuzzyfinder.ErrAbort) {
				return camperrors.Wrap(camperrors.ErrCancelled, "edit")
			}
			return err
		}
	}

	// Programmatic path: apply updates directly, skip $EDITOR
	if programmatic {
		return runProgrammaticEdit(ctx, cmd, svc, selectedIntent, resolver.Intents(), cfg, campaignRoot, noCommit)
	}

	// Editor path: open in $EDITOR
	return runEditorEdit(ctx, svc, selectedIntent)
}

// runEditorEdit opens the intent in $EDITOR (the original behavior).
func runEditorEdit(ctx context.Context, svc *intent.IntentService, i *intent.Intent) error {
	editorFn := func(ctx context.Context, path string) error {
		return editor.Edit(ctx, path)
	}

	updated, err := svc.Edit(ctx, i.ID, editorFn)
	if err != nil {
		return camperrors.Wrap(err, "failed to edit intent")
	}

	fmt.Printf("Intent saved: %s\n", updated.Path)
	return nil
}

// runProgrammaticEdit builds UpdateOptions from flags, calls UpdateDirect,
// and emits an audit event with per-field change records.
func runProgrammaticEdit(
	ctx context.Context,
	cmd *cobra.Command,
	svc *intent.IntentService,
	target *intent.Intent,
	intentsDir string,
	cfg *config.CampaignConfig,
	campaignRoot string,
	noCommit bool,
) error {
	opts, err := buildUpdateOptions(cmd)
	if err != nil {
		return err
	}

	updated, changes, err := svc.UpdateDirect(ctx, target.ID, opts)
	if err != nil {
		return camperrors.Wrap(err, "failed to update intent")
	}

	if len(changes) == 0 {
		fmt.Println("No changes detected")
		return nil
	}

	// Emit audit event with field-level changes
	event := audit.Event{
		Type:    audit.EventEdit,
		ID:      updated.ID,
		Title:   updated.Title,
		Changes: changes,
	}
	// Populate From/To on status changes for query consistency with move events
	for _, c := range changes {
		if c.Field == "status" {
			event.From = c.Old
			event.To = c.New
			break
		}
	}
	if err := appendIntentAuditEvent(ctx, intentsDir, event); err != nil {
		return err
	}

	fmt.Printf("Intent updated: %s\n", updated.Path)
	for _, c := range changes {
		fmt.Printf("  %s: %q -> %q\n", c.Field, c.Old, c.New)
	}

	// Auto-commit (unless --no-commit)
	if !noCommit {
		filesToCommit := []string{updated.Path, audit.FilePath(intentsDir)}
		// Status changes move the file — stage the old path deletion too
		if target.Path != updated.Path {
			filesToCommit = append(filesToCommit, target.Path)
		}
		files := commit.NormalizeFiles(campaignRoot, filesToCommit...)
		commitResult := commit.Intent(ctx, commit.IntentOptions{
			Options: commit.Options{
				CampaignRoot:  campaignRoot,
				CampaignID:    cfg.ID,
				Files:         files,
				SelectiveOnly: true,
			},
			Action:      commit.IntentEdit,
			IntentTitle: updated.Title,
		})
		if commitResult.Message != "" {
			fmt.Printf("  %s\n", commitResult.Message)
		}
	}

	return nil
}

// buildUpdateOptions constructs an UpdateOptions from CLI flags.
func buildUpdateOptions(cmd *cobra.Command) (intent.UpdateOptions, error) {
	var opts intent.UpdateOptions

	if cmd.Flags().Changed("title") {
		v, _ := cmd.Flags().GetString("title")
		opts.Title = &v
	}

	// Body replacement: --body or --body-file
	if cmd.Flags().Changed("body") {
		v, _ := cmd.Flags().GetString("body")
		opts.Body = &v
	} else if cmd.Flags().Changed("body-file") {
		path, _ := cmd.Flags().GetString("body-file")
		content, err := readBodySource(path)
		if err != nil {
			return opts, err
		}
		opts.Body = &content
	}

	// Body append: --append-body or --append-body-file
	if cmd.Flags().Changed("append-body") {
		v, _ := cmd.Flags().GetString("append-body")
		opts.Append = &v
	} else if cmd.Flags().Changed("append-body-file") {
		path, _ := cmd.Flags().GetString("append-body-file")
		content, err := readBodySource(path)
		if err != nil {
			return opts, err
		}
		opts.Append = &content
	}

	if cmd.Flags().Changed("set-type") {
		v, _ := cmd.Flags().GetString("set-type")
		t := intent.Type(v)
		opts.Type = &t
	}

	if cmd.Flags().Changed("set-status") {
		v, _ := cmd.Flags().GetString("set-status")
		s := intent.Status(v)
		opts.Status = &s
	}

	if cmd.Flags().Changed("set-concept") {
		v, _ := cmd.Flags().GetString("set-concept")
		opts.Concept = &v
	}

	if cmd.Flags().Changed("priority") {
		v, _ := cmd.Flags().GetString("priority")
		p := intent.Priority(v)
		opts.Priority = &p
	}

	if cmd.Flags().Changed("horizon") {
		v, _ := cmd.Flags().GetString("horizon")
		h := intent.Horizon(v)
		opts.Horizon = &h
	}

	if cmd.Flags().Changed("author") {
		v, _ := cmd.Flags().GetString("author")
		opts.Author = &v
	}

	return opts, nil
}

// hasProgrammaticEditFlags returns true if any update flag (as opposed to
// filter flag) was explicitly set.
func hasProgrammaticEditFlags(cmd *cobra.Command) bool {
	programmaticFlags := []string{
		"title", "body", "body-file", "append-body", "append-body-file",
		"set-type", "set-status", "set-concept", "priority", "horizon", "author",
	}
	for _, name := range programmaticFlags {
		if cmd.Flags().Changed(name) {
			return true
		}
	}
	return false
}

// validateEditBodyFlags enforces mutual exclusivity among body-related flags.
func validateEditBodyFlags(cmd *cobra.Command) error {
	bodySet := cmd.Flags().Changed("body")
	bodyFileSet := cmd.Flags().Changed("body-file")
	appendSet := cmd.Flags().Changed("append-body")
	appendFileSet := cmd.Flags().Changed("append-body-file")

	if bodySet && bodyFileSet {
		return camperrors.Wrap(camperrors.ErrInvalidInput, "--body and --body-file are mutually exclusive")
	}
	if appendSet && appendFileSet {
		return camperrors.Wrap(camperrors.ErrInvalidInput, "--append-body and --append-body-file are mutually exclusive")
	}
	if (bodySet || bodyFileSet) && (appendSet || appendFileSet) {
		return camperrors.Wrap(camperrors.ErrInvalidInput, "--body/--body-file and --append-body/--append-body-file are mutually exclusive")
	}
	return nil
}

