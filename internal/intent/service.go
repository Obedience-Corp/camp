package intent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	intentaudit "github.com/Obedience-Corp/camp/internal/intent/audit"
	"github.com/sahilm/fuzzy"
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

// PlannedPathMove describes a filesystem move that migration would perform.
type PlannedPathMove struct {
	Source string
	Dest   string
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
		return nil, fmt.Errorf("validation failed: %v", errs)
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

// Find locates an intent by ID across all status directories.
// Supports fuzzy matching - partial IDs will match.
func (s *IntentService) Find(ctx context.Context, id string) (*Intent, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	statuses := AllStatuses()

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

	return nil, camperrors.Wrap(ErrNotFound, id)
}

// Get retrieves an intent by its exact ID.
func (s *IntentService) Get(ctx context.Context, id string) (*Intent, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	statuses := AllStatuses()

	for _, status := range statuses {
		path := s.getIntentPath(status, id)
		if intent, err := s.loadIntent(path); err == nil {
			return intent, nil
		}
	}

	return nil, camperrors.Wrap(ErrNotFound, id)
}

// ListOptions contains options for listing intents.
type ListOptions struct {
	Status   *Status // Filter by status (nil for all)
	Type     *Type   // Filter by type (nil for all)
	Concept  string  // Filter by concept (empty for all)
	SortBy   string  // Sort field: "created", "updated", "title", "priority"
	SortDesc bool    // Sort in descending order
}

// List returns all intents matching the given options.
func (s *IntentService) List(ctx context.Context, opts *ListOptions) ([]*Intent, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	var intents []*Intent
	statuses := AllStatuses()

	if opts != nil && opts.Status != nil {
		statuses = []Status{*opts.Status}
	}

	for _, status := range statuses {
		if err := ctx.Err(); err != nil {
			return nil, camperrors.Wrap(err, "context cancelled")
		}

		dir := filepath.Join(s.intentsDir, string(status))
		files, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, camperrors.Wrapf(err, "reading directory %s", dir)
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
				if opts.Concept != "" && intent.Concept != opts.Concept {
					continue
				}
			}

			intents = append(intents, intent)
		}
	}

	// Deduplicate by ID — an intent file may exist in multiple status
	// directories due to incomplete Move() operations. Keep the first
	// occurrence (statuses are scanned in priority order).
	seen := make(map[string]bool, len(intents))
	deduped := make([]*Intent, 0, len(intents))
	for _, i := range intents {
		if seen[i.ID] {
			continue
		}
		seen[i.ID] = true
		deduped = append(deduped, i)
	}
	intents = deduped

	// Sort results
	if opts != nil && opts.SortBy != "" {
		s.sortIntents(intents, opts.SortBy, opts.SortDesc)
	} else {
		// Default sort by last-touched date descending (newest first)
		s.sortIntents(intents, "updated", true)
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
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	// Get all intents
	allIntents, err := s.List(ctx, nil)
	if err != nil {
		return nil, camperrors.Wrap(err, "listing intents")
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
		return nil, fmt.Errorf("validation failed: %v", errs)
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

// sortIntents sorts a slice of intents by the given field.
func (s *IntentService) sortIntents(intents []*Intent, sortBy string, desc bool) {
	sort.Slice(intents, func(i, j int) bool {
		var less bool
		switch sortBy {
		case "created":
			less = intents[i].CreatedAt.Before(intents[j].CreatedAt)
		case "updated":
			ui := intents[i].UpdatedAt
			if ui.IsZero() {
				ui = intents[i].CreatedAt
			}
			uj := intents[j].UpdatedAt
			if uj.IsZero() {
				uj = intents[j].CreatedAt
			}
			less = ui.Before(uj)
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

// EnsureDirectories creates all status directories if missing and migrates
// legacy top-level done/ and killed/ directories into the dungeon.
func (s *IntentService) EnsureDirectories(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return camperrors.Wrap(err, "context cancelled")
	}

	if err := s.ensureCanonicalIntentRoot(ctx); err != nil {
		return camperrors.Wrap(err, "ensuring canonical intent root")
	}

	// Create all status directories
	for _, status := range AllStatuses() {
		dir := filepath.Join(s.intentsDir, string(status))
		if err := os.MkdirAll(dir, 0755); err != nil {
			return camperrors.Wrapf(err, "creating directory %s", dir)
		}
	}

	// Migrate legacy top-level done/ and killed/ into dungeon
	legacyMappings := map[string]Status{
		"done":   StatusDone,
		"killed": StatusKilled,
	}

	for legacyDir, newStatus := range legacyMappings {
		if err := s.migrateLegacyDir(ctx, legacyDir, newStatus); err != nil {
			return camperrors.Wrapf(err, "migrating %s", legacyDir)
		}
	}

	return nil
}

func (s *IntentService) ensureCanonicalIntentRoot(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return camperrors.Wrap(err, "context cancelled")
	}

	legacyRoot := s.legacyIntentsDir()
	if filepath.Clean(legacyRoot) == filepath.Clean(s.intentsDir) {
		return nil
	}

	canonicalHasState, err := hasIntentState(s.intentsDir)
	if err != nil {
		return camperrors.Wrapf(err, "inspecting canonical intent root %s", s.intentsDir)
	}

	legacyHasState, err := hasIntentState(legacyRoot)
	if err != nil {
		return camperrors.Wrapf(err, "inspecting legacy intent root %s", legacyRoot)
	}

	if legacyHasState && canonicalHasState {
		return camperrors.Wrapf(
			ErrIntentMigrationConflict,
			"both %s and %s contain intent state; resolve with repair before retrying",
			legacyRoot,
			s.intentsDir,
		)
	}

	if !legacyHasState {
		return nil
	}

	if err := s.migrateLegacyIntentRoot(legacyRoot); err != nil {
		return camperrors.Wrapf(err, "migrating legacy intent root %s", legacyRoot)
	}

	return nil
}

func (s *IntentService) legacyIntentsDir() string {
	return filepath.Join(s.campaignRoot, "workflow", "intents")
}

// PlanLegacyIntentRootMigration returns the filesystem moves required to migrate
// legacy workflow/intents state into the canonical intent root.
func (s *IntentService) PlanLegacyIntentRootMigration() ([]PlannedPathMove, error) {
	legacyRoot := s.legacyIntentsDir()
	if filepath.Clean(legacyRoot) == filepath.Clean(s.intentsDir) {
		return nil, nil
	}

	canonicalHasState, err := hasIntentState(s.intentsDir)
	if err != nil {
		return nil, camperrors.Wrapf(err, "inspecting canonical intent root %s", s.intentsDir)
	}

	legacyHasState, err := hasIntentState(legacyRoot)
	if err != nil {
		return nil, camperrors.Wrapf(err, "inspecting legacy intent root %s", legacyRoot)
	}

	if legacyHasState && canonicalHasState {
		return nil, camperrors.Wrapf(
			ErrIntentMigrationConflict,
			"both %s and %s contain intent state; resolve with repair before retrying",
			legacyRoot,
			s.intentsDir,
		)
	}

	if !legacyHasState {
		return nil, nil
	}

	mappings := [][2]string{
		{filepath.Join(legacyRoot, string(StatusInbox)), filepath.Join(s.intentsDir, string(StatusInbox))},
		{filepath.Join(legacyRoot, string(StatusReady)), filepath.Join(s.intentsDir, string(StatusReady))},
		{filepath.Join(legacyRoot, string(StatusActive)), filepath.Join(s.intentsDir, string(StatusActive))},
		{filepath.Join(legacyRoot, "dungeon"), filepath.Join(s.intentsDir, "dungeon")},
		{filepath.Join(legacyRoot, "done"), filepath.Join(s.intentsDir, "dungeon", string(StatusDone))},
		{filepath.Join(legacyRoot, "killed"), filepath.Join(s.intentsDir, "dungeon", string(StatusKilled))},
	}

	var moves []PlannedPathMove
	for _, mapping := range mappings {
		if err := collectIntentTreeMoves(mapping[0], mapping[1], &moves); err != nil {
			return nil, err
		}
	}

	if err := collectIntentAuditMove(legacyRoot, s.intentsDir, &moves); err != nil {
		return nil, err
	}

	return moves, nil
}

func hasIntentState(root string) (bool, error) {
	if info, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, camperrors.Wrapf(err, "stat %s", root)
	} else if !info.IsDir() {
		return false, camperrors.Wrapf(ErrInvalidPath, "%s is not a directory", root)
	}

	hasAudit, err := hasNonEmptyFile(intentaudit.FilePath(root))
	if err != nil {
		return false, err
	}
	if hasAudit {
		return true, nil
	}

	stateDirs := []string{
		string(StatusInbox),
		string(StatusReady),
		string(StatusActive),
		string(StatusDone),
		string(StatusKilled),
		string(StatusArchived),
		string(StatusSomeday),
		"done",
		"killed",
	}

	for _, relDir := range stateDirs {
		hasMarkdown, err := hasMarkdownFiles(filepath.Join(root, relDir))
		if err != nil {
			return false, err
		}
		if hasMarkdown {
			return true, nil
		}
	}

	return false, nil
}

func hasNonEmptyFile(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, camperrors.Wrapf(err, "stat %s", path)
	}
	if info.IsDir() {
		return false, camperrors.Wrapf(ErrInvalidPath, "%s is not a file", path)
	}
	return info.Size() > 0, nil
}

func hasMarkdownFiles(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, camperrors.Wrapf(err, "reading directory %s", dir)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(entry.Name(), ".md") {
			return true, nil
		}
	}

	return false, nil
}

func (s *IntentService) migrateLegacyIntentRoot(legacyRoot string) error {
	for _, relDir := range []string{
		string(StatusInbox),
		string(StatusReady),
		string(StatusActive),
		"dungeon",
		"done",
		"killed",
	} {
		if err := moveIntentTree(filepath.Join(legacyRoot, relDir), filepath.Join(s.intentsDir, relDir)); err != nil {
			return err
		}
	}

	if err := moveIntentAuditFile(legacyRoot, s.intentsDir); err != nil {
		return err
	}

	return nil
}

func moveIntentAuditFile(legacyRoot, canonicalRoot string) error {
	srcPath := intentaudit.FilePath(legacyRoot)
	if _, err := os.Stat(srcPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return camperrors.Wrapf(err, "stat %s", srcPath)
	}

	dstPath := intentaudit.FilePath(canonicalRoot)
	if info, err := os.Stat(dstPath); err == nil {
		if info.Size() > 0 {
			return camperrors.Wrapf(ErrIntentMigrationConflict, "audit log already exists at %s", dstPath)
		}
		if err := os.Remove(dstPath); err != nil {
			return camperrors.Wrapf(err, "removing empty audit log %s", dstPath)
		}
	} else if !os.IsNotExist(err) {
		return camperrors.Wrapf(err, "stat %s", dstPath)
	}

	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return camperrors.Wrapf(err, "creating directory %s", filepath.Dir(dstPath))
	}

	if err := os.Rename(srcPath, dstPath); err != nil {
		return camperrors.Wrapf(err, "moving audit log %s", srcPath)
	}

	return nil
}

