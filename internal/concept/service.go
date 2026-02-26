package concept

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Obedience-Corp/camp/internal/config"
)

// Errors returned by the service.
var (
	ErrNotFound = errors.New("concept not found")
)

// DefaultService implements the Service interface.
type DefaultService struct {
	campaignRoot string
	concepts     []config.ConceptEntry
}

// NewService creates a new concept service.
func NewService(campaignRoot string, concepts []config.ConceptEntry) *DefaultService {
	return &DefaultService{
		campaignRoot: campaignRoot,
		concepts:     concepts,
	}
}

// List returns all available concepts (order preserved from config).
func (s *DefaultService) List(ctx context.Context) ([]Concept, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	var result []Concept
	for _, c := range s.concepts {
		if c.Path == "" {
			continue
		}

		hasItems := s.hasItemsWithIgnore(c.Path, c.Ignore, c.Depth)

		result = append(result, Concept{
			Name:        c.Name,
			Path:        c.Path,
			Description: c.Description,
			HasItems:    hasItems,
			MaxDepth:    c.Depth,
			Ignore:      c.Ignore,
		})
	}

	return result, nil
}

// ListItems returns subdirectories for a given concept.
// The subpath parameter allows drilling into nested directories.
func (s *DefaultService) ListItems(ctx context.Context, conceptName, subpath string) ([]Item, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	concept, err := s.Get(ctx, conceptName)
	if err != nil {
		return nil, err
	}

	if concept.Path == "" {
		return []Item{}, nil
	}

	// Check depth limit
	// depth: 0 means no drilling at all
	if concept.MaxDepth != nil && *concept.MaxDepth == 0 {
		return []Item{}, nil
	}

	// Check depth for deeper levels
	currentDepth := countPathDepth(subpath) + 1 // +1 because we're listing children
	if concept.MaxDepth != nil && currentDepth > *concept.MaxDepth {
		return []Item{}, nil
	}

	// Build full path including optional subpath
	fullPath := filepath.Join(s.campaignRoot, concept.Path)
	if subpath != "" {
		fullPath = filepath.Join(fullPath, subpath)
	}

	// Build relative path (from campaign root) for item paths
	relativePath := concept.Path
	if subpath != "" {
		relativePath = filepath.Join(concept.Path, subpath)
	}

	items, err := s.listDirItems(fullPath, relativePath)
	if err != nil {
		return nil, err
	}

	// Filter out ignored paths (only at top level, subpath=="")
	if subpath == "" && len(concept.Ignore) > 0 {
		items = filterIgnored(items, concept.Ignore)
	}

	// Mark items as drill-disabled if at max depth
	if concept.MaxDepth != nil {
		currentDepth := countPathDepth(subpath)
		atMaxDepth := currentDepth+1 >= *concept.MaxDepth
		if atMaxDepth {
			for i := range items {
				items[i].DrillDisabled = true
			}
		}
	}

	return items, nil
}

// Resolve resolves a concept name and optional item to a full path.
func (s *DefaultService) Resolve(ctx context.Context, conceptName, item string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("context cancelled: %w", err)
	}

	concept, err := s.Get(ctx, conceptName)
	if err != nil {
		return "", err
	}

	if item == "" {
		return filepath.Join(s.campaignRoot, concept.Path), nil
	}

	return filepath.Join(s.campaignRoot, concept.Path, item), nil
}

// Get retrieves a concept by name.
func (s *DefaultService) Get(ctx context.Context, name string) (*Concept, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	for _, c := range s.concepts {
		if c.Name == name {
			return &Concept{
				Name:        c.Name,
				Path:        c.Path,
				Description: c.Description,
				HasItems:    s.hasItemsWithIgnore(c.Path, c.Ignore, c.Depth),
				MaxDepth:    c.Depth,
				Ignore:      c.Ignore,
			}, nil
		}
	}

	return nil, fmt.Errorf("%w: %s", ErrNotFound, name)
}

