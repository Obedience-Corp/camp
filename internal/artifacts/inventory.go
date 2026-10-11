package artifacts

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// InventoryFile is a local, non-git file in a declared artifact root.
type InventoryFile struct {
	Path     string    `json:"path"`
	Root     string    `json:"root"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	Symlink  bool      `json:"symlink,omitempty"`
}

// Inventory asks git for all non-tracked files, including ignored files, in
// declared roots. It reads metadata, never media contents or hashes. Overlapping
// roots assign each file to its deepest declaration, once.
func Inventory(ctx context.Context, campRoot string) ([]InventoryFile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cfg, err := Load(campRoot)
	if err != nil {
		return nil, err
	}
	roots := make([]string, 0, len(cfg.Roots))
	for _, root := range cfg.Roots {
		rel, err := EnsureRootWithin(campRoot, root.Path)
		if err != nil {
			return nil, err
		}
		roots = append(roots, rel)
	}
	files := make([]InventoryFile, 0)
	seen := make(map[string]bool)
	for _, root := range roots {
		// --others without --exclude-standard includes ignored files too.
		paths, err := lsFiles(ctx, campRoot, ":(literal)"+root, []string{"--others"})
		if err != nil {
			return nil, err
		}
		for _, path := range paths {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if seen[path] {
				continue
			}
			seen[path] = true
			owner, ok := deepestRoot(roots, path)
			if !ok || !filepath.IsLocal(filepath.FromSlash(path)) {
				continue
			}
			info, err := os.Lstat(filepath.Join(campRoot, filepath.FromSlash(path)))
			if os.IsNotExist(err) {
				continue
			} // removed during the scan
			if err != nil {
				return nil, camperrors.Wrapf(err, "stat artifact %s", path)
			}
			link := info.Mode()&os.ModeSymlink != 0
			if !info.Mode().IsRegular() && !link {
				continue
			}
			files = append(files, InventoryFile{Path: path, Root: owner, Size: info.Size(), Modified: info.ModTime(), Symlink: link})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}
