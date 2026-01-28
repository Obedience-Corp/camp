package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Service provides all workflow operations.
// It implements WorkflowInitializer, WorkflowReader, WorkflowWriter, and Crawler interfaces.
type Service struct {
	root       string  // Workflow root path
	schemaPath string  // Path to .workflow.yaml
	schema     *Schema // Loaded schema (nil if not loaded)
}

// ServiceOption configures a Service.
type ServiceOption func(*Service)

// NewService creates a new workflow Service for the given root directory.
// The service will look for a .workflow.yaml file in the root directory.
func NewService(root string, opts ...ServiceOption) *Service {
	s := &Service{
		root:       root,
		schemaPath: filepath.Join(root, SchemaFileName),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// WithSchema sets a pre-loaded schema on the service.
// Useful for testing or when the schema has already been loaded.
func WithSchema(schema *Schema) ServiceOption {
	return func(s *Service) {
		s.schema = schema
	}
}

// Root returns the workflow root directory path.
func (s *Service) Root() string {
	return s.root
}

// Schema returns the loaded workflow schema, or nil if not loaded.
func (s *Service) Schema() *Schema {
	return s.schema
}

// HasSchema returns true if a workflow schema exists at the root path.
func (s *Service) HasSchema() bool {
	if s.schema != nil {
		return true
	}
	// Check if schema file exists
	_, err := LoadSchema(context.Background(), s.schemaPath)
	return err == nil
}

// LoadSchema loads the workflow schema from disk.
// Returns an error if the schema file doesn't exist or is invalid.
func (s *Service) LoadSchema(ctx context.Context) error {
	schema, err := LoadSchema(ctx, s.schemaPath)
	if err != nil {
		return err
	}
	s.schema = schema
	return nil
}

// InitOptions configures workflow initialization.
type InitOptions struct {
	Force bool // Overwrite existing files
}

// InitResult contains what was created during init.
type InitResult struct {
	CreatedDirs  []string
	CreatedFiles []string
	Skipped      []string
}

// SyncOptions configures sync behavior.
type SyncOptions struct {
	DryRun bool // Preview without making changes
}

// SyncResult contains what was synced.
type SyncResult struct {
	Created  []string // Directories created
	Existing []string // Directories that already exist
}

// MigrateOptions configures migration behavior.
type MigrateOptions struct {
	DryRun bool // Preview without making changes
	Force  bool // Skip confirmation
}

// MigrateResult contains migration outcomes.
type MigrateResult struct {
	Created   []string // New directories/files
	Preserved []string // Existing items kept
	Schema    *Schema  // Generated schema
}

// StatusResult contains workflow statistics.
type StatusResult struct {
	Name           string         // Workflow name
	Location       string         // Root path
	Counts         map[string]int // Items per status
	TotalItems     int
	LastTransition *HistoryEntry // Most recent transition
}

// ShowOptions configures show output.
type ShowOptions struct {
	Tree   bool // Display as tree
	Schema bool // Show raw schema
}

// ShowResult contains workflow structure.
type ShowResult struct {
	Name        string
	Description string
	Directories []DirectoryInfo
	SchemaRaw   string // Raw YAML if requested
}

// DirectoryInfo describes a directory for display.
type DirectoryInfo struct {
	Path        string
	Description string
	Order       int
	ItemCount   int
	Children    []DirectoryInfo
}

// ListOptions configures list output.
type ListOptions struct {
	All  bool // List all statuses
	JSON bool // Output as JSON
}

// ListResult contains items in a status.
type ListResult struct {
	Status string
	Items  []Item
}

// HistoryOptions configures history queries.
type HistoryOptions struct {
	Limit int    // Max entries (0 = all)
	Item  string // Filter by item name
	JSON  bool   // Output as JSON
}

// MoveOptions configures move behavior.
type MoveOptions struct {
	Reason string // Reason for move (logged to history)
	Force  bool   // Skip transition validation
}

// MoveResult contains move outcomes.
type MoveResult struct {
	Item   string
	From   string
	To     string
	Reason string
}

// CrawlOptions configures crawl behavior.
type CrawlOptions struct {
	Status string // Specific status to crawl (empty = all)
	All    bool   // Include recently modified items
}

// CrawlResult contains crawl session summary.
type CrawlResult struct {
	Reviewed    int
	Kept        int
	Moved       int
	Skipped     int
	Transitions map[string]int // "from → to": count
}

// resolvePath converts a status path to an absolute filesystem path.
func (s *Service) resolvePath(status string) string {
	return filepath.Join(s.root, status)
}

// Init creates a new workflow with the default structure.
// It creates the schema file and all directories defined in the schema.
// Returns ErrSchemaExists if a schema already exists and Force is false.
func (s *Service) Init(ctx context.Context, opts InitOptions) (*InitResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	result := &InitResult{
		CreatedDirs:  []string{},
		CreatedFiles: []string{},
		Skipped:      []string{},
	}

	// Check if schema already exists
	if _, err := os.Stat(s.schemaPath); err == nil {
		if !opts.Force {
			return nil, ErrSchemaExists
		}
		result.Skipped = append(result.Skipped, s.schemaPath)
	}

	// Get default schema
	schema := DefaultSchema()

	// Write schema file
	data, err := yaml.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal schema: %w", err)
	}

	if err := os.WriteFile(s.schemaPath, data, 0644); err != nil {
		return nil, fmt.Errorf("failed to write schema file: %w", err)
	}
	result.CreatedFiles = append(result.CreatedFiles, s.schemaPath)

	// Store schema
	s.schema = schema

	// Create directories
	for _, dirPath := range schema.AllDirectories() {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		fullPath := s.resolvePath(dirPath)
		if err := os.MkdirAll(fullPath, 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory %s: %w", dirPath, err)
		}
		result.CreatedDirs = append(result.CreatedDirs, dirPath)
	}

	return result, nil
}

// Sync ensures all directories defined in the schema exist.
// It creates any missing directories but does not remove extra directories.
// If DryRun is true, it reports what would be created without making changes.
func (s *Service) Sync(ctx context.Context, opts SyncOptions) (*SyncResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Load schema if not already loaded
	if s.schema == nil {
		if err := s.LoadSchema(ctx); err != nil {
			return nil, err
		}
	}

	result := &SyncResult{}

	// Check each directory
	for _, dirPath := range s.schema.AllDirectories() {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		fullPath := s.resolvePath(dirPath)
		info, err := os.Stat(fullPath)
		if err == nil && info.IsDir() {
			result.Existing = append(result.Existing, dirPath)
			continue
		}

		// Directory doesn't exist
		if !opts.DryRun {
			if err := os.MkdirAll(fullPath, 0755); err != nil {
				return nil, fmt.Errorf("failed to create directory %s: %w", dirPath, err)
			}
		}
		result.Created = append(result.Created, dirPath)
	}

	return result, nil
}

// List returns items in a status directory.
// The status can be a top-level directory (e.g., "active") or
// a nested path (e.g., "dungeon/completed").
func (s *Service) List(ctx context.Context, status string, opts ListOptions) (*ListResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Load schema if not already loaded
	if s.schema == nil {
		if err := s.LoadSchema(ctx); err != nil {
			return nil, err
		}
	}

	// Validate status exists in schema
	if !s.schema.HasDirectory(status) {
		return nil, fmt.Errorf("%w: %s", ErrInvalidStatus, status)
	}

	statusPath := s.resolvePath(status)
	entries, err := os.ReadDir(statusPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrStatusNotFound, status)
		}
		return nil, fmt.Errorf("failed to read directory %s: %w", status, err)
	}

	result := &ListResult{
		Status: status,
		Items:  make([]Item, 0, len(entries)),
	}

	for _, entry := range entries {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		info, err := entry.Info()
		if err != nil {
			continue // Skip entries we can't stat
		}

		item := Item{
			Name:    entry.Name(),
			Path:    filepath.Join(statusPath, entry.Name()),
			IsDir:   entry.IsDir(),
			ModTime: info.ModTime(),
			Size:    info.Size(),
		}

		result.Items = append(result.Items, item)
	}

	return result, nil
}

