package skills

import (
	"context"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/Obedience-Corp/camp/internal/campaign"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// ErrDeclineEdited refuses to decline a plugin bundle with files camp did not
// write, unless forced.
var ErrDeclineEdited = camperrors.New("the bundle holds changes camp did not write")

// ErrDeclineShape refuses to decline a plugin bundle that is not a plain
// folder of regular files: a symlink, a file, or a folder holding a symlink or
// special file. camp never removes through a symlink. The error says what is
// there.
var ErrDeclineShape = camperrors.New("camp only removes a plain folder of files")

// declineStep, when set, runs after each step of Decline and can stop it
// there. Tests use it to cut a decline short at every point.
var declineStep func(step string) error

func afterDeclineStep(step string) error {
	if declineStep == nil {
		return nil
	}
	return declineStep(step)
}

// DeclineOptions configures Decline.
type DeclineOptions struct {
	Root string
	Slug string
	// Plugin names the plugin that offers the bundle.
	Plugin string
	// Force removes a bundle with local changes after securing an undo.
	Force bool
	// WorktreesRoot is where project worktrees live; empty skips them.
	WorktreesRoot string
	// CampVersion is recorded when the camp has no manifest yet.
	CampVersion string
	// Now stamps backup directories; nil means time.Now.
	Now func() time.Time
}

// DeclineResult reports what Decline did.
type DeclineResult struct {
	// Removed is set when the bundle directory existed and was removed.
	Removed bool
	// LinksRemoved lists camp-root-relative links that were removed.
	LinksRemoved []string
	// Restore is the undo for local changes a forced decline removed.
	Restore *Undo
	// Version is the plugin version recorded for the bundle, if any.
	Version         string
	ManifestWritten bool
}

// Decline removes a plugin bundle and its projected links and records in the
// manifest that this camp declined it, so no automatic request adds it again.
// A bundle with files camp did not write is refused unless Force, which first
// secures an undo. A symlinked bundle, or one holding a symlink or special
// file, is always refused. Declining a bundle that is not there only records
// the decline.
func Decline(ctx context.Context, opts DeclineOptions) (*DeclineResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !ValidPluginBundleName(opts.Slug) {
		return nil, camperrors.Newf("%q is not a plugin skill bundle name", opts.Slug)
	}
	skillsDir := filepath.Join(opts.Root, campaign.CampaignDir, SkillsSubdir)
	manifest, err := ReadManifest(os.DirFS(skillsDir))
	if err != nil {
		return nil, err
	}
	files, err := declinedFiles(skillsDir, opts.Slug)
	if err != nil {
		return nil, err
	}
	res := &DeclineResult{Version: manifest.providers()[opts.Slug].Version}
	if edited(skillsDir, manifest, opts.Slug, files) {
		if !opts.Force {
			return nil, ErrDeclineEdited
		}
		if res.Restore, err = secureDeclineUndo(ctx, opts, skillsDir, files); err != nil {
			return nil, err
		}
	}
	// The decline is recorded before anything is removed, keeping the bundle's
	// hashes, so a decline cut short leaves a declined bundle that an automatic
	// request skips and a second decline still recognises as camp's.
	recorded, wrote, err := recordDecline(skillsDir, manifest, opts, res.Version, true)
	if err == nil {
		err = afterDeclineStep("recorded")
	}
	if err != nil {
		return res, err
	}
	if err := removeDeclined(opts, skillsDir, files != nil, res); err != nil {
		return res, err
	}
	_, dropped, err := recordDecline(skillsDir, recorded, opts, res.Version, false)
	res.ManifestWritten = wrote || dropped
	return res, err
}

// removeDeclined removes the bundle's projected links, then its folder.
func removeDeclined(opts DeclineOptions, skillsDir string, present bool, res *DeclineResult) error {
	var err error
	if res.LinksRemoved, err = removeBundleLinks(opts.Root, skillsDir, opts.WorktreesRoot, opts.Slug); err == nil {
		err = afterDeclineStep("links")
	}
	if err != nil || !present {
		return err
	}
	if err := os.RemoveAll(filepath.Join(skillsDir, opts.Slug)); err != nil {
		return camperrors.Wrapf(err, "remove %s%s", shippedSkillsPrefix, opts.Slug)
	}
	res.Removed = true
	return afterDeclineStep("folder")
}

// declinedFiles lists the regular files of the bundle, relative to it. It is
// nil when the bundle directory does not exist.
func declinedFiles(skillsDir, slug string) ([]string, error) {
	rel := shippedSkillsPrefix + slug
	info, err := os.Lstat(filepath.Join(skillsDir, slug))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil
	case err != nil:
		return nil, camperrors.WrapJoinf(ErrDeclineShape, err, "%s cannot be inspected", rel)
	case info.Mode()&fs.ModeSymlink != 0:
		return nil, camperrors.Wrapf(ErrDeclineShape, "%s is a symlink", rel)
	case info.Mode().IsRegular():
		return nil, camperrors.Wrapf(ErrDeclineShape, "%s is a file, not a folder", rel)
	case !info.IsDir():
		return nil, camperrors.Wrapf(ErrDeclineShape, "%s is a special file, not a folder", rel)
	}
	files := []string{}
	err = fs.WalkDir(os.DirFS(filepath.Join(skillsDir, slug)), ".", func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return camperrors.WrapJoinf(ErrDeclineShape, err, "%s/%s cannot be read", rel, p)
		case p == pluginSkillFile && d.IsDir():
			return camperrors.Wrapf(ErrDeclineShape, "%s/%s is a folder, not a file", rel, p)
		case d.IsDir():
			return nil
		case d.Type()&fs.ModeSymlink != 0:
			return camperrors.Wrapf(ErrDeclineShape, "%s holds a symlink at %s", rel, p)
		case !d.Type().IsRegular():
			return camperrors.Wrapf(ErrDeclineShape, "%s holds a special file at %s", rel, p)
		}
		files = append(files, p)
		return nil
	})
	return files, err
}

