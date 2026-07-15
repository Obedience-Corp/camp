// Package spelling centralizes resolution of the two on-disk spellings a
// dungeon directory can have: the visible "dungeon" and the hidden
// ".dungeon". Every camp code path that locates or creates a dungeon
// directory should go through this package instead of hardcoding either
// spelling, so existing campaigns (visible) and new campaigns (hidden by
// default) are both handled uniformly.
package spelling

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

const (
	// Visible is the legacy, un-hidden dungeon directory name.
	Visible = "dungeon"
	// Hidden is the dotfile dungeon directory name used by new campaigns.
	Hidden = ".dungeon"
)

// Resolved describes the outcome of resolving a dungeon directory name under
// a parent directory.
type Resolved struct {
	// Name is "dungeon" or ".dungeon".
	Name string
	// Path is filepath.Join(parent, Name).
	Path string
	// Exists reports whether Path was already present on disk.
	Exists bool
	// Warning is non-empty only when both spellings exist under parent.
	Warning string
}

// Resolve inspects parent for "dungeon" and ".dungeon" subdirectories.
//
// If both exist, the visible spelling wins and Warning explains the
// conflict. If exactly one exists, that spelling is returned. If neither
// exists, Resolve reports Exists=false and defaults Name to Visible; callers
// that are about to create a new dungeon directory should decide the
// spelling with NameForNew instead of relying on this fallback.
func Resolve(ctx context.Context, parent string) (Resolved, error) {
	if err := ctx.Err(); err != nil {
		return Resolved{}, camperrors.Wrap(err, "context cancelled")
	}

	visiblePath := filepath.Join(parent, Visible)
	hiddenPath := filepath.Join(parent, Hidden)

	visibleOK, err := isDir(visiblePath)
	if err != nil {
		return Resolved{}, camperrors.Wrapf(err, "stat %s", visiblePath)
	}
	hiddenOK, err := isDir(hiddenPath)
	if err != nil {
		return Resolved{}, camperrors.Wrapf(err, "stat %s", hiddenPath)
	}

	switch {
	case visibleOK && hiddenOK:
		return Resolved{
			Name:   Visible,
			Path:   visiblePath,
			Exists: true,
			Warning: fmt.Sprintf(
				"both %s and %s exist under %s; using %s",
				Visible, Hidden, parent, Visible,
			),
		}, nil
	case visibleOK:
		return Resolved{Name: Visible, Path: visiblePath, Exists: true}, nil
	case hiddenOK:
		return Resolved{Name: Hidden, Path: hiddenPath, Exists: true}, nil
	default:
		return Resolved{Name: Visible, Path: visiblePath, Exists: false}, nil
	}
}

// NameForNew decides the directory name a brand-new dungeon under parent
// should use. If a dungeon already exists under parent (either spelling),
// its established name wins so repeated init/repair calls stay idempotent
// and never create a second copy alongside it. Otherwise hidden selects
// between the hidden and visible spelling.
func NameForNew(ctx context.Context, parent string, hidden bool) (string, error) {
	resolved, err := Resolve(ctx, parent)
	if err != nil {
		return "", err
	}
	if resolved.Exists {
		return resolved.Name, nil
	}
	if hidden {
		return Hidden, nil
	}
	return Visible, nil
}

// RewriteRel rewrites the leading "dungeon" path segment of rel (e.g.
// "dungeon/done" or "dungeon") to dungeonName. rel values that don't start
// with the visible spelling are returned unchanged.
func RewriteRel(rel, dungeonName string) string {
	if rel == Visible {
		return dungeonName
	}
	if rest, ok := strings.CutPrefix(rel, Visible+"/"); ok {
		return filepath.Join(dungeonName, rest)
	}
	return rel
}

// IsDungeonName reports whether name is either recognized dungeon spelling.
func IsDungeonName(name string) bool {
	return name == Visible || name == Hidden
}

// WarnIfConflicting writes resolved.Warning to w when both spellings exist
// under the resolved parent. It is a no-op otherwise.
func WarnIfConflicting(w io.Writer, resolved Resolved) {
	if resolved.Warning == "" {
		return
	}
	_, _ = fmt.Fprintln(w, "warning: "+resolved.Warning)
}

func isDir(path string) (bool, error) {
	info, err := os.Stat(path)
	if err == nil {
		return info.IsDir(), nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
