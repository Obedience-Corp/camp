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
	"time"

	dungeonscaffold "github.com/Obedience-Corp/camp/internal/dungeon/scaffold"
	"github.com/Obedience-Corp/camp/internal/dungeon/statuspath"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/pathutil"
	"github.com/Obedience-Corp/camp/internal/workflow"
)

// Service errors.
// Sentinels marked with %w wrap the canonical sentinel from internal/errors
// to enable cross-package errors.Is() matching.
var (
	ErrNotFound               = camperrors.Wrap(camperrors.ErrNotFound, "item not found")
	ErrAlreadyExists          = camperrors.Wrap(camperrors.ErrAlreadyExists, "already exists")
	ErrNotInDungeon           = errors.New("item not in dungeon")
	ErrInvalidStatus          = camperrors.Wrap(camperrors.ErrInvalidInput, "invalid status")
	ErrInvalidDocsDestination = camperrors.Wrap(camperrors.ErrInvalidInput, "invalid docs destination")
	ErrInvalidItemPath        = camperrors.Wrap(camperrors.ErrInvalidInput, "invalid item path")
	ErrCrawlAborted           = camperrors.Wrap(camperrors.ErrCancelled, "crawl aborted")
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
	result, err := dungeonscaffold.Init(ctx, s.dungeonPath, dungeonscaffold.InitOptions{
		Force: opts.Force,
	})
	if err != nil {
		return nil, err
	}

	return &InitResult{
		CreatedDirs:  result.CreatedDirs,
		CreatedFiles: result.CreatedFiles,
		Skipped:      result.Skipped,
	}, nil
}

// ListStatusDirs scans the dungeon for all subdirectories, counts items in each
// (excluding .gitkeep), and returns them sorted alphabetically.
func (s *Service) ListStatusDirs(ctx context.Context) ([]StatusDir, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	entries, err := os.ReadDir(s.dungeonPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, camperrors.Wrap(err, "reading dungeon directory")
	}

	var dirs []StatusDir
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dirPath := filepath.Join(s.dungeonPath, entry.Name())

		count, err := statuspath.CountItems(dirPath)
		if err != nil {
			continue
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
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	entries, err := os.ReadDir(s.dungeonPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // Empty if dungeon doesn't exist
		}
		return nil, camperrors.Wrap(err, "reading dungeon directory")
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
		return camperrors.Wrap(err, "context cancelled")
	}

	// Strip trailing slash if present
	itemName = filepath.Clean(itemName)
	if itemName == "/" {
		return camperrors.Wrap(ErrNotInDungeon, "invalid item name")
	}

	srcPath := filepath.Join(s.dungeonPath, itemName)
	archivedDir := filepath.Join(s.dungeonPath, "archived")
	dstPath := statuspath.DatedItemPath(archivedDir, itemName, time.Now())

	// Verify source exists
	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		return camperrors.Wrap(ErrNotFound, itemName)
	}

	// Verify source is in dungeon root (not a path traversal)
	absSource, err := filepath.Abs(srcPath)
	if err != nil {
		return camperrors.Wrap(err, "resolving source path")
	}
	absDungeon, err := filepath.Abs(s.dungeonPath)
	if err != nil {
		return camperrors.Wrap(err, "resolving dungeon path")
	}
	if filepath.Dir(absSource) != absDungeon {
		return camperrors.Wrap(ErrNotInDungeon, itemName)
	}

	// Check if destination already exists
	if _, exists, err := statuspath.ExistingItemPath(archivedDir, itemName); err != nil {
		return camperrors.Wrap(err, "checking archived destination")
	} else if exists {
		return camperrors.Wrapf(ErrAlreadyExists, "%s already in archived/", itemName)
	}

	// Ensure archived date directory exists
	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return camperrors.Wrap(err, "creating archived directory")
	}

	// Move the item
	if err := os.Rename(srcPath, dstPath); err != nil {
		return camperrors.Wrapf(err, "moving %s to archived", itemName)
	}

	return nil
}

