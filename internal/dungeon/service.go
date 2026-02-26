package dungeon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Obedience-Corp/camp/internal/workflow"
)

// Service errors.
var (
	ErrNotFound      = errors.New("item not found")
	ErrAlreadyExists = errors.New("already exists")
	ErrNotInDungeon  = errors.New("item not in dungeon")
	ErrInvalidStatus = errors.New("invalid status")
)

// systemFiles are non-status entries excluded from item listings.
var systemFiles = map[string]bool{
	"OBEY.md":       true,
	"crawl.jsonl":   true,
	CrawlConfigFile: true,
}

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

// ListStatusDirs scans the dungeon for all subdirectories, counts items in each
// (excluding .gitkeep), and returns them sorted alphabetically.
func (s *Service) ListStatusDirs(ctx context.Context) ([]StatusDir, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	entries, err := os.ReadDir(s.dungeonPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading dungeon directory: %w", err)
	}

	var dirs []StatusDir
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dirPath := filepath.Join(s.dungeonPath, entry.Name())

		// Count items (excluding .gitkeep)
		subEntries, err := os.ReadDir(dirPath)
		if err != nil {
			continue
		}
		count := 0
		for _, sub := range subEntries {
			if sub.Name() != ".gitkeep" {
				count++
			}
		}

		dirs = append(dirs, StatusDir{
			Name:      entry.Name(),
			Path:      dirPath,
			ItemCount: count,
		})
	}

	sort.Slice(dirs, func(i, j int) bool {
		return dirs[i].Name < dirs[j].Name
	})

	return dirs, nil
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

	for _, entry := range entries {
		// Skip all subdirectories (they are status dirs)
		if entry.IsDir() {
			continue
		}

		// Skip system files
		if systemFiles[entry.Name()] {
			continue
		}

		// Skip hidden files
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue // Skip entries we can't stat
		}

		items = append(items, DungeonItem{
			Name:    entry.Name(),
			Path:    filepath.Join(s.dungeonPath, entry.Name()),
			Type:    ItemTypeFile,
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
		"AGENTS.md":  true,
		"CLAUDE.md":  true,
		"OBEY.md":    true,
		"README.md":  true,
		".gitkeep":   true,
		".gitignore": true,
	}

	// Check .workflow.yaml for structural directory exclusions.
	// If the parent has a workflow schema, all defined directories are structural
	// and should not appear as triage candidates.
	schemaPath := filepath.Join(parentPath, workflow.SchemaFileName)
	if schema, err := workflow.LoadSchema(ctx, schemaPath); err == nil {
		for name := range schema.Directories {
			if name != "." {
				excluded[name] = true
			}
		}
	}

	// Check dungeon/.crawl.yaml for explicit parent-level exclusions.
	// This allows each dungeon to declare which sibling directories are
	// structural and should be skipped during triage.
	crawlCfgPath := filepath.Join(s.dungeonPath, CrawlConfigFile)
	if cfg, err := loadCrawlConfig(crawlCfgPath); err == nil {
		for _, name := range cfg.Excludes {
			excluded[name] = true
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		// File exists but failed to parse — warn so the user can fix it.
		fmt.Fprintf(os.Stderr, "Warning: failed to parse %s: %v\n", crawlCfgPath, err)
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

		// Skip directories that contain OBEY.md (managed campaign directories).
		// These are structural directories that should not be triage candidates.
		if entry.IsDir() {
			obeyPath := filepath.Join(parentPath, name, "OBEY.md")
			if _, err := os.Stat(obeyPath); err == nil {
				continue
			}
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
// The status must be a simple directory name (no path separators).
func (s *Service) MoveToStatus(ctx context.Context, itemName, status string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context cancelled: %w", err)
	}

	if err := validateStatusName(status); err != nil {
		return err
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
// The status must be a simple directory name (no path separators).
func (s *Service) MoveToDungeonStatus(ctx context.Context, itemName, parentPath, status string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context cancelled: %w", err)
	}

	if err := validateStatusName(status); err != nil {
		return err
	}

	// Validate parentPath is within campaign root
	absSource, err := filepath.Abs(filepath.Join(parentPath, itemName))
	if err != nil {
		return fmt.Errorf("resolving source path: %w", err)
	}
	absCampaignRoot, err := filepath.Abs(s.campaignRoot)
	if err != nil {
		return fmt.Errorf("resolving campaign root: %w", err)
	}
	if !strings.HasPrefix(absSource, absCampaignRoot+string(filepath.Separator)) {
		return fmt.Errorf("%w: source outside campaign root", ErrNotInDungeon)
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

// validateStatusName ensures a status name is safe (no path separators or traversal).
func validateStatusName(status string) error {
	if status == "" {
		return fmt.Errorf("%w: empty status name", ErrInvalidStatus)
	}
	if strings.Contains(status, string(filepath.Separator)) || strings.Contains(status, "/") {
		return fmt.Errorf("%w: %s (contains path separator)", ErrInvalidStatus, status)
	}
	if status == "." || status == ".." {
		return fmt.Errorf("%w: %s", ErrInvalidStatus, status)
	}
	return nil
}
