// Package dirlink implements camp-managed directory links (Unix symlinks and
// Windows directory symlinks or junctions) shared by project links and skill
// projections.
package dirlink

import (
	"os"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// Kind classifies a directory link created or recognized by camp.
type Kind int

const (
	KindNone Kind = iota
	KindSymlink
	KindJunction
)

// Windows reparse tags for directory symlinks and junctions (IO_REPARSE_TAG_*).
const (
	ReparseTagSymlink    uint32 = 0xA000000C
	ReparseTagMountPoint uint32 = 0xA0000003
)

// KnownDirectoryLinkTag reports whether tag is a directory symlink or junction.
func KnownDirectoryLinkTag(tag uint32) bool {
	switch tag {
	case ReparseTagSymlink, ReparseTagMountPoint:
		return true
	default:
		return false
	}
}

// Stat describes a path that may be a camp directory link.
type Stat struct {
	Kind       Kind
	ReparseTag uint32
}

// IsDirectoryLink reports whether s describes a directory link.
func (s Stat) IsDirectoryLink() bool {
	return s.Kind != KindNone
}

// CreateOptions configures Create.
type CreateOptions struct {
	// LinkPath is the new link entry to create.
	LinkPath string
	// TargetPath is the resolved absolute directory camp links to.
	TargetPath string
	// StoreTarget is the target string stored in a symlink (relative or absolute).
	// When empty, TargetPath is used. Ignored for Windows junctions, which always
	// store an absolute substitute name; os.Readlink still returns a usable path.
	StoreTarget string
}

// Lstat inspects path without following it and reports whether it is a camp
// directory link (Unix symlink, Windows directory symlink, or junction).
func Lstat(path string) (Stat, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Stat{}, camperrors.Wrap(err, "lstat directory link")
	}
	return statFromLstat(path, info)
}

// IsDirectoryLink reports whether path is a directory link.
func IsDirectoryLink(path string) (bool, error) {
	st, err := Lstat(path)
	if err != nil {
		return false, err
	}
	return st.IsDirectoryLink(), nil
}

// ReadTarget returns the link target without following the link. It uses
// os.Readlink, which on Windows supports directory symlinks and junctions.
func ReadTarget(path string) (string, error) {
	target, err := os.Readlink(path)
	if err != nil {
		return "", camperrors.Wrap(err, "read directory link target")
	}
	return target, nil
}

// Create makes a directory link at LinkPath pointing at TargetPath.
func Create(opts CreateOptions) error {
	if opts.LinkPath == "" || opts.TargetPath == "" {
		return camperrors.Wrap(camperrors.ErrInvalidInput, "directory link paths are required")
	}
	return createPlatform(opts)
}

// Remove deletes only the link entry at path, never the link target tree.
func Remove(path string) error {
	st, err := Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !st.IsDirectoryLink() {
		return camperrors.Wrapf(camperrors.ErrInvalidInput, "refuse to remove non-link directory entry %s", path)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return camperrors.Wrap(err, "remove directory link")
	}
	return nil
}