// edited reports whether any file differs from what the manifest records camp
// wrote, including files camp never wrote. A file that cannot be read counts
// as edited, so a forced decline backs it up first.
func edited(skillsDir string, manifest *Manifest, slug string, files []string) bool {
	for _, rel := range files {
		recorded, ok := manifest.recorded(slug, rel)
		if !ok {
			return true
		}
		data, err := os.ReadFile(filepath.Join(skillsDir, slug, filepath.FromSlash(rel)))
		if err != nil || HashContent(data) != recorded {
			return true
		}
	}
	return false
}

func secureDeclineUndo(ctx context.Context, opts DeclineOptions, skillsDir string, files []string) (*Undo, error) {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	outcomes := make([]FileOutcome, 0, len(files))
	for _, rel := range files {
		outcomes = append(outcomes, FileOutcome{
			FileStatus: FileStatus{ShippedFile: ShippedFile{Bundle: opts.Slug, Path: rel}},
			Action:     ActionReplace,
		})
	}
	undos, err := secureUndo(ctx, opts.Root, skillsDir, now().UTC(), outcomes)
	if err != nil || len(undos) == 0 {
		return nil, err
	}
	undo := undos[0]
	if undo.BackupDir != "" {
		bundleRel := shippedSkillsPrefix + opts.Slug
		undo.Command = "mkdir -p " + bundleRel + " && cp -R " + undo.BackupDir + "/. " + bundleRel + "/"
	}
	return &undo, nil
}

// removeBundleLinks removes the links camp projected for slug from the camp
// root tool directories and from every project worktree, skipping any folder
// that resolves outside the camp.
func removeBundleLinks(root, skillsDir, worktreesRoot, slug string) ([]string, error) {
	dirs := make([]string, 0, len(toolPaths))
	for _, tool := range ToolNames() {
		dirs = append(dirs, filepath.Join(root, toolPaths[tool]))
	}
	if worktreesRoot != "" {
		wts, err := ListWorktreeRoots(worktreesRoot)
		if err != nil {
			return nil, err
		}
		for _, wt := range wts {
			for _, rel := range append(slices.Clone(WorktreeSkillDestRels), GrokSkillsRel) {
				dirs = append(dirs, filepath.Join(wt, rel))
			}
		}
	}
	var removed []string
	for _, dir := range dirs {
		// A tool folder, or a worktree, that resolves outside the camp is left
		// alone, as link and unlink do: decline only removes inside the camp.
		if kind, err := CheckPathType(dir); err != nil || kind != TypeDirectory || ValidateDestination(dir, root) != nil {
			continue
		}
		n, err := RemoveProjectedSkillEntries(dir, skillsDir, []string{slug}, false)
		if err != nil {
			return removed, err
		}
		if n > 0 {
			rel, _ := filepath.Rel(root, filepath.Join(dir, slug))
			removed = append(removed, filepath.ToSlash(rel))
		}
	}
	return removed, nil
}

// recordDecline records slug as declined, keeping its file hashes when
// keepHashes is set and dropping them otherwise. It returns the manifest now
// on disk and whether it wrote one.
func recordDecline(skillsDir string, manifest *Manifest, opts DeclineOptions, version string, keepHashes bool) (*Manifest, bool, error) {
	next := &Manifest{SchemaVersion: ManifestSchemaVersion, CampVersion: opts.CampVersion}
	if manifest != nil {
		next.CampVersion = manifest.CampVersion
		next.Bundles = maps.Clone(manifest.Bundles)
		next.Providers = maps.Clone(manifest.Providers)
	}
	if !keepHashes {
		delete(next.Bundles, opts.Slug)
	}
	next.setProvider(opts.Slug, ProviderRecord{Plugin: opts.Plugin, Version: version, Declined: true})
	if manifest != nil && manifest.sameRecords(next) {
		return manifest, false, nil
	}
	return next, true, writeManifest(skillsDir, next)
}