func collectIntentAuditMove(legacyRoot, canonicalRoot string, moves *[]PlannedPathMove) error {
	srcPath := intentaudit.FilePath(legacyRoot)
	if _, err := os.Stat(srcPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return camperrors.Wrapf(err, "stat %s", srcPath)
	}

	dstPath := intentaudit.FilePath(canonicalRoot)
	if info, err := os.Stat(dstPath); err == nil {
		if info.Size() > 0 {
			return camperrors.Wrapf(ErrIntentMigrationConflict, "audit log already exists at %s", dstPath)
		}
	} else if !os.IsNotExist(err) {
		return camperrors.Wrapf(err, "stat %s", dstPath)
	}

	*moves = append(*moves, PlannedPathMove{Source: srcPath, Dest: dstPath})
	return nil
}

func collectIntentTreeMoves(srcDir, dstDir string, moves *[]PlannedPathMove) error {
	if _, err := os.Stat(srcDir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return camperrors.Wrapf(err, "stat %s", srcDir)
	}

	if _, err := os.Stat(dstDir); os.IsNotExist(err) {
		*moves = append(*moves, PlannedPathMove{Source: srcDir, Dest: dstDir})
		return nil
	} else if err != nil {
		return camperrors.Wrapf(err, "stat %s", dstDir)
	}

	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return camperrors.Wrapf(err, "reading directory %s", srcDir)
	}

	for _, entry := range entries {
		srcPath := filepath.Join(srcDir, entry.Name())
		dstPath := filepath.Join(dstDir, entry.Name())

		if entry.IsDir() {
			if err := collectIntentTreeMoves(srcPath, dstPath, moves); err != nil {
				return err
			}
			continue
		}

		if _, err := os.Stat(dstPath); err == nil {
			if entry.Name() == ".gitkeep" {
				continue
			}
			return camperrors.Wrapf(ErrIntentMigrationConflict, "destination already exists for %s", dstPath)
		} else if !os.IsNotExist(err) {
			return camperrors.Wrapf(err, "stat %s", dstPath)
		}

		*moves = append(*moves, PlannedPathMove{Source: srcPath, Dest: dstPath})
	}

	return nil
}

