package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ktr0731/go-fuzzyfinder"
	"github.com/spf13/cobra"

	"github.com/obediencecorp/camp/internal/config"
	"github.com/obediencecorp/camp/internal/editor"
	"github.com/obediencecorp/camp/internal/intent"
)

var intentEditCmd = &cobra.Command{
	Use:   "edit [id]",
	Short: "Edit an existing intent",
	Long: `Edit an intent in your preferred editor.

If ID is provided, opens the intent directly (supports partial matching).
If no ID is provided, shows a fuzzy picker to select an intent.

PARTIAL ID MATCHING:
  Full ID:       20260119-153412-add-retry-logic
  Time suffix:   153412-add-retry
  Slug portion:  add-retry

Examples:
  camp intent edit                       Interactive picker
  camp intent edit 20260119-153412...    Direct edit by full ID
  camp intent edit retry-logic           Partial match edit
  camp intent edit --status active       Picker filtered by status`,
	Args: cobra.MaximumNArgs(1),
	RunE: runIntentEdit,
}

func init() {
	intentCmd.AddCommand(intentEditCmd)

	flags := intentEditCmd.Flags()
	flags.StringP("status", "s", "", "Filter picker by status")
	flags.StringP("type", "t", "", "Filter picker by type")
	flags.StringP("project", "p", "", "Filter picker by project")
}

func runIntentEdit(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Parse flags
	statusFilter, _ := cmd.Flags().GetString("status")
	typeFilter, _ := cmd.Flags().GetString("type")
	projectFilter, _ := cmd.Flags().GetString("project")

	// Find campaign root
	_, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return fmt.Errorf("not in a campaign directory: %w", err)
	}

	// Create service
	svc := intent.NewIntentService(campaignRoot)

	var selectedIntent *intent.Intent

	if len(args) > 0 {
		// Direct ID provided - resolve and edit
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

	// Edit the intent
	editorFn := func(ctx context.Context, path string) error {
		return editor.Edit(ctx, path)
	}

	updated, err := svc.Edit(ctx, selectedIntent.ID, editorFn)
	if err != nil {
		return fmt.Errorf("failed to edit intent: %w", err)
	}

	fmt.Printf("✓ Intent saved: %s\n", updated.Path)
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
	if errors.Is(err, intent.ErrNotFound) {
		return nil, fmt.Errorf("intent not found: %s", partialID)
	}

	return nil, fmt.Errorf("failed to find intent: %w", err)
}

// pickIntent shows a fuzzy picker for intent selection.
func pickIntent(ctx context.Context, svc *intent.IntentService, status, typ, project string) (*intent.Intent, error) {
	// Build list options
	opts := &intent.ListOptions{
		Project:  project,
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
		return nil, fmt.Errorf("failed to list intents: %w", err)
	}

	if len(intents) == 0 {
		return nil, fmt.Errorf("no intents found")
	}

	// Show fuzzy picker
	idx, err := fuzzyfinder.Find(
		intents,
		func(i int) string {
			intent := intents[i]
			// Format: [status] ID - Title (project)
			proj := intent.Project
			if proj == "" {
				proj = "-"
			}
			return fmt.Sprintf("[%s] %s - %s (%s)",
				intent.Status,
				truncateID(intent.ID),
				intent.Title,
				proj,
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

	if i.Project != "" {
		sb.WriteString(fmt.Sprintf("Project:  %s\n", i.Project))
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
