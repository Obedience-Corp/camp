package dungeon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// Service errors.
var (
	ErrNotFound      = errors.New("item not found")
	ErrAlreadyExists = errors.New("already exists")
	ErrNotInDungeon  = errors.New("item not in dungeon")
	ErrInvalidStatus = errors.New("invalid status")
)

// ValidStatuses lists the valid dungeon status directories.
var ValidStatuses = []string{"completed", "archived", "someday"}

// Service provides operations for managing the dungeon directory.
type Service struct {
	campaignRoot string
	dungeonPath  string
}

// NewService creates a new dungeon Service.
// dungeonPath is the full path to the dungeon directory (e.g., from PathResolver.Dungeon()).
func NewService(campaignRoot, dungeonPath string) *Service {
	return &Service{
		campaignRoot: campaignRoot,
		dungeonPath:  dungeonPath,
	}
}

// InitOptions contains options for initializing the dungeon.
type InitOptions struct {
	Force bool // Overwrite existing files
}

// InitResult contains information about what was created during init.
type InitResult struct {
	CreatedDirs  []string
	CreatedFiles []string
	Skipped      []string
}

// Init creates the dungeon directory structure.
// This creates the flow-compatible dungeon structure:
// - dungeon/
// - dungeon/completed/
// - dungeon/archived/
// - dungeon/someday/
// - dungeon/OBEY.md
// This operation is idempotent unless Force is true.
func (s *Service) Init(ctx context.Context, opts InitOptions) (*InitResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	result := &InitResult{}

	// Create directories - flow-compatible structure
	dirs := []string{
		s.dungeonPath,
		filepath.Join(s.dungeonPath, "completed"),
		filepath.Join(s.dungeonPath, "archived"),
		filepath.Join(s.dungeonPath, "someday"),
	}

	for _, dir := range dirs {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("context cancelled: %w", err)
		}

		if _, err := os.Stat(dir); os.IsNotExist(err) {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return nil, fmt.Errorf("creating directory %s: %w", dir, err)
			}
			result.CreatedDirs = append(result.CreatedDirs, dir)
		}
	}

	// Create OBEY.md template file only
	obeyPath := filepath.Join(s.dungeonPath, "OBEY.md")

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	exists := false
	if _, err := os.Stat(obeyPath); err == nil {
		exists = true
	}

	if exists && !opts.Force {
		result.Skipped = append(result.Skipped, obeyPath)
	} else {
		content, err := GetOBEYTemplate()
		if err != nil {
			return nil, fmt.Errorf("reading template for %s: %w", obeyPath, err)
		}

		if err := os.WriteFile(obeyPath, content, 0644); err != nil {
			return nil, fmt.Errorf("writing %s: %w", obeyPath, err)
		}
		result.CreatedFiles = append(result.CreatedFiles, obeyPath)
	}

	// Create .gitkeep in empty status directories
	statusDirs := []string{"completed", "archived", "someday"}
	for _, dir := range statusDirs {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("context cancelled: %w", err)
		}

		gitkeepPath := filepath.Join(s.dungeonPath, dir, ".gitkeep")
		if _, err := os.Stat(gitkeepPath); os.IsNotExist(err) {
			if err := os.WriteFile(gitkeepPath, []byte{}, 0644); err != nil {
				return nil, fmt.Errorf("failed to create .gitkeep in %s: %w", dir, err)
			}
			result.CreatedFiles = append(result.CreatedFiles, filepath.Join(dir, ".gitkeep"))
		}
	}

	return result, nil
}

// ListItems returns all items at the dungeon root (excluding subdirectories and system files).
func (s *Service) ListItems(ctx context.Context) ([]DungeonItem, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	entries, err := os.ReadDir(s.dungeonPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // Empty if dungeon doesn't exist
		}
		return nil, fmt.Errorf("reading dungeon directory: %w", err)
	}

	var items []DungeonItem
	excludedNames := map[string]bool{
		"completed":   true,
		"archived":    true,
		"someday":     true,
		"OBEY.md":     true,
		"crawl.jsonl": true,
	}

	for _, entry := range entries {
		if excludedNames[entry.Name()] {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue // Skip entries we can't stat
		}

		itemType := ItemTypeFile
		name := entry.Name()
		if entry.IsDir() {
			itemType = ItemTypeDirectory
			name = entry.Name() + "/" // Indicate directory
		}

		items = append(items, DungeonItem{
			Name:    name,
			Path:    filepath.Join(s.dungeonPath, entry.Name()),
			Type:    itemType,
			ModTime: info.ModTime(),
		})
	}

	// Sort by modification time (oldest first for review)
	sort.Slice(items, func(i, j int) bool {
		return items[i].ModTime.Before(items[j].ModTime)
	})

	return items, nil
}

