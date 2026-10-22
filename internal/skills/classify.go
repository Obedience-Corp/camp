package skills

import (
	"bytes"
	"context"
	"errors"
	"io/fs"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// ShippedState classifies a shipped skill file, or a bundle, on disk against
// the content this camp version ships and the manifest of what camp wrote.
type ShippedState string

const (
	// ShippedCurrent means the file is byte-identical to the shipped content.
	ShippedCurrent ShippedState = "current"
	// ShippedOutdated means the file still holds what camp last wrote, from an
	// older version, so replacing it loses nothing.
	ShippedOutdated ShippedState = "outdated"
	// ShippedModified means the file was changed after camp wrote it.
	ShippedModified ShippedState = "modified"
	// ShippedUnknown means the file differs and the manifest has no record of
	// what camp wrote, so a local edit cannot be ruled out.
	ShippedUnknown ShippedState = "unknown"
	// ShippedMissing means the file does not exist.
	ShippedMissing ShippedState = "missing"
)

// Preserved reports whether an update without --force leaves the file alone.
func (s ShippedState) Preserved() bool {
	return s == ShippedModified || s == ShippedUnknown
}

func (s ShippedState) rank() int {
	switch s {
	case ShippedModified:
		return 4
	case ShippedUnknown:
		return 3
	case ShippedOutdated:
		return 2
	case ShippedMissing:
		return 1
	default:
		return 0
	}
}

// FileStatus is one shipped file and its classification.
type FileStatus struct {
	ShippedFile
	State ShippedState
}

// BundleStatus is the aggregate state of one shipped bundle.
type BundleStatus struct {
	Slug  string
	State ShippedState
}

// Inventory classifies every shipped skill file of a camp.
type Inventory struct {
	Files []FileStatus
	// Manifest is the recorded manifest, nil when the camp has none.
	Manifest *Manifest
}

// Classify compares each shipped file with its copy in skillsFS, rooted at
// .campaign/skills/, and with the manifest stored there. Files camp does not
// ship are never read.
func Classify(ctx context.Context, skillsFS fs.FS, shipped []ShippedFile) (*Inventory, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	manifest, err := ReadManifest(skillsFS)
	if err != nil {
		return nil, err
	}
	inv := &Inventory{Manifest: manifest, Files: make([]FileStatus, 0, len(shipped))}
	for _, f := range shipped {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		state, err := classifyFile(skillsFS, manifest, f)
		if err != nil {
			return nil, err
		}
		inv.Files = append(inv.Files, FileStatus{ShippedFile: f, State: state})
	}
	return inv, nil
}

func classifyFile(skillsFS fs.FS, manifest *Manifest, f ShippedFile) (ShippedState, error) {
	disk, err := fs.ReadFile(skillsFS, f.Bundle+"/"+f.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return ShippedMissing, nil
	}
	if err != nil {
		return "", camperrors.Wrapf(err, "read %s", f.RelPath())
	}
	if bytes.Equal(disk, f.Content) {
		return ShippedCurrent, nil
	}
	recorded, ok := manifest.recorded(f.Bundle, f.Path)
	switch {
	case !ok:
		return ShippedUnknown, nil
	case recorded == HashContent(disk):
		return ShippedOutdated, nil
	default:
		return ShippedModified, nil
	}
}

// Bundles aggregates file states per bundle in first-seen order. A preserved state
// outranks outdated, outdated outranks missing, and missing outranks current.
func (inv *Inventory) Bundles() []BundleStatus {
	var bundles []BundleStatus
	index := make(map[string]int)
	for _, f := range inv.Files {
		i, seen := index[f.Bundle]
		if !seen {
			index[f.Bundle] = len(bundles)
			bundles = append(bundles, BundleStatus{Slug: f.Bundle, State: f.State})
			continue
		}
		if f.State.rank() > bundles[i].State.rank() {
			bundles[i].State = f.State
		}
	}
	return bundles
}

// NextManifest returns the manifest to record once every file in written holds
// its shipped content: current and written files record the shipped hash and
// every other file keeps its previous record, if any.
func (inv *Inventory) NextManifest(campVersion string, written map[string]bool) *Manifest {
	next := &Manifest{SchemaVersion: ManifestSchemaVersion, CampVersion: campVersion}
	for _, f := range inv.Files {
		if f.State == ShippedCurrent || written[f.RelPath()] {
			next.set(f.Bundle, f.Path, f.Hash())
			continue
		}
		if hash, ok := inv.Manifest.recorded(f.Bundle, f.Path); ok {
			next.set(f.Bundle, f.Path, hash)
		}
	}
	return next
}

// ManifestStale reports whether an update without --force would record
// different hashes than the stored manifest, or whether there is none.
func (inv *Inventory) ManifestStale() bool {
	written := make(map[string]bool)
	for _, f := range inv.Files {
		if f.State == ShippedMissing || f.State == ShippedOutdated {
			written[f.RelPath()] = true
		}
	}
	return inv.Manifest == nil || !inv.Manifest.sameRecords(inv.NextManifest("", written))
}
