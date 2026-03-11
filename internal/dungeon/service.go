package dungeon

import (
	"errors"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
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

// Path returns the full dungeon path.
func (s *Service) Path() string {
	return s.dungeonPath
}

// ArchivedPath returns the full path to the archived directory.
func (s *Service) ArchivedPath() string {
	return filepath.Join(s.dungeonPath, "archived")
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
