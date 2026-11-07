package project

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/Obedience-Corp/camp/internal/dirlink"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// BrokenProjectLink is an unresolved local project symlink. It is optional
// machine setup, so clone and sync report it without treating it as failure.
type BrokenProjectLink struct {
	Path   string `json:"path"`
	Target string `json:"target"`
}

// ListBrokenLinks inspects direct project entries without changing links or
// following them into another machine's checkout.
func ListBrokenLinks(campaignRoot string) ([]BrokenProjectLink, error) {
	projectsDir := filepath.Join(campaignRoot, "projects")
	entries, err := os.ReadDir(projectsDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, camperrors.Wrapf(err, "list project links in %s", projectsDir)
	}
	var broken []BrokenProjectLink
	for _, entry := range entries {
		isLink, err := dirlink.DirEntryIsDirectoryLink(entry, filepath.Join(projectsDir, entry.Name()))
		if err != nil {
			return broken, camperrors.Wrapf(err, "inspect project link %s", entry.Name())
		}
		if !isLink {
			continue
		}
		path := filepath.Join(projectsDir, entry.Name())
		target, err := dirlink.ReadTarget(path)
		if err != nil {
			return broken, camperrors.Wrapf(err, "read project link %s", path)
		}
		if _, err := os.Stat(path); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return broken, camperrors.Wrapf(err, "inspect project link %s", path)
		}
		broken = append(broken, BrokenProjectLink{Path: filepath.ToSlash(filepath.Join("projects", entry.Name())), Target: target})
	}
	return broken, nil
}
