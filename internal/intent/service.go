package intent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sahilm/fuzzy"
)

// Service errors.
var (
	ErrNotFound    = errors.New("intent not found")
	ErrCancelled   = errors.New("intent creation cancelled")
	ErrFileExists  = errors.New("intent file already exists")
	ErrInvalidPath = errors.New("invalid path")
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
	Project   string
	Author    string
	Body      string    // Description/body content for the intent
	Timestamp time.Time // Optional; defaults to time.Now()
}

// CreateDirect creates a new intent directly without opening an editor.
// This is the "fast capture" mode for quick idea capture.
func (s *IntentService) CreateDirect(ctx context.Context, opts CreateOptions) (*Intent, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	ts := opts.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}

	// Generate ID and template data
	data := NewTemplateDataFromInput(opts.Title, string(opts.Type), opts.Project, opts.Author, opts.Body, ts)

	// Render template
	content, err := RenderTemplate(data)
	if err != nil {
		return nil, fmt.Errorf("rendering template: %w", err)
	}

	// Parse the rendered content to get an Intent struct
	intent, err := ParseIntent([]byte(content))
	if err != nil {
		return nil, fmt.Errorf("parsing rendered template: %w", err)
	}

	// Determine final path (inbox by default)
	finalPath := s.getIntentPath(StatusInbox, data.ID)

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(finalPath), 0755); err != nil {
		return nil, fmt.Errorf("creating directory: %w", err)
	}

	// Check if file already exists
	if _, err := os.Stat(finalPath); err == nil {
		return nil, fmt.Errorf("%w: %s", ErrFileExists, finalPath)
	}

	// Write intent file
	if err := os.WriteFile(finalPath, []byte(content), 0644); err != nil {
		return nil, fmt.Errorf("writing intent file: %w", err)
	}

	intent.Path = finalPath
	return intent, nil
}

// CreateWithEditor creates an intent using the editor workflow.
// It creates a temp file with a template, opens the editor, and saves the result.
// The editorFn callback is used to open the editor, allowing for testing.
func (s *IntentService) CreateWithEditor(ctx context.Context, opts CreateOptions, editorFn EditorFunc) (*Intent, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	ts := opts.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}

	// Generate template data
	data := NewTemplateDataFromInput(opts.Title, string(opts.Type), opts.Project, opts.Author, opts.Body, ts)

	// Render template
	content, err := RenderTemplate(data)
	if err != nil {
		return nil, fmt.Errorf("rendering template: %w", err)
	}

	// Create temp file
	tmpfile, err := os.CreateTemp("", "intent_*.md")
	if err != nil {
		return nil, fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpfile.Name()
	defer os.Remove(tmpPath) // Clean up temp file

	// Write template to temp file
	if _, err := tmpfile.WriteString(content); err != nil {
		tmpfile.Close()
		return nil, fmt.Errorf("writing temp file: %w", err)
	}
	tmpfile.Close()

	// Open editor (blocking)
	if editorFn != nil {
		if err := editorFn(ctx, tmpPath); err != nil {
			return nil, fmt.Errorf("opening editor: %w", err)
		}
	}

	// Read modified content
	modified, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("reading edited file: %w", err)
	}

	// Check for cancellation (empty file or unchanged)
	if isCancelled(content, string(modified)) {
		return nil, ErrCancelled
	}

	// Parse and validate
	intent, err := ParseIntent(modified)
	if err != nil {
		return nil, fmt.Errorf("parsing edited intent: %w", err)
	}

	if errs := intent.Validate(); len(errs) > 0 {
		return nil, fmt.Errorf("validation failed: %v", errs)
	}

	// Move to final location
	finalPath := s.getIntentPath(intent.Status, intent.ID)
	if err := os.MkdirAll(filepath.Dir(finalPath), 0755); err != nil {
		return nil, fmt.Errorf("creating directory: %w", err)
	}

	if err := moveFile(tmpPath, finalPath); err != nil {
		return nil, fmt.Errorf("moving intent file: %w", err)
	}

	intent.Path = finalPath
	return intent, nil
}

