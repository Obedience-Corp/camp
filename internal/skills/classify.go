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
	// ShippedDeclined means this camp declined the plugin bundle on purpose.
	ShippedDeclined ShippedState = "declined"
	// ShippedProviderMissing means the camp records the plugin bundle and its
	// plugin is not installed here or offers no skills camp reads.
	ShippedProviderMissing ShippedState = "provider-missing"
	// ShippedAhead means a newer plugin version than this machine has wrote
	// the plugin bundle, so this machine leaves it alone.
	ShippedAhead ShippedState = "ahead"
)

// Preserved reports whether an update without --force leaves the file alone.
func (s ShippedState) Preserved() bool {
	return s == ShippedModified || s == ShippedUnknown || s == ShippedSymlink
}

func (s ShippedState) rank() int {
	switch s {
	case ShippedDeclined:
		return 8
	case ShippedProviderMissing:
		return 7
	case ShippedAhead:
		return 6
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
	// Problem says why a plugin file could not be inspected, such as its
	// bundle being a file or the file being a folder. Such a file is unknown
	// and left alone, even with --force.
	Problem string
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
	// pluginDirs holds each plugin bundle the manifest records whose
	// directory exists in the camp.
	pluginDirs map[string]bool
	// plugins holds the plugin bundles classified in this run.
	plugins map[string]pluginRun
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
	inv.pluginDirs = presentPluginDirs(skillsFS, manifest)
	for _, f := range shipped {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		status, err := classifyFile(ctx, skillsFS, manifest, prov, f)
		if err != nil && f.Plugin != "" && ctx.Err() == nil {
			status, err = FileStatus{ShippedFile: f, State: ShippedUnknown, Problem: pluginProblem(skillsFS, f, err)}, nil
		}
		if err != nil {
			return nil, err
		}
		inv.Files = append(inv.Files, status)
	}
	return inv, nil
}

func classifyFile(ctx context.Context, skillsFS fs.FS, manifest *Manifest, prov Provenance, f ShippedFile) (FileStatus, error) {
	status := FileStatus{ShippedFile: f}
	if f.Plugin != "" {
		// Git history shows what camp init wrote, never what a plugin wrote.
		prov = nil
	}
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

// presentPluginDirs reports which recorded plugin bundles still have a
// directory. A directory that cannot be inspected counts as present, so its
// record is kept and an unreadable plugin bundle never fails a camp update.
// pluginProblem names what is at a plugin file's path that camp cannot read.
func pluginProblem(skillsFS fs.FS, f ShippedFile, err error) string {
	if info, statErr := fs.Lstat(skillsFS, f.Bundle); statErr == nil && !info.IsDir() {
		return shippedSkillsPrefix + f.Bundle + " is a file, not a folder"
	}
	if info, statErr := fs.Lstat(skillsFS, f.Bundle+"/"+f.Path); statErr == nil && info.IsDir() {
		return f.RelPath() + " is a folder, not a file"
	}
	cause := err
	if inner := errors.Unwrap(err); inner != nil {
		cause = inner
	}
	return f.RelPath() + " cannot be read: " + cause.Error()
}

func presentPluginDirs(skillsFS fs.FS, manifest *Manifest) map[string]bool {
	present := make(map[string]bool)
	for slug := range manifest.providers() {
		if !fs.ValidPath(slug) || strings.Contains(slug, "/") {
			continue
		}
		info, err := fs.Stat(skillsFS, slug)
		switch {
		case err == nil:
			present[slug] = info.IsDir()
		case !errors.Is(err, fs.ErrNotExist):
			present[slug] = true
		}
	}
	return present
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
// every other file keeps its previous record, if any. A plugin bundle's record
// is kept while its directory exists or it is declined, whether or not its
// plugin is installed; a bundle camp no longer ships is dropped.
func (inv *Inventory) NextManifest(campVersion string, written map[string]bool) *Manifest {
	next := &Manifest{SchemaVersion: ManifestSchemaVersion, CampVersion: campVersion}
	classified := make(map[string]bool)
	for _, f := range inv.Files {
		classified[f.Bundle] = true
		if f.State == ShippedCurrent || written[f.RelPath()] {
			next.set(f.Bundle, f.Path, f.Hash())
			continue
		}
		if hash, ok := inv.Manifest.recorded(f.Bundle, f.Path); ok {
			next.set(f.Bundle, f.Path, hash)
		}
	}
	for slug, run := range inv.plugins {
		if rec, ok := inv.pluginRecord(slug, run, written); ok {
			next.setProvider(slug, rec)
		}
	}
	inv.carryPluginRecords(next, classified)
	return next
}

// carryPluginRecords copies forward the plugin bundles this run did not
// classify, with any hashes they have.
func (inv *Inventory) carryPluginRecords(next *Manifest, classified map[string]bool) {
	for slug, rec := range inv.Manifest.providers() {
		if classified[slug] || (!rec.Declined && !inv.pluginDirs[slug]) {
			continue
		}
		next.setProvider(slug, rec)
		// A declined bundle normally has no hashes; a decline cut short keeps
		// them until its folder is gone, and they must survive a rewrite.
		for rel, hash := range inv.Manifest.Bundles[slug] {
			next.set(slug, rel, hash)
		}
	}
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