// hasItemsWithIgnore checks if the concept path has subdirectories (excluding ignored).
func (s *DefaultService) hasItemsWithIgnore(path string, ignore []string, depth *int) bool {
	if path == "" {
		return false
	}

	// depth: 0 means no drilling, so no items
	if depth != nil && *depth == 0 {
		return false
	}

	fullPath := filepath.Join(s.campaignRoot, path)
	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return false
	}

	ignoreSet := makeIgnoreSet(ignore)

	for _, entry := range entries {
		if entry.IsDir() && !isHidden(entry.Name()) {
			if !ignoreSet[entry.Name()+"/"] && !ignoreSet[entry.Name()] {
				return true
			}
		}
	}

	return false
}

// listDirItems returns directory entries as Items.
// fullPath is the absolute path for reading, relativePath is the path from campaign root for Item.Path.
func (s *DefaultService) listDirItems(fullPath, relativePath string) ([]Item, error) {
	entries, err := os.ReadDir(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []Item{}, nil
		}
		return nil, fmt.Errorf("reading directory: %w", err)
	}

	var items []Item
	for _, entry := range entries {
		if isHidden(entry.Name()) {
			continue
		}

		item := Item{
			Name:  entry.Name(),
			Path:  filepath.Join(relativePath, entry.Name()), // Relative path from campaign root
			IsDir: entry.IsDir(),
		}

		// Count children for directories
		if entry.IsDir() {
			item.Children = s.countChildren(filepath.Join(fullPath, entry.Name()))
		}

		items = append(items, item)
	}

	// Sort directories first, then by name
	sort.Slice(items, func(i, j int) bool {
		if items[i].IsDir != items[j].IsDir {
			return items[i].IsDir // Directories first
		}
		return items[i].Name < items[j].Name
	})

	return items, nil
}

// countChildren returns the count of non-hidden children in a directory.
func (s *DefaultService) countChildren(path string) int {
	entries, err := os.ReadDir(path)
	if err != nil {
		return 0
	}

	count := 0
	for _, entry := range entries {
		if !isHidden(entry.Name()) {
			count++
		}
	}
	return count
}

// isHidden returns true if the name starts with a dot.
func isHidden(name string) bool {
	return len(name) > 0 && name[0] == '.'
}

// countPathDepth counts the number of path segments in a subpath.
func countPathDepth(subpath string) int {
	if subpath == "" {
		return 0
	}
	return len(strings.Split(filepath.Clean(subpath), string(filepath.Separator)))
}

// makeIgnoreSet creates a set from ignore patterns for fast lookup.
func makeIgnoreSet(ignore []string) map[string]bool {
	set := make(map[string]bool, len(ignore))
	for _, pattern := range ignore {
		set[pattern] = true
		// Also add without trailing slash
		set[strings.TrimSuffix(pattern, "/")] = true
	}
	return set
}