// EditorFunc is a callback for opening an editor on a file.
type EditorFunc func(ctx context.Context, path string) error

// Find locates an intent by ID across all status directories.
// Supports fuzzy matching - partial IDs will match.
func (s *IntentService) Find(ctx context.Context, id string) (*Intent, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	statuses := []Status{StatusInbox, StatusActive, StatusReady, StatusDone, StatusKilled}

	// First try exact match
	for _, status := range statuses {
		path := s.getIntentPath(status, id)
		if intent, err := s.loadIntent(path); err == nil {
			return intent, nil
		}
	}

	// Try fuzzy match (ID contains the search term)
	for _, status := range statuses {
		dir := filepath.Join(s.intentsDir, string(status))
		files, err := os.ReadDir(dir)
		if err != nil {
			continue // Directory may not exist
		}

		for _, file := range files {
			if !strings.HasSuffix(file.Name(), ".md") {
				continue
			}
			// Check if filename contains the search ID
			baseName := strings.TrimSuffix(file.Name(), ".md")
			if strings.Contains(baseName, id) {
				path := filepath.Join(dir, file.Name())
				if intent, err := s.loadIntent(path); err == nil {
					return intent, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
}

// Get retrieves an intent by its exact ID.
func (s *IntentService) Get(ctx context.Context, id string) (*Intent, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	statuses := []Status{StatusInbox, StatusActive, StatusReady, StatusDone, StatusKilled}

	for _, status := range statuses {
		path := s.getIntentPath(status, id)
		if intent, err := s.loadIntent(path); err == nil {
			return intent, nil
		}
	}

	return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
}

// ListOptions contains options for listing intents.
type ListOptions struct {
	Status   *Status // Filter by status (nil for all)
	Type     *Type   // Filter by type (nil for all)
	Project  string  // Filter by project (empty for all)
	SortBy   string  // Sort field: "created", "updated", "title", "priority"
	SortDesc bool    // Sort in descending order
}

// List returns all intents matching the given options.
func (s *IntentService) List(ctx context.Context, opts *ListOptions) ([]*Intent, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	var intents []*Intent
	statuses := []Status{StatusInbox, StatusActive, StatusReady, StatusDone, StatusKilled}

	if opts != nil && opts.Status != nil {
		statuses = []Status{*opts.Status}
	}

	for _, status := range statuses {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("context cancelled: %w", err)
		}

		dir := filepath.Join(s.intentsDir, string(status))
		files, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("reading directory %s: %w", dir, err)
		}

		for _, file := range files {
			if !strings.HasSuffix(file.Name(), ".md") {
				continue
			}
			path := filepath.Join(dir, file.Name())
			intent, err := s.loadIntent(path)
			if err != nil {
				// Log warning but continue
				continue
			}

			// Apply filters
			if opts != nil {
				if opts.Type != nil && intent.Type != *opts.Type {
					continue
				}
				if opts.Project != "" && intent.Project != opts.Project {
					continue
				}
			}

			intents = append(intents, intent)
		}
	}

	// Sort results
	if opts != nil && opts.SortBy != "" {
		s.sortIntents(intents, opts.SortBy, opts.SortDesc)
	} else {
		// Default sort by created date descending (newest first)
		s.sortIntents(intents, "created", true)
	}

	return intents, nil
}

// intentSource implements fuzzy.Source interface for intent searching.
type intentSource []*Intent

func (is intentSource) String(i int) string {
	return is[i].Title
}

func (is intentSource) Len() int {
	return len(is)
}

// Search returns intents matching the query string using fuzzy matching.
// Empty query returns all intents. Results are sorted by relevance score.
func (s *IntentService) Search(ctx context.Context, query string) ([]*Intent, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	// Get all intents
	allIntents, err := s.List(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("listing intents: %w", err)
	}

	// Empty query returns all intents
	if query == "" {
		return allIntents, nil
	}

	// Use fuzzy matching on titles
	matches := fuzzy.FindFrom(query, intentSource(allIntents))

	// Build results from matches (already sorted by score)
	results := make([]*Intent, len(matches))
	for i, match := range matches {
		results[i] = allIntents[match.Index]
	}

	return results, nil
}

// Edit opens an existing intent in an editor and saves changes.
func (s *IntentService) Edit(ctx context.Context, id string, editorFn EditorFunc) (*Intent, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
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
			return nil, fmt.Errorf("opening editor: %w", err)
		}
	}

	// Re-read and parse
	content, err := os.ReadFile(intent.Path)
	if err != nil {
		return nil, fmt.Errorf("reading edited file: %w", err)
	}

	updated, err := ParseIntent(content)
	if err != nil {
		return nil, fmt.Errorf("parsing edited intent: %w", err)
	}

	if errs := updated.Validate(); len(errs) > 0 {
		return nil, fmt.Errorf("validation failed: %v", errs)
	}

	// Handle status change (move to new directory)
	if updated.Status != originalStatus {
		newPath := s.getIntentPath(updated.Status, updated.ID)
		if err := os.MkdirAll(filepath.Dir(newPath), 0755); err != nil {
			return nil, fmt.Errorf("creating directory: %w", err)
		}
		if err := moveFile(originalPath, newPath); err != nil {
			return nil, fmt.Errorf("moving intent to new status: %w", err)
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
		return fmt.Errorf("context cancelled: %w", err)
	}

	if intent.Path == "" {
		return fmt.Errorf("%w: intent has no path", ErrInvalidPath)
	}

	data, err := SerializeIntent(intent)
	if err != nil {
		return fmt.Errorf("serializing intent: %w", err)
	}

	if err := os.WriteFile(intent.Path, data, 0644); err != nil {
		return fmt.Errorf("writing intent file: %w", err)
	}

	return nil
}

// Delete removes an intent file.
func (s *IntentService) Delete(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context cancelled: %w", err)
	}

	intent, err := s.Find(ctx, id)
	if err != nil {
		return err
	}

	if err := os.Remove(intent.Path); err != nil {
		return fmt.Errorf("removing intent file: %w", err)
	}

	return nil
}