// Archive moves an item from the dungeon root to archived/.
func (s *Service) Archive(ctx context.Context, itemName string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context cancelled: %w", err)
	}

	// Strip trailing slash if present
	itemName = filepath.Clean(itemName)
	if itemName == "/" {
		return fmt.Errorf("%w: invalid item name", ErrNotInDungeon)
	}

	srcPath := filepath.Join(s.dungeonPath, itemName)
	dstPath := filepath.Join(s.dungeonPath, "archived", itemName)

	// Verify source exists
	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		return fmt.Errorf("%w: %s", ErrNotFound, itemName)
	}

	// Verify source is in dungeon root (not a path traversal)
	absSource, err := filepath.Abs(srcPath)
	if err != nil {
		return fmt.Errorf("resolving source path: %w", err)
	}
	absDungeon, err := filepath.Abs(s.dungeonPath)
	if err != nil {
		return fmt.Errorf("resolving dungeon path: %w", err)
	}
	if filepath.Dir(absSource) != absDungeon {
		return fmt.Errorf("%w: %s", ErrNotInDungeon, itemName)
	}

	// Ensure archived directory exists
	archivedDir := filepath.Join(s.dungeonPath, "archived")
	if err := os.MkdirAll(archivedDir, 0755); err != nil {
		return fmt.Errorf("creating archived directory: %w", err)
	}

	// Check if destination already exists
	if _, err := os.Stat(dstPath); err == nil {
		return fmt.Errorf("%w: %s already in archived/", ErrAlreadyExists, itemName)
	}

	// Move the item
	if err := os.Rename(srcPath, dstPath); err != nil {
		return fmt.Errorf("moving %s to archived: %w", itemName, err)
	}

	return nil
}

// AppendCrawlLog appends an entry to crawl.jsonl.
func (s *Service) AppendCrawlLog(ctx context.Context, entry CrawlEntry) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context cancelled: %w", err)
	}

	logPath := filepath.Join(s.dungeonPath, "crawl.jsonl")

	// Open file in append mode, create if doesn't exist
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("opening crawl log: %w", err)
	}
	defer f.Close()

	// Marshal entry to JSON
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshaling entry: %w", err)
	}

	// Write with newline
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("writing entry: %w", err)
	}

	return nil
}

// ListParentItems returns all items in the parent directory that are candidates
// for moving into the dungeon. It excludes the dungeon directory itself,
// campaign metadata, git directories, and other system files.
func (s *Service) ListParentItems(ctx context.Context, parentPath string) ([]DungeonItem, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	entries, err := os.ReadDir(parentPath)
	if err != nil {
		return nil, fmt.Errorf("reading parent directory: %w", err)
	}

	excluded := map[string]bool{
		"dungeon":    true,
		".campaign":  true,
		".git":       true,
		"CLAUDE.md":  true,
		".gitkeep":   true,
		".gitignore": true,
	}

	var items []DungeonItem
	for _, entry := range entries {
		name := entry.Name()

		if excluded[name] {
			continue
		}

		// Skip hidden files not explicitly excluded
		if strings.HasPrefix(name, ".") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		itemType := ItemTypeFile
		if entry.IsDir() {
			itemType = ItemTypeDirectory
		}

		items = append(items, DungeonItem{
			Name:    name,
			Path:    filepath.Join(parentPath, name),
			Type:    itemType,
			ModTime: info.ModTime(),
		})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].ModTime.Before(items[j].ModTime)
	})

	return items, nil
}