// AppendCrawlLog appends an entry to crawl.jsonl.
func (s *Service) AppendCrawlLog(ctx context.Context, entry CrawlEntry) error {
	if err := ctx.Err(); err != nil {
		return camperrors.Wrap(err, "context cancelled")
	}

	logPath := filepath.Join(s.dungeonPath, "crawl.jsonl")

	// Open file in append mode, create if doesn't exist
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return camperrors.Wrap(err, "opening crawl log")
	}
	defer f.Close()

	// Marshal entry to JSON
	data, err := json.Marshal(entry)
	if err != nil {
		return camperrors.Wrap(err, "marshaling entry")
	}

	// Write with newline
	if _, err := f.Write(append(data, '\n')); err != nil {
		return camperrors.Wrap(err, "writing entry")
	}

	return nil
}

// ListParentItems returns all items in the parent directory that are candidates
// for moving into the dungeon. It excludes the dungeon directory itself,
// campaign metadata, git directories, and other system files.
func (s *Service) ListParentItems(ctx context.Context, parentPath string) ([]DungeonItem, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	entries, err := os.ReadDir(parentPath)
	if err != nil {
		return nil, camperrors.Wrap(err, "reading parent directory")
	}

	excluded := map[string]bool{
		"dungeon":      true,
		".campaign":    true,
		".git":         true,
		"AGENTS.md":    true,
		"CLAUDE.md":    true,
		"OBEY.md":      true,
		"README.md":    true,
		".gitkeep":     true,
		".gitignore":   true,
		".crawlignore": true,
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

	// Check parent/.crawlignore for gitignore-style pattern exclusions.
	crawlIgnorePath := filepath.Join(parentPath, CrawlIgnoreFile)
	var crawlIgnore *CrawlIgnoreMatcher
	if m, err := LoadCrawlIgnore(crawlIgnorePath); err == nil {
		crawlIgnore = m
	} else if !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(os.Stderr, "Warning: failed to parse %s: %v\n", crawlIgnorePath, err)
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

		// Layer 5: gitignore-style pattern matching from .crawlignore.
		if crawlIgnore != nil {
			if matched, _ := crawlIgnore.Excludes(name, entry.IsDir()); matched {
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
		return camperrors.Wrap(err, "context cancelled")
	}
	validName, err := validateDirectChildItemName(itemName)
	if err != nil {
		return err
	}
	itemName = validName

	sourcePath := filepath.Join(parentPath, itemName)
	targetPath := filepath.Join(s.dungeonPath, itemName)

	if _, err := os.Stat(sourcePath); err != nil {
		return camperrors.Wrap(ErrNotFound, itemName)
	}

	if _, err := os.Stat(s.dungeonPath); err != nil {
		return camperrors.Wrap(err, "dungeon directory does not exist")
	}

	if _, err := os.Stat(targetPath); err == nil {
		return camperrors.Wrapf(ErrAlreadyExists, "%s already in dungeon", itemName)
	}

	if err := os.Rename(sourcePath, targetPath); err != nil {
		return camperrors.Wrapf(err, "moving %s to dungeon", itemName)
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
func (s *Service) MoveToStatus(ctx context.Context, itemName, status string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", camperrors.Wrap(err, "context cancelled")
	}

	if err := validateStatusName(status); err != nil {
		return "", err
	}
	validName, err := validateDirectChildItemName(itemName)
	if err != nil {
		return "", err
	}
	itemName = validName

	srcPath := filepath.Join(s.dungeonPath, itemName)
	statusDir := filepath.Join(s.dungeonPath, status)
	dstPath := statuspath.DatedItemPath(statusDir, itemName, time.Now())

	// Verify source exists
	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		return "", camperrors.Wrap(ErrNotFound, itemName)
	}

	// Verify source is in dungeon root (not a path traversal)
	absSource, err := filepath.Abs(srcPath)
	if err != nil {
		return "", camperrors.Wrap(err, "resolving source path")
	}
	absDungeon, err := filepath.Abs(s.dungeonPath)
	if err != nil {
		return "", camperrors.Wrap(err, "resolving dungeon path")
	}
	if filepath.Dir(absSource) != absDungeon {
		return "", camperrors.Wrap(ErrNotInDungeon, itemName)
	}

	// Check if destination already exists in any dated or legacy bucket
	if _, exists, err := statuspath.ExistingItemPath(statusDir, itemName); err != nil {
		return "", camperrors.Wrapf(err, "checking %s destination", status)
	} else if exists {
		return "", camperrors.Wrapf(ErrAlreadyExists, "%s already in %s/", itemName, status)
	}

	// Ensure status date directory exists
	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return "", camperrors.Wrapf(err, "creating %s directory", status)
	}

	if err := os.Rename(srcPath, dstPath); err != nil {
		return "", camperrors.Wrapf(err, "moving %s to %s", itemName, status)
	}

	return dstPath, nil
}

// MoveToDungeonStatus moves an item from a parent directory directly into a dungeon status directory.
// The status must be a simple directory name (no path separators).
func (s *Service) MoveToDungeonStatus(ctx context.Context, itemName, parentPath, status string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", camperrors.Wrap(err, "context cancelled")
	}

	if err := validateStatusName(status); err != nil {
		return "", err
	}
	validName, err := validateDirectChildItemName(itemName)
	if err != nil {
		return "", err
	}
	itemName = validName

	// Validate parentPath is within campaign root
	sourcePath := filepath.Join(parentPath, itemName)
	if err := pathutil.ValidateBoundary(s.campaignRoot, sourcePath); err != nil {
		return "", camperrors.Wrap(ErrNotInDungeon, "source outside campaign root")
	}

	statusDir := filepath.Join(s.dungeonPath, status)
	targetPath := statuspath.DatedItemPath(statusDir, itemName, time.Now())

	if _, err := os.Stat(sourcePath); err != nil {
		return "", camperrors.Wrap(ErrNotFound, itemName)
	}

	if _, exists, err := statuspath.ExistingItemPath(statusDir, itemName); err != nil {
		return "", camperrors.Wrapf(err, "checking %s destination", status)
	} else if exists {
		return "", camperrors.Wrapf(ErrAlreadyExists, "%s already in %s/", itemName, status)
	}

	// Ensure status date directory exists
	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return "", camperrors.Wrapf(err, "creating %s directory", status)
	}

	if err := os.Rename(sourcePath, targetPath); err != nil {
		return "", camperrors.Wrapf(err, "moving %s to dungeon/%s", itemName, status)
	}

	return targetPath, nil
}

