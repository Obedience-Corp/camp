// Package spelling centralizes resolution of the two on-disk spellings a
// dungeon directory can have: the visible "dungeon" and the hidden
// ".dungeon". Every camp code path that locates or creates a dungeon
// directory should go through this package instead of hardcoding either
// spelling.
//
// A campaign uses exactly one spelling. "dungeon_hidden" decides which one a
// brand-new campaign is scaffolded with; from then on the campaign's own
// layout decides, and "camp dungeon migrate" converts a legacy campaign to
// the hidden spelling in one sweep. Both spellings under the same parent is a
// broken state rather than a supported one: whichever spelling lost would be
// invisible to every listing, so Resolve reports a ConflictError instead of
// picking one.
package spelling

import (
	"context"
	"fmt"
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

	// MigrateCommand converts a campaign to the hidden spelling. It is named
	// in ConflictError so the error tells the user how to get unstuck.
	MigrateCommand = "camp dungeon migrate"
)

// ConflictError reports that both dungeon spellings exist under Parent.
//
// Resolving this to either spelling would hide whatever is filed under the
// other one, so camp refuses to guess and asks for the two to be reconciled.
type ConflictError struct {
	// Parent is the directory holding both "dungeon" and ".dungeon".
	Parent string
}

// Error implements the error interface.
func (e *ConflictError) Error() string {
	return fmt.Sprintf(
		"both %s/ and %s/ exist under %s: a campaign uses exactly one dungeon spelling, "+
			"so camp cannot tell which one holds your work and resolving either would hide the other. "+
			"Move the contents of one into the other and delete the empty directory, then run %q "+
			"to convert the campaign to %s/.",
		Visible, Hidden, e.Parent, MigrateCommand, Hidden,
	)
}

// Is reports whether e matches target. A ConflictError matches ErrConflict, or
// another *ConflictError with the same Parent.
func (e *ConflictError) Is(target error) bool {
	if target == camperrors.ErrConflict {
		return true
	}
	t, ok := target.(*ConflictError)
	if !ok {
		return false
	}
	return t.Parent == "" || e.Parent == t.Parent
}

// NewConflict creates a ConflictError for parent.
func NewConflict(parent string) *ConflictError { return &ConflictError{Parent: parent} }

// Resolved describes the outcome of resolving a dungeon directory name under
// a parent directory.
type Resolved struct {
	// Name is "dungeon" or ".dungeon".
	Name string
	// Path is filepath.Join(parent, Name).
	Path string
	// Exists reports whether Path was already present on disk.
	Exists bool
}

// Hidden reports whether r resolved to the hidden spelling.
func (r Resolved) Hidden() bool { return r.Name == Hidden }

// Resolve inspects parent for "dungeon" and ".dungeon" subdirectories.
//
// Exactly one present returns that spelling. Both present returns a
// ConflictError. Neither present reports Exists=false and defaults Name to
// Visible; callers about to create a dungeon should use NameForNew rather
// than relying on that fallback.
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
		return Resolved{}, NewConflict(parent)
	case visibleOK:
		return Resolved{Name: Visible, Path: visiblePath, Exists: true}, nil
	case hiddenOK:
		return Resolved{Name: Hidden, Path: hiddenPath, Exists: true}, nil
	default:
		return Resolved{Name: Visible, Path: visiblePath, Exists: false}, nil
	}
}

// NameForNew decides the directory name a brand-new dungeon under parent
// should use.
//
// A dungeon already under parent keeps its established name, so repeated
// init/repair calls stay idempotent. Otherwise campaignName wins: a new
// directory inside an existing campaign follows what that campaign already
// uses, never the dungeon_hidden setting, because a campaign that accreted
// one spelling next to the other is exactly the broken state Resolve rejects.
// Resolve campaignName with CampaignName.
func NameForNew(ctx context.Context, parent, campaignName string) (string, error) {
	if !IsDungeonName(campaignName) {
		return "", camperrors.Wrapf(camperrors.ErrInvalidInput,
			"campaign dungeon spelling %q must be %q or %q", campaignName, Visible, Hidden)
	}
	resolved, err := Resolve(ctx, parent)
	if err != nil {
		return "", err
	}
	if resolved.Exists {
		return resolved.Name, nil
	}
	return campaignName, nil
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

// NameFor returns the spelling selected by a dungeon_hidden setting value.
func NameFor(hidden bool) string {
	if hidden {
		return Hidden
	}
	return Visible
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