// Move changes an intent's status by moving it to a different directory.
func (s *IntentService) Move(ctx context.Context, id string, newStatus Status) (*Intent, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
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
		return nil, fmt.Errorf("creating directory: %w", err)
	}

	// Serialize and write to new location
	data, err := SerializeIntent(intent)
	if err != nil {
		return nil, fmt.Errorf("serializing intent: %w", err)
	}

	if err := os.WriteFile(newPath, data, 0644); err != nil {
		return nil, fmt.Errorf("writing intent file: %w", err)
	}

	// Remove old file
	if err := os.Remove(oldPath); err != nil {
		// Try to clean up new file if remove fails
		os.Remove(newPath)
		return nil, fmt.Errorf("removing old intent file: %w", err)
	}

	intent.Path = newPath
	return intent, nil
}

// Archive moves an intent to the killed status.
func (s *IntentService) Archive(ctx context.Context, id string) (*Intent, error) {
	return s.Move(ctx, id, StatusKilled)
}

// Helper methods

// getIntentPath returns the file path for an intent given its status and ID.
func (s *IntentService) getIntentPath(status Status, id string) string {
	return filepath.Join(s.intentsDir, string(status), id+".md")
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

// sortIntents sorts a slice of intents by the given field.
func (s *IntentService) sortIntents(intents []*Intent, sortBy string, desc bool) {
	sort.Slice(intents, func(i, j int) bool {
		var less bool
		switch sortBy {
		case "created":
			less = intents[i].CreatedAt.Before(intents[j].CreatedAt)
		case "updated":
			less = intents[i].UpdatedAt.Before(intents[j].UpdatedAt)
		case "title":
			less = intents[i].Title < intents[j].Title
		case "priority":
			less = priorityRank(intents[i].Priority) < priorityRank(intents[j].Priority)
		default:
			less = intents[i].CreatedAt.Before(intents[j].CreatedAt)
		}
		if desc {
			return !less
		}
		return less
	})
}
