// Package concept provides types and operations for managing concepts,
// which are named directory shortcuts with discoverable subdirectories.
package concept

import "context"

// Concept represents a named concept (directory shortcut) in a campaign.
// Concepts can contain items (subdirectories) that can be selected via cascading menus.
type Concept struct {
	// Name is the short name of the concept (e.g., "p" for projects).
	Name string

	// Path is the relative path from campaign root (e.g., "projects/").
	Path string

	// Description provides human-readable help text.
	Description string

	// HasItems indicates whether this concept has subdirectory items.
	// If true, the concept expands to show available items when selected.
	HasItems bool

	// MaxDepth controls drilling depth: nil=unlimited, 0=no drilling, 1+=levels.
	MaxDepth *int

	// Ignore lists subdirectory paths to exclude from listing.
	Ignore []string
}

// Item represents a selectable item within a concept.
// Items are typically directories within the concept's path.
type Item struct {
	// Name is the display name of the item (directory name).
	Name string

	// Path is the full relative path from campaign root.
	Path string

	// IsDir indicates whether this item is a directory.
	IsDir bool

	// Children is the count of non-hidden children for directories.
	// Used by UI to show drill-down indicators.
	Children int

	// DrillDisabled indicates drilling is disabled (due to depth limit).
	// When true, UI should not show drill arrow OR "(empty)" label.
	DrillDisabled bool
}

// Service provides operations for working with concepts.
type Service interface {
	// List returns all available concepts in the campaign.
	List(ctx context.Context) ([]Concept, error)

	// ListItems returns items (subdirectories) for a given concept.
	// The subpath parameter allows drilling into nested directories.
	// Returns empty slice if the concept has no items or directory doesn't exist.
	ListItems(ctx context.Context, conceptName, subpath string) ([]Item, error)

	// Resolve resolves a concept name and optional item to a full path.
	// If item is empty, returns the concept's path.
	// If item is provided, returns the item's path within the concept.
	Resolve(ctx context.Context, conceptName, item string) (string, error)

	// ResolvePath validates a path exists and returns its Item details.
	// The path is relative to the campaign root.
	// Returns an error if the path does not exist or is invalid.
	ResolvePath(ctx context.Context, path string) (*Item, error)

	// ConceptForPath returns the concept that contains the given path.
	// Returns nil if the path is not within any known concept.
	ConceptForPath(ctx context.Context, path string) (*Concept, error)

	// Get retrieves a concept by name.
	Get(ctx context.Context, name string) (*Concept, error)
}