// MoveToDungeon moves an item from the parent directory into the dungeon root.
func (s *Service) MoveToDungeon(ctx context.Context, itemName, parentPath string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context cancelled: %w", err)
	}

	sourcePath := filepath.Join(parentPath, itemName)
	targetPath := filepath.Join(s.dungeonPath, itemName)

	if _, err := os.Stat(sourcePath); err != nil {
		return fmt.Errorf("%w: %s", ErrNotFound, itemName)
	}

	if _, err := os.Stat(s.dungeonPath); err != nil {
		return fmt.Errorf("dungeon directory does not exist: %w", err)
	}

	if _, err := os.Stat(targetPath); err == nil {
		return fmt.Errorf("%w: %s already in dungeon", ErrAlreadyExists, itemName)
	}

	if err := os.Rename(sourcePath, targetPath); err != nil {
		return fmt.Errorf("moving %s to dungeon: %w", itemName, err)
	}

	return nil
}

// Path returns the full dungeon path.
func (s *Service) Path() string {
	return s.dungeonPath
}

// ArchivedPath returns the full path to the archived directory.
func (s *Service) ArchivedPath() string {
	return filepath.Join(s.dungeonPath, "archived")
}

// MoveToStatus moves an item from the dungeon root to a status directory.
// Status must be one of: completed, archived, someday.
func (s *Service) MoveToStatus(ctx context.Context, itemName, status string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context cancelled: %w", err)
	}

	if !isValidStatus(status) {
		return fmt.Errorf("%w: %s", ErrInvalidStatus, status)
	}

	itemName = filepath.Clean(itemName)
	if itemName == "/" {
		return fmt.Errorf("%w: invalid item name", ErrNotInDungeon)
	}

	srcPath := filepath.Join(s.dungeonPath, itemName)
	dstPath := filepath.Join(s.dungeonPath, status, itemName)

	// Verify source exists
	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		return fmt.Errorf("%w: %s", ErrNotFound, itemName)
	}

	// Verify source is in dungeon root (not a path traversal)
	absSource, err := filepath.Abs(srcPath)
	if err != nil {
		return fmt.Errorf("resolving source path: %w", err)
	}
	absDungeon, err := filepath.Abs(s.dungeonPath)
	if err != nil {
		return fmt.Errorf("resolving dungeon path: %w", err)
	}
	if filepath.Dir(absSource) != absDungeon {
		return fmt.Errorf("%w: %s", ErrNotInDungeon, itemName)
	}

	// Ensure status directory exists
	statusDir := filepath.Join(s.dungeonPath, status)
	if err := os.MkdirAll(statusDir, 0755); err != nil {
		return fmt.Errorf("creating %s directory: %w", status, err)
	}

	// Check if destination already exists
	if _, err := os.Stat(dstPath); err == nil {
		return fmt.Errorf("%w: %s already in %s/", ErrAlreadyExists, itemName, status)
	}

	if err := os.Rename(srcPath, dstPath); err != nil {
		return fmt.Errorf("moving %s to %s: %w", itemName, status, err)
	}

	return nil
}

// MoveToDungeonStatus moves an item from a parent directory directly into a dungeon status directory.
// Status must be one of: completed, archived, someday.
func (s *Service) MoveToDungeonStatus(ctx context.Context, itemName, parentPath, status string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context cancelled: %w", err)
	}

	if !isValidStatus(status) {
		return fmt.Errorf("%w: %s", ErrInvalidStatus, status)
	}

	sourcePath := filepath.Join(parentPath, itemName)
	targetPath := filepath.Join(s.dungeonPath, status, itemName)

	if _, err := os.Stat(sourcePath); err != nil {
		return fmt.Errorf("%w: %s", ErrNotFound, itemName)
	}

	// Ensure status directory exists
	statusDir := filepath.Join(s.dungeonPath, status)
	if err := os.MkdirAll(statusDir, 0755); err != nil {
		return fmt.Errorf("creating %s directory: %w", status, err)
	}

	if _, err := os.Stat(targetPath); err == nil {
		return fmt.Errorf("%w: %s already in %s/", ErrAlreadyExists, itemName, status)
	}

	if err := os.Rename(sourcePath, targetPath); err != nil {
		return fmt.Errorf("moving %s to dungeon/%s: %w", itemName, status, err)
	}

	return nil
}

func isValidStatus(status string) bool {
	return slices.Contains(ValidStatuses, status)
}
