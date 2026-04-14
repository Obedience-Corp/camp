package intent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ktr0731/go-fuzzyfinder"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/intent"
)

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
