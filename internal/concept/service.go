package concept

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/obediencecorp/camp/internal/config"
)

// Errors returned by the service.
var (
	ErrNotFound = errors.New("concept not found")
)

// DefaultService implements the Service interface.
type DefaultService struct {
	campaignRoot string
	paths        config.CampaignPaths
}

// NewService creates a new concept service.
func NewService(campaignRoot string, paths config.CampaignPaths) *DefaultService {
	return &DefaultService{
		campaignRoot: campaignRoot,
		paths:        paths,
	}
}

// List returns all available concepts from CampaignPaths.
func (s *DefaultService) List(ctx context.Context) ([]Concept, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	// Build concepts from CampaignPaths fields
	conceptDefs := []struct {
		name        string
		path        string
		description string
	}{
		{"projects", s.paths.Projects, "Projects directory"},
		{"worktrees", s.paths.Worktrees, "Git worktrees"},
		{"festivals", s.paths.Festivals, "Festivals directory"},
		{"intents", s.paths.Intents, "Intents directory"},
		{"workflow", s.paths.Workflow, "Workflow directory"},
		{"code_reviews", s.paths.CodeReviews, "Code reviews"},
		{"pipelines", s.paths.Pipelines, "Pipelines"},
		{"design", s.paths.Design, "Design documents"},
		{"ai_docs", s.paths.AIDocs, "AI documentation"},
		{"docs", s.paths.Docs, "Documentation"},
		{"dungeon", s.paths.Dungeon, "Archived/paused work"},
	}

	var concepts []Concept
	for _, def := range conceptDefs {
		if def.path == "" {
			continue
		}

		hasItems := s.hasItems(def.path)

		concepts = append(concepts, Concept{
			Name:        def.name,
			Path:        def.path,
			Description: def.description,
			HasItems:    hasItems,
		})
	}

	// Sort by name for consistent display
	sort.Slice(concepts, func(i, j int) bool {
		return concepts[i].Name < concepts[j].Name
	})

	return concepts, nil
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

	// Build full path including optional subpath
	fullPath := filepath.Join(s.campaignRoot, concept.Path)
	if subpath != "" {
		fullPath = filepath.Join(fullPath, subpath)
	}

	return s.listDirItems(fullPath)
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

	// Map concept names to paths
	pathMap := map[string]struct {
		path        string
		description string
	}{
		"projects":     {s.paths.Projects, "Projects directory"},
		"worktrees":    {s.paths.Worktrees, "Git worktrees"},
		"festivals":    {s.paths.Festivals, "Festivals directory"},
		"intents":      {s.paths.Intents, "Intents directory"},
		"workflow":     {s.paths.Workflow, "Workflow directory"},
		"code_reviews": {s.paths.CodeReviews, "Code reviews"},
		"pipelines":    {s.paths.Pipelines, "Pipelines"},
		"design":       {s.paths.Design, "Design documents"},
		"ai_docs":      {s.paths.AIDocs, "AI documentation"},
		"docs":         {s.paths.Docs, "Documentation"},
		"dungeon":      {s.paths.Dungeon, "Archived/paused work"},
	}

	info, ok := pathMap[name]
	if !ok || info.path == "" {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, name)
	}

	return &Concept{
		Name:        name,
		Path:        info.path,
		Description: info.description,
		HasItems:    s.hasItems(info.path),
	}, nil
}

// hasItems checks if the concept path has subdirectories.
func (s *DefaultService) hasItems(path string) bool {
	if path == "" {
		return false
	}

	fullPath := filepath.Join(s.campaignRoot, path)
	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		if entry.IsDir() && !isHidden(entry.Name()) {
			return true
		}
	}

	return false
}

