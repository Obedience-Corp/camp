package workflow

import (
	"context"
	"os"
	"path/filepath"
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

// HasParentFlow checks if any parent directory of dir contains a .workflow.yaml file.
// Returns the path to the parent schema file and true if found, empty string and false otherwise.
// This is used to prevent flow nesting.
func HasParentFlow(dir string) (string, bool) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", false
	}

	current := filepath.Dir(absDir)
	for current != "/" && current != "." && current != absDir {
		schemaPath := filepath.Join(current, SchemaFileName)
		if _, err := os.Stat(schemaPath); err == nil {
			return schemaPath, true
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return "", false
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
	Force         bool   // Overwrite existing files
	SchemaVersion int    // Schema version to use (0 or 1 = v1, 2 = v2)
	Name          string // Workflow name (empty = default)
	Description   string // Workflow description/purpose (empty = default)
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

// MigrateV1ToV2Result contains the result of a v1 to v2 migration.
type MigrateV1ToV2Result struct {
	MovedItems   []string // Items moved
	RemovedDirs  []string // Directories removed
	SchemaUpdate bool     // Whether schema was updated
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
	Item            string
	From            string
	To              string
	Reason          string
	SourcePath      string
	DestinationPath string
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
