package dungeon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Obedience-Corp/camp/internal/dungeon/statuspath"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/workflow"
)

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