func moveIntentTree(srcDir, dstDir string) error {
	if _, err := os.Stat(srcDir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return camperrors.Wrapf(err, "stat %s", srcDir)
	}

	if _, err := os.Stat(dstDir); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(dstDir), 0755); err != nil {
			return camperrors.Wrapf(err, "creating directory %s", filepath.Dir(dstDir))
		}
		if err := os.Rename(srcDir, dstDir); err != nil {
			return camperrors.Wrapf(err, "moving %s", srcDir)
		}
		return nil
	} else if err != nil {
		return camperrors.Wrapf(err, "stat %s", dstDir)
	}

	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return camperrors.Wrapf(err, "reading directory %s", srcDir)
	}

	for _, entry := range entries {
		srcPath := filepath.Join(srcDir, entry.Name())
		dstPath := filepath.Join(dstDir, entry.Name())

		if entry.IsDir() {
			if err := moveIntentTree(srcPath, dstPath); err != nil {
				return err
			}
			continue
		}

		if _, err := os.Stat(dstPath); err == nil {
			if entry.Name() == ".gitkeep" {
				if err := os.Remove(srcPath); err != nil {
					return camperrors.Wrapf(err, "removing %s", srcPath)
				}
				continue
			}
			return camperrors.Wrapf(ErrIntentMigrationConflict, "destination already exists for %s", dstPath)
		} else if !os.IsNotExist(err) {
			return camperrors.Wrapf(err, "stat %s", dstPath)
		}

		if err := os.Rename(srcPath, dstPath); err != nil {
			return camperrors.Wrapf(err, "moving %s", srcPath)
		}
	}

	if remaining, err := os.ReadDir(srcDir); err == nil && len(remaining) == 0 {
		_ = os.Remove(srcDir)
	}

	return nil
}

