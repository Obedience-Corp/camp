package intent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// Service errors.
// Sentinels marked with %w wrap the canonical sentinel from internal/errors
// to enable cross-package errors.Is() matching.
var (
	ErrNotFound                = camperrors.Wrap(camperrors.ErrNotFound, "intent not found")
	ErrCancelled               = camperrors.Wrap(camperrors.ErrCancelled, "intent creation cancelled")
	ErrFileExists              = camperrors.Wrap(camperrors.ErrAlreadyExists, "intent file already exists")
	ErrInvalidPath             = camperrors.Wrap(camperrors.ErrInvalidInput, "invalid path")
	ErrIntentMigrationConflict = camperrors.Wrap(camperrors.ErrConflict, "intent migration conflict")
)

// IntentService provides operations for managing intent files.
type IntentService struct {
	campaignRoot string
	intentsDir   string
}

// NewIntentService creates a new IntentService.
// intentsDir is the full path to the intents directory (e.g., from PathResolver.Intents()).
func NewIntentService(campaignRoot, intentsDir string) *IntentService {
	return &IntentService{
		campaignRoot: campaignRoot,
		intentsDir:   intentsDir,
	}
}

// CreateOptions contains options for creating a new intent.
type CreateOptions struct {
	Title     string
	Type      Type
	Concept   string // Full concept path (e.g., "projects/camp")
	Author    string
	Body      string    // Description/body content for the intent
	Timestamp time.Time // Optional; defaults to time.Now()
}

// CreateDirect creates a new intent directly without opening an editor.
// This is the "fast capture" mode for quick idea capture.
func (s *IntentService) CreateDirect(ctx context.Context, opts CreateOptions) (*Intent, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	ts := opts.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}

	// Generate ID and template data
	data := NewTemplateDataFromInput(opts.Title, string(opts.Type), opts.Concept, opts.Author, opts.Body, ts)

	// Render template
	content, err := RenderTemplate(data)
	if err != nil {
		return nil, camperrors.Wrap(err, "rendering template")
	}

	// Parse the rendered content to get an Intent struct
	intent, err := ParseIntent([]byte(content))
	if err != nil {
		return nil, camperrors.Wrap(err, "parsing rendered template")
	}

	// Determine final path (inbox by default)
	finalPath := s.getIntentPath(StatusInbox, data.ID)

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(finalPath), 0755); err != nil {
		return nil, camperrors.Wrap(err, "creating directory")
	}

	// Check if file already exists
	if _, err := os.Stat(finalPath); err == nil {
		return nil, camperrors.Wrap(ErrFileExists, finalPath)
	}

	// Write intent file
	if err := os.WriteFile(finalPath, []byte(content), 0644); err != nil {
		return nil, camperrors.Wrap(err, "writing intent file")
	}

	intent.Path = finalPath
	return intent, nil
}

// CreateWithEditor creates an intent using the editor workflow.
// It creates a temp file with a template, opens the editor, and saves the result.
// The editorFn callback is used to open the editor, allowing for testing.
func (s *IntentService) CreateWithEditor(ctx context.Context, opts CreateOptions, editorFn EditorFunc) (*Intent, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	ts := opts.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}

	// Generate template data
	data := NewTemplateDataFromInput(opts.Title, string(opts.Type), opts.Concept, opts.Author, opts.Body, ts)

	// Render template
	content, err := RenderTemplate(data)
	if err != nil {
		return nil, camperrors.Wrap(err, "rendering template")
	}

	// Create temp file
	tmpfile, err := os.CreateTemp("", "intent_*.md")
	if err != nil {
		return nil, camperrors.Wrap(err, "creating temp file")
	}
	tmpPath := tmpfile.Name()
	defer func() {
		_ = os.Remove(tmpPath) // Clean up temp file
	}()

	// Write template to temp file
	if _, err := tmpfile.WriteString(content); err != nil {
		_ = tmpfile.Close()
		return nil, camperrors.Wrap(err, "writing temp file")
	}
	if err := tmpfile.Close(); err != nil {
		return nil, camperrors.Wrap(err, "closing temp file")
	}

	// Open editor (blocking)
	if editorFn != nil {
		if err := editorFn(ctx, tmpPath); err != nil {
			return nil, camperrors.Wrap(err, "opening editor")
		}
	}

	// Read modified content
	modified, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, camperrors.Wrap(err, "reading edited file")
	}

	// Check for cancellation (empty file or unchanged)
	if isCancelled(content, string(modified)) {
		return nil, ErrCancelled
	}

	// Parse and validate
	intent, err := ParseIntent(modified)
	if err != nil {
		return nil, camperrors.Wrap(err, "parsing edited intent")
	}

	if errs := intent.Validate(); len(errs) > 0 {
		return nil, intentValidationError(errs)
	}

	// Move to final location
	finalPath := s.getIntentPath(intent.Status, intent.ID)
	if err := os.MkdirAll(filepath.Dir(finalPath), 0755); err != nil {
		return nil, camperrors.Wrap(err, "creating directory")
	}

	if err := moveFile(tmpPath, finalPath); err != nil {
		return nil, camperrors.Wrap(err, "moving intent file")
	}

	intent.Path = finalPath
	return intent, nil
}

// EditorFunc is a callback for opening an editor on a file.
type EditorFunc func(ctx context.Context, path string) error

