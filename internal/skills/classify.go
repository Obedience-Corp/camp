package skills

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/campaign"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// ShippedState classifies a shipped skill file, or a bundle, on disk against
// the content this camp version ships and the manifest of what camp wrote.
type ShippedState string

const (
	// ShippedCurrent means the file is byte-identical to the shipped content.
	ShippedCurrent ShippedState = "current"
	// ShippedOutdated means the file still holds what camp last wrote, from an
	// older version, so replacing it loses nothing. The manifest proves it, or,
	// for a file the manifest does not record, git history does.
	ShippedOutdated ShippedState = "outdated"
	// ShippedModified means the file was changed after camp wrote it.
	ShippedModified ShippedState = "modified"
	// ShippedUnknown means the file differs and neither the manifest nor git
	// history shows what camp wrote, so a local edit cannot be ruled out.
	ShippedUnknown ShippedState = "unknown"
	// ShippedMissing means the file does not exist.
	ShippedMissing ShippedState = "missing"
	// ShippedSymlink means the file, or a directory above it inside the
	// bundle, is a symlink whose content differs; camp never writes through it.
	ShippedSymlink ShippedState = "symlink"
)

// Preserved reports whether an update without --force leaves the file alone.
func (s ShippedState) Preserved() bool {
	return s == ShippedModified || s == ShippedUnknown || s == ShippedSymlink
}

func (s ShippedState) rank() int {
	switch s {
	case ShippedModified:
		return 5
	case ShippedUnknown:
		return 4
	case ShippedSymlink:
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
	// GitEvidence marks an outdated file the manifest does not record, shown
	// unchanged since camp wrote it by git history alone.
	GitEvidence bool
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

// ClassifyCamp classifies the shipped skill files of the camp at root, with
// that camp's git history as evidence for files the manifest does not record.
func ClassifyCamp(ctx context.Context, root string, shipped []ShippedFile) (*Inventory, error) {
	skillsDir := filepath.Join(root, campaign.CampaignDir, SkillsSubdir)
	return Classify(ctx, os.DirFS(skillsDir), shipped, NewGitProvenance(root))
}

// Classify compares each shipped file with its copy in skillsFS, rooted at
// .campaign/skills/, and with the manifest stored there. A differing file the
// manifest does not record is outdated when prov shows it unchanged since camp
// wrote it, and unknown otherwise; a nil prov offers no such evidence. Files
// camp does not ship are never read.
func Classify(ctx context.Context, skillsFS fs.FS, shipped []ShippedFile, prov Provenance) (*Inventory, error) {
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
		status, err := classifyFile(ctx, skillsFS, manifest, prov, f)
		if err != nil {
			return nil, err
		}
		inv.Files = append(inv.Files, status)
	}
	return inv, nil
}

func classifyFile(ctx context.Context, skillsFS fs.FS, manifest *Manifest, prov Provenance, f ShippedFile) (FileStatus, error) {
	status := FileStatus{ShippedFile: f}
	name := f.Bundle + "/" + f.Path
	linked, err := throughSymlink(skillsFS, name)
	if errors.Is(err, fs.ErrNotExist) {
		status.State = ShippedMissing
		return status, nil
	}
	if err != nil {
		return status, camperrors.Wrapf(err, "inspect %s", f.RelPath())
	}
	disk, err := fs.ReadFile(skillsFS, name)
	switch {
	case linked && err == nil && bytes.Equal(disk, f.Content):
		status.State = ShippedCurrent
		return status, nil
	case linked:
		status.State = ShippedSymlink
		return status, nil
	case err != nil:
		return status, camperrors.Wrapf(err, "read %s", f.RelPath())
	case bytes.Equal(disk, f.Content):
		status.State = ShippedCurrent
		return status, nil
	}
	recorded, ok := manifest.recorded(f.Bundle, f.Path)
	switch {
	case !ok:
		return classifyUnrecorded(ctx, prov, status)
	case recorded == HashContent(disk):
		status.State = ShippedOutdated
	default:
		status.State = ShippedModified
	}
	return status, nil
}

func classifyUnrecorded(ctx context.Context, prov Provenance, status FileStatus) (FileStatus, error) {
	status.State = ShippedUnknown
	if prov == nil {
		return status, nil
	}
	once, err := prov.WrittenOnce(ctx, status.RelPath())
	if err != nil {
		return status, err
	}
	if once {
		status.State, status.GitEvidence = ShippedOutdated, true
	}
	return status, nil
}

func throughSymlink(skillsFS fs.FS, name string) (bool, error) {
	parts := strings.Split(name, "/")
	for i := range parts {
		info, err := fs.Lstat(skillsFS, strings.Join(parts[:i+1], "/"))
		if err != nil {
			return false, err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return true, nil
		}
	}
	return false, nil
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