// Move moves an item from its current status to a new status.
// It validates the transition against schema rules unless Force is set.
// If the workflow has history tracking enabled, the move is logged.
func (s *Service) Move(ctx context.Context, item, to string, opts MoveOptions) (*MoveResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Load schema if not already loaded
	if s.schema == nil {
		if err := s.LoadSchema(ctx); err != nil {
			return nil, err
		}
	}

	// Validate destination status exists
	if !s.schema.HasDirectory(to) {
		return nil, fmt.Errorf("%w: %s", ErrInvalidStatus, to)
	}

	// Find the item in the workflow
	from, itemPath, err := s.findItem(ctx, item)
	if err != nil {
		return nil, err
	}

	// Validate transition unless Force is set
	if !opts.Force && !s.schema.IsValidTransition(from, to) {
		return nil, fmt.Errorf("%w: cannot move from %s to %s", ErrInvalidTransition, from, to)
	}

	// Destination path
	destPath := filepath.Join(s.resolvePath(to), filepath.Base(itemPath))

	// Check if destination already exists
	if _, err := os.Stat(destPath); err == nil {
		return nil, fmt.Errorf("%w: %s", ErrAlreadyExists, destPath)
	}

	// Ensure destination directory exists
	if err := os.MkdirAll(s.resolvePath(to), 0755); err != nil {
		return nil, fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Move the item
	if err := os.Rename(itemPath, destPath); err != nil {
		return nil, fmt.Errorf("failed to move item: %w", err)
	}

	result := &MoveResult{
		Item:   item,
		From:   from,
		To:     to,
		Reason: opts.Reason,
	}

	// Log to history if enabled
	if s.schema.TrackHistory {
		entry := HistoryEntry{
			Item:   item,
			From:   from,
			To:     to,
			Reason: opts.Reason,
		}
		if err := s.appendHistory(ctx, entry); err != nil {
			// Log but don't fail the move
		}
	}

	return result, nil
}

// findItem searches for an item in all status directories.
// Returns the status directory and full path where the item was found.
func (s *Service) findItem(ctx context.Context, itemName string) (string, string, error) {
	for _, status := range s.schema.AllDirectories() {
		if ctx.Err() != nil {
			return "", "", ctx.Err()
		}

		itemPath := filepath.Join(s.resolvePath(status), itemName)
		if _, err := os.Stat(itemPath); err == nil {
			return status, itemPath, nil
		}
	}

	return "", "", fmt.Errorf("%w: %s", ErrItemNotFound, itemName)
}

// appendHistory adds an entry to the history file.
func (s *Service) appendHistory(ctx context.Context, entry HistoryEntry) error {
	// Will be implemented in the history task
	return nil
}