// filterIgnored removes items matching ignore patterns.
func filterIgnored(items []Item, ignore []string) []Item {
	if len(ignore) == 0 {
		return items
	}

	ignoreSet := makeIgnoreSet(ignore)
	var filtered []Item
	for _, item := range items {
		if !ignoreSet[item.Name+"/"] && !ignoreSet[item.Name] {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

// ResolvePath validates a path exists and returns its Item details.
func (s *DefaultService) ResolvePath(ctx context.Context, path string) (*Item, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	if path == "" {
		return nil, errors.New("empty path")
	}

	// Normalize path
	path = filepath.Clean(path)

	// Build full path from campaign root
	fullPath := filepath.Join(s.campaignRoot, path)

	// Check if path exists
	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("path not found: %s", path)
		}
		return nil, fmt.Errorf("checking path: %w", err)
	}

	item := &Item{
		Name:  filepath.Base(path),
		Path:  fullPath,
		IsDir: info.IsDir(),
	}

	if info.IsDir() {
		item.Children = s.countChildren(fullPath)
	}

	return item, nil
}

// ConceptForPath returns the concept that contains the given path.
func (s *DefaultService) ConceptForPath(ctx context.Context, path string) (*Concept, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	concepts, err := s.List(ctx)
	if err != nil {
		return nil, err
	}

	// Normalize path for comparison
	path = filepath.Clean(path)

	for _, c := range concepts {
		conceptPath := filepath.Clean(c.Path)
		// Check if path starts with or equals the concept path
		if path == conceptPath || hasPathPrefix(path, conceptPath) {
			return &c, nil
		}
	}

	return nil, fmt.Errorf("path not within any concept: %s", path)
}

// hasPathPrefix checks if path starts with prefix as a proper path prefix.
func hasPathPrefix(path, prefix string) bool {
	if len(path) <= len(prefix) {
		return false
	}
	// Check that path starts with prefix and has a path separator after it
	return path[:len(prefix)] == prefix && path[len(prefix)] == filepath.Separator
}

// Ensure DefaultService implements Service.
var _ Service = (*DefaultService)(nil)

// FSService is a service implementation that uses an fs.FS for testability.
type FSService struct {
	campaignRoot string
	concepts     []config.ConceptEntry
	fsys         fs.FS
}

// NewFSService creates a new concept service with a custom filesystem.
func NewFSService(campaignRoot string, concepts []config.ConceptEntry, fsys fs.FS) *FSService {
	return &FSService{
		campaignRoot: campaignRoot,
		concepts:     concepts,
		fsys:         fsys,
	}
}

// List returns all available concepts (order preserved from config).
func (s *FSService) List(ctx context.Context) ([]Concept, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	var result []Concept
	for _, c := range s.concepts {
		if c.Path == "" {
			continue
		}

		hasItems := s.hasItemsWithIgnore(c.Path, c.Ignore, c.Depth)

		result = append(result, Concept{
			Name:        c.Name,
			Path:        c.Path,
			Description: c.Description,
			HasItems:    hasItems,
			MaxDepth:    c.Depth,
			Ignore:      c.Ignore,
		})
	}

	return result, nil
}

// ListItems returns subdirectories for a given concept.
func (s *FSService) ListItems(ctx context.Context, conceptName, subpath string) ([]Item, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	concept, err := s.Get(ctx, conceptName)
	if err != nil {
		return nil, err
	}

	if concept.Path == "" {
		return []Item{}, nil
	}

	// Check depth limit
	if concept.MaxDepth != nil && *concept.MaxDepth == 0 {
		return []Item{}, nil
	}

	currentDepth := countPathDepth(subpath) + 1
	if concept.MaxDepth != nil && currentDepth > *concept.MaxDepth {
		return []Item{}, nil
	}

	// Build path including optional subpath
	targetPath := concept.Path
	if subpath != "" {
		targetPath = filepath.Join(concept.Path, subpath)
	}

	items, err := s.listDirItemsFS(targetPath)
	if err != nil {
		return nil, err
	}

	// Filter out ignored paths at top level
	if subpath == "" && len(concept.Ignore) > 0 {
		items = filterIgnored(items, concept.Ignore)
	}

	// Mark items as drill-disabled if at max depth
	if concept.MaxDepth != nil {
		currentDepth := countPathDepth(subpath)
		atMaxDepth := currentDepth+1 >= *concept.MaxDepth
		if atMaxDepth {
			for i := range items {
				items[i].DrillDisabled = true
			}
		}
	}

	return items, nil
}

// Resolve resolves a concept name and optional item to a full path.
func (s *FSService) Resolve(ctx context.Context, conceptName, item string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("context cancelled: %w", err)
	}

	concept, err := s.Get(ctx, conceptName)
	if err != nil {
		return "", err
	}

	if item == "" {
		return filepath.Join(s.campaignRoot, concept.Path), nil
	}

	return filepath.Join(s.campaignRoot, concept.Path, item), nil
}

