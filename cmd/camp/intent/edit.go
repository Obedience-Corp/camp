package intent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ktr0731/go-fuzzyfinder"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/editor"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/intent/audit"
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
		// No ID - show fuzzy picker
		selectedIntent, err = pickIntent(ctx, svc, statusFilter, typeFilter, projectFilter)
		if err != nil {
			if errors.Is(err, fuzzyfinder.ErrAbort) {
				return fmt.Errorf("edit cancelled")
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
	auditChanges := make([]audit.FieldChange, len(changes))
	for i, c := range changes {
		auditChanges[i] = audit.FieldChange{
			Field: c.Field,
			Old:   c.Old,
			New:   c.New,
		}
	}

	if err := appendIntentAuditEvent(ctx, intentsDir, audit.Event{
		Type:    audit.EventEdit,
		ID:      updated.ID,
		Title:   updated.Title,
		Changes: auditChanges,
	}); err != nil {
		return err
	}

	fmt.Printf("Intent updated: %s\n", updated.Path)
	for _, c := range changes {
		fmt.Printf("  %s: %q -> %q\n", c.Field, c.Old, c.New)
	}

	// Auto-commit (unless --no-commit)
	if !noCommit {
		files := commit.NormalizeFiles(campaignRoot, updated.Path, audit.FilePath(intentsDir))
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
		return fmt.Errorf("--body and --body-file are mutually exclusive")
	}
	if appendSet && appendFileSet {
		return fmt.Errorf("--append-body and --append-body-file are mutually exclusive")
	}
	if (bodySet || bodyFileSet) && (appendSet || appendFileSet) {
		return fmt.Errorf("--body/--body-file and --append-body/--append-body-file are mutually exclusive (replace vs append)")
	}
	return nil
}

// resolveIntentByPartialID finds an intent by partial ID matching.
func resolveIntentByPartialID(ctx context.Context, svc *intent.IntentService, partialID string) (*intent.Intent, error) {
	// First try exact match via Find (which supports fuzzy)
	result, err := svc.Find(ctx, partialID)
	if err == nil {
		return result, nil
	}

	// If Find failed with not found, provide helpful error
	if errors.Is(err, camperrors.ErrNotFound) {
		return nil, fmt.Errorf("intent not found: %s", partialID)
	}

	return nil, camperrors.Wrap(err, "failed to find intent")
}

// pickIntent shows a fuzzy picker for intent selection.
func pickIntent(ctx context.Context, svc *intent.IntentService, status, typ, project string) (*intent.Intent, error) {
	// Build list options
	opts := &intent.ListOptions{
		Concept:  project, // project from CLI maps to Concept field
		SortBy:   "updated",
		SortDesc: true,
	}

	if status != "" {
		s := intent.Status(status)
		opts.Status = &s
	}
	if typ != "" {
		t := intent.Type(typ)
		opts.Type = &t
	}

	// Get intents
	intents, err := svc.List(ctx, opts)
	if err != nil {
		return nil, camperrors.Wrap(err, "failed to list intents")
	}

	if len(intents) == 0 {
		return nil, fmt.Errorf("no intents found")
	}

	// Show fuzzy picker
	idx, err := fuzzyfinder.Find(
		intents,
		func(i int) string {
			intent := intents[i]
			// Format: [status] ID - Title (concept)
			concept := intent.Concept
			if concept == "" {
				concept = "-"
			}
			return fmt.Sprintf("[%s] %s - %s (%s)",
				intent.Status,
				truncateID(intent.ID),
				intent.Title,
				concept,
			)
		},
		fuzzyfinder.WithPromptString("Select intent: "),
		fuzzyfinder.WithPreviewWindow(func(i, w, h int) string {
			if i < 0 || i >= len(intents) {
				return ""
			}
			intent := intents[i]
			return formatIntentPreview(intent)
		}),
	)

	if err != nil {
		return nil, err
	}

	return intents[idx], nil
}

// truncateID shortens ID for picker display.
func truncateID(id string) string {
	// Show timestamp-slug portion, truncate long slugs
	if len(id) > 30 {
		return id[:30] + "..."
	}
	return id
}

// formatIntentPreview formats an intent for the preview window.
func formatIntentPreview(i *intent.Intent) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Title:    %s\n", i.Title))
	sb.WriteString(fmt.Sprintf("ID:       %s\n", i.ID))
	sb.WriteString(fmt.Sprintf("Status:   %s\n", i.Status))
	sb.WriteString(fmt.Sprintf("Type:     %s\n", i.Type))

	if i.Concept != "" {
		sb.WriteString(fmt.Sprintf("Concept:  %s\n", i.Concept))
	}
	if i.Priority != "" {
		sb.WriteString(fmt.Sprintf("Priority: %s\n", i.Priority))
	}
	if i.Horizon != "" {
		sb.WriteString(fmt.Sprintf("Horizon:  %s\n", i.Horizon))
	}

	sb.WriteString(fmt.Sprintf("\nCreated:  %s\n", i.CreatedAt.Format("2006-01-02 15:04")))
	if !i.UpdatedAt.IsZero() {
		sb.WriteString(fmt.Sprintf("Updated:  %s\n", i.UpdatedAt.Format("2006-01-02 15:04")))
	}

	if i.Path != "" {
		sb.WriteString(fmt.Sprintf("\nPath: %s\n", i.Path))
	}

	return sb.String()
}