// validateStatusName ensures a status name is safe (no path separators or traversal).
func validateStatusName(status string) error {
	if status == "" {
		return camperrors.Wrap(ErrInvalidStatus, "empty status name")
	}
	if strings.Contains(status, string(filepath.Separator)) || strings.Contains(status, "/") {
		return camperrors.Wrapf(ErrInvalidStatus, "%s (contains path separator)", status)
	}
	if status == "." || status == ".." {
		return camperrors.Wrap(ErrInvalidStatus, status)
	}
	return nil
}

func validateDirectChildItemName(itemName string) (string, error) {
	trimmed := strings.TrimSpace(itemName)
	if trimmed == "" {
		return "", camperrors.Wrapf(ErrInvalidItemPath, "%q is not a direct child item name", itemName)
	}
	if filepath.IsAbs(trimmed) {
		return "", camperrors.Wrapf(ErrInvalidItemPath, "%q is not a direct child item name", itemName)
	}

	cleaned := filepath.Clean(trimmed)
	if cleaned == "." || cleaned == ".." {
		return "", camperrors.Wrapf(ErrInvalidItemPath, "%q is not a direct child item name", itemName)
	}
	if cleaned != trimmed {
		return "", camperrors.Wrapf(ErrInvalidItemPath, "%q is not a direct child item name", itemName)
	}
	if cleaned != filepath.Base(cleaned) {
		return "", camperrors.Wrapf(ErrInvalidItemPath, "%q is not a direct child item name", itemName)
	}
	if strings.Contains(cleaned, "/") || strings.Contains(cleaned, "\\") {
		return "", camperrors.Wrapf(ErrInvalidItemPath, "%q is not a direct child item name", itemName)
	}

	return cleaned, nil
}