// Edit opens an existing intent in an editor and saves changes.
func (s *IntentService) Edit(ctx context.Context, id string, editorFn EditorFunc) (*Intent, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	intent, err := s.Find(ctx, id)
	if err != nil {
		return nil, err
	}

	originalPath := intent.Path
	originalStatus := intent.Status

	// Open editor
	if editorFn != nil {
		if err := editorFn(ctx, intent.Path); err != nil {
			return nil, camperrors.Wrap(err, "opening editor")
		}
	}

	// Re-read and parse
	content, err := os.ReadFile(intent.Path)
	if err != nil {
		return nil, camperrors.Wrap(err, "reading edited file")
	}

	updated, err := ParseIntent(content)
	if err != nil {
		return nil, camperrors.Wrap(err, "parsing edited intent")
	}

	if errs := updated.Validate(); len(errs) > 0 {
		return nil, intentValidationError(errs)
	}

	// Handle status change (move to new directory)
	if updated.Status != originalStatus {
		newPath := s.getIntentPath(updated.Status, updated.ID)
		if err := os.MkdirAll(filepath.Dir(newPath), 0755); err != nil {
			return nil, camperrors.Wrap(err, "creating directory")
		}
		if err := moveFile(originalPath, newPath); err != nil {
			return nil, camperrors.Wrap(err, "moving intent to new status")
		}
		updated.Path = newPath
	} else {
		updated.Path = originalPath
	}

	// Update timestamp
	updated.UpdatedAt = time.Now()
	if err := s.Save(ctx, updated); err != nil {
		return nil, err
	}

	return updated, nil
}

// Save writes an intent to its file path.
func (s *IntentService) Save(ctx context.Context, intent *Intent) error {
	if err := ctx.Err(); err != nil {
		return camperrors.Wrap(err, "context cancelled")
	}

	if intent.Path == "" {
		return camperrors.Wrap(ErrInvalidPath, "intent has no path")
	}

	data, err := SerializeIntent(intent)
	if err != nil {
		return camperrors.Wrap(err, "serializing intent")
	}

	if err := os.WriteFile(intent.Path, data, 0644); err != nil {
		return camperrors.Wrap(err, "writing intent file")
	}

	return nil
}

// Delete removes an intent file.
func (s *IntentService) Delete(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return camperrors.Wrap(err, "context cancelled")
	}

	intent, err := s.Find(ctx, id)
	if err != nil {
		return err
	}

	if err := os.Remove(intent.Path); err != nil {
		return camperrors.Wrap(err, "removing intent file")
	}

	return nil
}

// Move changes an intent's status by moving it to a different directory.
func (s *IntentService) Move(ctx context.Context, id string, newStatus Status) (*Intent, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	intent, err := s.Find(ctx, id)
	if err != nil {
		return nil, err
	}

	if intent.Status == newStatus {
		return intent, nil // Already in target status
	}

	// Update intent
	oldPath := intent.Path
	intent.Status = newStatus
	intent.UpdatedAt = time.Now()

	// Determine new path
	newPath := s.getIntentPath(newStatus, intent.ID)

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(newPath), 0755); err != nil {
		return nil, camperrors.Wrap(err, "creating directory")
	}

	// Serialize and write to new location
	data, err := SerializeIntent(intent)
	if err != nil {
		return nil, camperrors.Wrap(err, "serializing intent")
	}

	if err := os.WriteFile(newPath, data, 0644); err != nil {
		return nil, camperrors.Wrap(err, "writing intent file")
	}

	// Remove old file
	if err := os.Remove(oldPath); err != nil {
		// Try to clean up new file if remove fails
		os.Remove(newPath)
		return nil, camperrors.Wrap(err, "removing old intent file")
	}
	// Clean up any orphan copies in other status directories
	s.removeAllCopies(id, newPath)

	intent.Path = newPath
	return intent, nil
}

// Archive moves an intent to the archived dungeon status.
func (s *IntentService) Archive(ctx context.Context, id string) (*Intent, error) {
	return s.Move(ctx, id, StatusArchived)
}

// StatusCount holds the count of intents for a single status directory.
type StatusCount struct {
	Status Status
	Count  int
}

// Count returns the number of intent files in each status directory.
// This is lightweight — it counts files without parsing them.
func (s *IntentService) Count(ctx context.Context) ([]StatusCount, int, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, camperrors.Wrap(err, "context cancelled")
	}

	statuses := AllStatuses()
	counts := make([]StatusCount, 0, len(statuses))
	total := 0

	for _, status := range statuses {
		dir := filepath.Join(s.intentsDir, string(status))
		files, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				counts = append(counts, StatusCount{Status: status, Count: 0})
				continue
			}
			return nil, 0, camperrors.Wrapf(err, "reading directory %s", dir)
		}

		n := 0
		for _, f := range files {
			if !f.IsDir() && strings.HasSuffix(f.Name(), ".md") {
				n++
			}
		}
		counts = append(counts, StatusCount{Status: status, Count: n})
		total += n
	}

	return counts, total, nil
}

// Helper methods

// getIntentPath returns the file path for an intent given its status and ID.
func (s *IntentService) getIntentPath(status Status, id string) string {
	return filepath.Join(s.intentsDir, string(status), id+".md")
}

// removeAllCopies removes all files for the given intent ID across all
// status directories except the one at exceptPath. Used by Move() to
// clean up orphan copies left by incomplete prior moves.
func (s *IntentService) removeAllCopies(id string, exceptPath string) {
	for _, status := range AllStatuses() {
		p := s.getIntentPath(status, id)
		if p == exceptPath {
			continue
		}
		os.Remove(p) // ignore errors — file may not exist
	}
}

// loadIntent reads and parses an intent from a file path.
func (s *IntentService) loadIntent(path string) (*Intent, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	intent, err := ParseIntentFromFile(path, content)
	if err != nil {
		return nil, err
	}

	return intent, nil
}

func intentValidationError(errs []error) error {
	return camperrors.NewValidation("intent", "one or more fields failed validation", camperrors.Join(errs...))
}