// listDirItems returns directory entries as Items.
func (s *DefaultService) listDirItems(path string) ([]Item, error) {
	entries, err := os.ReadDir(path)
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
			Path:  filepath.Join(path, entry.Name()),
			IsDir: entry.IsDir(),
		}

		// Count children for directories
		if entry.IsDir() {
			item.Children = s.countChildren(filepath.Join(path, entry.Name()))
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
	paths        config.CampaignPaths
	fsys         fs.FS
}

// NewFSService creates a new concept service with a custom filesystem.
func NewFSService(campaignRoot string, paths config.CampaignPaths, fsys fs.FS) *FSService {
	return &FSService{
		campaignRoot: campaignRoot,
		paths:        paths,
		fsys:         fsys,
	}
}

// List returns all available concepts from CampaignPaths.
func (s *FSService) List(ctx context.Context) ([]Concept, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	// Build concepts from CampaignPaths fields
	conceptDefs := []struct {
		name        string
		path        string
		description string
	}{
		{"projects", s.paths.Projects, "Projects directory"},
		{"worktrees", s.paths.Worktrees, "Git worktrees"},
		{"festivals", s.paths.Festivals, "Festivals directory"},
		{"intents", s.paths.Intents, "Intents directory"},
		{"workflow", s.paths.Workflow, "Workflow directory"},
		{"code_reviews", s.paths.CodeReviews, "Code reviews"},
		{"pipelines", s.paths.Pipelines, "Pipelines"},
		{"design", s.paths.Design, "Design documents"},
		{"ai_docs", s.paths.AIDocs, "AI documentation"},
		{"docs", s.paths.Docs, "Documentation"},
		{"dungeon", s.paths.Dungeon, "Archived/paused work"},
	}

	var concepts []Concept
	for _, def := range conceptDefs {
		if def.path == "" {
			continue
		}

		hasItems := s.hasItems(def.path)

		concepts = append(concepts, Concept{
			Name:        def.name,
			Path:        def.path,
			Description: def.description,
			HasItems:    hasItems,
		})
	}

	// Sort by name for consistent display
	sort.Slice(concepts, func(i, j int) bool {
		return concepts[i].Name < concepts[j].Name
	})

	return concepts, nil
}

// ListItems returns subdirectories for a given concept.
// The subpath parameter allows drilling into nested directories.
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

	// Build path including optional subpath
	targetPath := concept.Path
	if subpath != "" {
		targetPath = filepath.Join(concept.Path, subpath)
	}

	return s.listDirItemsFS(targetPath)
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

	// Map concept names to paths
	pathMap := map[string]struct {
		path        string
		description string
	}{
		"projects":     {s.paths.Projects, "Projects directory"},
		"worktrees":    {s.paths.Worktrees, "Git worktrees"},
		"festivals":    {s.paths.Festivals, "Festivals directory"},
		"intents":      {s.paths.Intents, "Intents directory"},
		"workflow":     {s.paths.Workflow, "Workflow directory"},
		"code_reviews": {s.paths.CodeReviews, "Code reviews"},
		"pipelines":    {s.paths.Pipelines, "Pipelines"},
		"design":       {s.paths.Design, "Design documents"},
		"ai_docs":      {s.paths.AIDocs, "AI documentation"},
		"docs":         {s.paths.Docs, "Documentation"},
		"dungeon":      {s.paths.Dungeon, "Archived/paused work"},
	}

	info, ok := pathMap[name]
	if !ok || info.path == "" {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, name)
	}

	return &Concept{
		Name:        name,
		Path:        info.path,
		Description: info.description,
		HasItems:    s.hasItems(info.path),
	}, nil
}

// hasItems checks if the concept path has subdirectories via fs.FS.
func (s *FSService) hasItems(path string) bool {
	if path == "" {
		return false
	}

	entries, err := fs.ReadDir(s.fsys, path)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		if entry.IsDir() && !isHidden(entry.Name()) {
			return true
		}
	}

	return false
}

// listDirItemsFS returns directory entries as Items via fs.FS.
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
			Path:  filepath.Join(s.campaignRoot, path, entry.Name()),
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