// Get retrieves a concept by name.
func (s *FSService) Get(ctx context.Context, name string) (*Concept, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	for _, c := range s.concepts {
		if c.Name == name {
			return &Concept{
				Name:        c.Name,
				Path:        c.Path,
				Description: c.Description,
				HasItems:    s.hasItemsWithIgnore(c.Path, c.Ignore, c.Depth),
				MaxDepth:    c.Depth,
				Ignore:      c.Ignore,
			}, nil
		}
	}

	return nil, fmt.Errorf("%w: %s", ErrNotFound, name)
}

// hasItemsWithIgnore checks if the concept path has subdirectories via fs.FS.
func (s *FSService) hasItemsWithIgnore(path string, ignore []string, depth *int) bool {
	if path == "" {
		return false
	}

	if depth != nil && *depth == 0 {
		return false
	}

	entries, err := fs.ReadDir(s.fsys, path)
	if err != nil {
		return false
	}

	ignoreSet := makeIgnoreSet(ignore)

	for _, entry := range entries {
		if entry.IsDir() && !isHidden(entry.Name()) {
			if !ignoreSet[entry.Name()+"/"] && !ignoreSet[entry.Name()] {
				return true
			}
		}
	}

	return false
}

// listDirItemsFS returns directory entries as Items via fs.FS.
// path is relative to campaign root (used for both reading from fsys and storing in Item.Path).
func (s *FSService) listDirItemsFS(path string) ([]Item, error) {
	entries, err := fs.ReadDir(s.fsys, path)
	if err != nil {
		return []Item{}, nil
	}

	var items []Item
	for _, entry := range entries {
		if isHidden(entry.Name()) {
			continue
		}

		item := Item{
			Name:  entry.Name(),
			Path:  filepath.Join(path, entry.Name()), // Relative path from campaign root
			IsDir: entry.IsDir(),
		}

		// Count children for directories
		if entry.IsDir() {
			item.Children = s.countChildrenFS(filepath.Join(path, entry.Name()))
		}

		items = append(items, item)
	}

	// Sort directories first, then by name
	sort.Slice(items, func(i, j int) bool {
		if items[i].IsDir != items[j].IsDir {
			return items[i].IsDir
		}
		return items[i].Name < items[j].Name
	})

	return items, nil
}

// countChildrenFS returns the count of non-hidden children in a directory via fs.FS.
func (s *FSService) countChildrenFS(path string) int {
	entries, err := fs.ReadDir(s.fsys, path)
	if err != nil {
		return 0
	}

	count := 0
	for _, entry := range entries {
		if !isHidden(entry.Name()) {
			count++
		}
	}
	return count
}

// ResolvePath validates a path exists and returns its Item details via fs.FS.
func (s *FSService) ResolvePath(ctx context.Context, path string) (*Item, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	if path == "" {
		return nil, errors.New("empty path")
	}

	// Normalize path
	path = filepath.Clean(path)

	// Check if path exists using fs.Stat
	info, err := fs.Stat(s.fsys, path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("path not found: %s", path)
		}
		return nil, fmt.Errorf("checking path: %w", err)
	}

	item := &Item{
		Name:  filepath.Base(path),
		Path:  filepath.Join(s.campaignRoot, path),
		IsDir: info.IsDir(),
	}

	if info.IsDir() {
		item.Children = s.countChildrenFS(path)
	}

	return item, nil
}

// ConceptForPath returns the concept that contains the given path via fs.FS.
func (s *FSService) ConceptForPath(ctx context.Context, path string) (*Concept, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	concepts, err := s.List(ctx)
	if err != nil {
		return nil, err
	}

	// Normalize path for comparison
	path = filepath.Clean(path)

	for _, c := range concepts {
		conceptPath := filepath.Clean(c.Path)
		// Check if path starts with or equals the concept path
		if path == conceptPath || hasPathPrefix(path, conceptPath) {
			return &c, nil
		}
	}

	return nil, fmt.Errorf("path not within any concept: %s", path)
}

// Ensure FSService implements Service.
var _ Service = (*FSService)(nil)
