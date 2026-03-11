package dungeon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/Obedience-Corp/camp/internal/dungeon/statuspath"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/pathutil"
)

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

	if _, exists, err := statuspath.ExistingItemPath(statusDir, itemName); err != nil {
		return "", camperrors.Wrapf(err, "checking %s destination", status)
	} else if exists {
		return "", camperrors.Wrapf(ErrAlreadyExists, "%s already in %s/", itemName, status)
	}

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

	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return "", camperrors.Wrapf(err, "creating %s directory", status)
	}

	if err := os.Rename(sourcePath, targetPath); err != nil {
		return "", camperrors.Wrapf(err, "moving %s to dungeon/%s", itemName, status)
	}

	return targetPath, nil
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

	if _, exists, err := statuspath.ExistingItemPath(archivedDir, itemName); err != nil {
		return camperrors.Wrap(err, "checking archived destination")
	} else if exists {
		return camperrors.Wrapf(ErrAlreadyExists, "%s already in archived/", itemName)
	}

	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return camperrors.Wrap(err, "creating archived directory")
	}

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
