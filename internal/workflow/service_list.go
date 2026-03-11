package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

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
		return nil, camperrors.Wrap(ErrInvalidStatus, status)
	}

	statusPath := s.resolvePath(status)
	entries, err := os.ReadDir(statusPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, camperrors.Wrap(ErrStatusNotFound, status)
		}
		return nil, camperrors.Wrapf(err, "failed to read directory %s", status)
	}

	result := &ListResult{
		Status: status,
		Items:  make([]Item, 0, len(entries)),
	}

	// System files to exclude from listings
	excludedFiles := map[string]bool{
		"OBEY.md":  true,
		".gitkeep": true,
	}

	// For v2 root listing, also exclude dungeon dir and hidden entries
	isRootListing := status == "." && s.schema != nil && s.schema.Version == 2

	for _, entry := range entries {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Skip excluded files
		if excludedFiles[entry.Name()] {
			continue
		}

		// Skip hidden files and dungeon when listing root in v2
		if isRootListing {
			if strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			if entry.Name() == "dungeon" && entry.IsDir() {
				continue
			}
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

	return "", "", camperrors.Wrap(ErrItemNotFound, itemName)
}
