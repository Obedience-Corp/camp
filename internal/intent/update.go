package intent

import (
	"context"
	"os"
	"path/filepath"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// UpdateOptions contains pointer fields so callers can distinguish "not set"
// (nil) from "set to empty string" (pointer to ""). This is critical for
// programmatic edits where only some fields should change.
type UpdateOptions struct {
	Title   *string
	Body    *string   // Replaces the entire body/content section
	Append  *string   // Appended to existing body (mutually exclusive with Body)
	Type    *Type     // Set the intent type
	Status  *Status   // Set the intent status
	Concept *string   // Set the concept field
	Author  *string   // Set the author attribution

	Priority *Priority
	Horizon  *Horizon
}

// FieldChange records a single field's old and new value for audit purposes.
type FieldChange struct {
	Field string `json:"field"`
	Old   string `json:"old"`
	New   string `json:"new"`
}

// hasChanges returns true if any field in the options is set.
func (o *UpdateOptions) hasChanges() bool {
	return o.Title != nil || o.Body != nil || o.Append != nil ||
		o.Type != nil || o.Status != nil || o.Concept != nil ||
		o.Author != nil || o.Priority != nil || o.Horizon != nil
}

// UpdateDirect applies programmatic field updates to an existing intent
// without opening an editor. Returns the updated intent and a slice of
// field changes for audit logging.
func (s *IntentService) UpdateDirect(ctx context.Context, id string, opts UpdateOptions) (*Intent, []FieldChange, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, camperrors.Wrap(err, "context cancelled")
	}

	if !opts.hasChanges() {
		return nil, nil, camperrors.Wrap(camperrors.ErrInvalidInput, "no update fields specified")
	}

	intent, err := s.Find(ctx, id)
	if err != nil {
		return nil, nil, err
	}

	var changes []FieldChange
	originalPath := intent.Path
	originalStatus := intent.Status

	// Apply each field, recording changes
	if opts.Title != nil && *opts.Title != intent.Title {
		changes = append(changes, FieldChange{Field: "title", Old: intent.Title, New: *opts.Title})
		intent.Title = *opts.Title
	}

	if opts.Body != nil {
		old := intent.Content
		intent.Content = *opts.Body
		if old != *opts.Body {
			changes = append(changes, FieldChange{Field: "body", Old: truncateForAudit(old), New: truncateForAudit(*opts.Body)})
		}
	}

	if opts.Append != nil {
		old := intent.Content
		if intent.Content != "" && intent.Content[len(intent.Content)-1] != '\n' {
			intent.Content += "\n"
		}
		intent.Content += *opts.Append
		changes = append(changes, FieldChange{Field: "body", Old: truncateForAudit(old), New: truncateForAudit(intent.Content)})
	}

	if opts.Type != nil && *opts.Type != intent.Type {
		changes = append(changes, FieldChange{Field: "type", Old: string(intent.Type), New: string(*opts.Type)})
		intent.Type = *opts.Type
	}

	if opts.Status != nil && *opts.Status != intent.Status {
		changes = append(changes, FieldChange{Field: "status", Old: string(intent.Status), New: string(*opts.Status)})
		intent.Status = *opts.Status
	}

	if opts.Concept != nil && *opts.Concept != intent.Concept {
		changes = append(changes, FieldChange{Field: "concept", Old: intent.Concept, New: *opts.Concept})
		intent.Concept = *opts.Concept
	}

	if opts.Author != nil && *opts.Author != intent.Author {
		changes = append(changes, FieldChange{Field: "author", Old: intent.Author, New: *opts.Author})
		intent.Author = *opts.Author
	}

	if opts.Priority != nil && *opts.Priority != intent.Priority {
		changes = append(changes, FieldChange{Field: "priority", Old: string(intent.Priority), New: string(*opts.Priority)})
		intent.Priority = *opts.Priority
	}

	if opts.Horizon != nil && *opts.Horizon != intent.Horizon {
		changes = append(changes, FieldChange{Field: "horizon", Old: string(intent.Horizon), New: string(*opts.Horizon)})
		intent.Horizon = *opts.Horizon
	}

	if len(changes) == 0 {
		// All values were identical to existing — no-op
		return intent, nil, nil
	}

	// Validate after all changes applied
	if errs := intent.Validate(); len(errs) > 0 {
		return nil, nil, intentValidationError(errs)
	}

	intent.UpdatedAt = time.Now()

	// Handle status change (move to new directory)
	if intent.Status != originalStatus {
		newPath := s.getIntentPath(intent.Status, intent.ID)
		if err := os.MkdirAll(filepath.Dir(newPath), 0755); err != nil {
			return nil, nil, camperrors.Wrap(err, "creating directory for status change")
		}
		intent.Path = newPath
	}

	// Serialize and write
	if err := s.Save(ctx, intent); err != nil {
		return nil, nil, err
	}

	// Remove old file if status changed
	if intent.Status != originalStatus && originalPath != intent.Path {
		_ = os.Remove(originalPath)
		s.removeAllCopies(intent.ID, intent.Path)
	}

	return intent, changes, nil
}

// truncateForAudit truncates long strings for audit log readability.
func truncateForAudit(s string) string {
	const maxLen = 200
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