// migrateLegacyDir moves intent files from a legacy top-level status directory
// into the corresponding dungeon subdirectory, updating frontmatter status.
func (s *IntentService) migrateLegacyDir(ctx context.Context, legacyDir string, newStatus Status) error {
	srcDir := filepath.Join(s.intentsDir, legacyDir)

	// Check if legacy directory exists
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Nothing to migrate
		}
		return camperrors.Wrapf(err, "reading directory %s", srcDir)
	}

	dstDir := filepath.Join(s.intentsDir, string(newStatus))

	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return camperrors.Wrap(err, "context cancelled")
		}

		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		srcPath := filepath.Join(srcDir, entry.Name())
		dstPath := filepath.Join(dstDir, entry.Name())

		// Read, update status in frontmatter, write to new location
		content, err := os.ReadFile(srcPath)
		if err != nil {
			return camperrors.Wrapf(err, "reading %s", srcPath)
		}

		intent, err := ParseIntentFromFile(srcPath, content)
		if err != nil {
			// Can't parse — just move the file as-is
			if _, serr := os.Stat(dstPath); serr == nil {
				// Already migrated (prior interrupted run) — remove source and skip
				os.Remove(srcPath)
				continue
			}
			if err := os.Rename(srcPath, dstPath); err != nil {
				return camperrors.Wrapf(err, "moving %s", srcPath)
			}
			continue
		}

		// Update status and write to new location
		intent.Status = newStatus
		data, err := SerializeIntent(intent)
		if err != nil {
			return camperrors.Wrapf(err, "serializing %s", srcPath)
		}

		if _, serr := os.Stat(dstPath); serr == nil {
			// Already migrated (prior interrupted run) — remove source and skip
			os.Remove(srcPath)
			continue
		}

		if err := os.WriteFile(dstPath, data, 0644); err != nil {
			return camperrors.Wrapf(err, "writing %s", dstPath)
		}

		if err := os.Remove(srcPath); err != nil {
			return camperrors.Wrapf(err, "removing %s", srcPath)
		}
	}

	// Remove legacy directory if empty
	remaining, err := os.ReadDir(srcDir)
	if err == nil && len(remaining) == 0 {
		os.Remove(srcDir)
	}

	return nil
}
