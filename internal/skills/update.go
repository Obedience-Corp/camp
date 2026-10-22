package skills

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Obedience-Corp/camp/internal/campaign"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
)

// FileAction is what an update does, or would do, with one shipped file.
type FileAction string

const (
	// ActionAdd writes a missing file.
	ActionAdd FileAction = "add"
	// ActionUpdate replaces an outdated file.
	ActionUpdate FileAction = "update"
	// ActionReplace overwrites a modified or unknown file under --force.
	ActionReplace FileAction = "replace"
	// ActionPreserve leaves a modified, unknown, or symlinked file alone.
	ActionPreserve FileAction = "preserve"
	// ActionKeep leaves a current file alone.
	ActionKeep FileAction = "keep"
)

// Writes reports whether the action writes the shipped content.
func (a FileAction) Writes() bool {
	return a == ActionAdd || a == ActionUpdate || a == ActionReplace
}

func actionFor(state ShippedState, force bool) FileAction {
	switch {
	case state == ShippedMissing:
		return ActionAdd
	case state == ShippedOutdated:
		return ActionUpdate
	case state == ShippedSymlink:
		return ActionPreserve
	case state.Preserved() && force:
		return ActionReplace
	case state.Preserved():
		return ActionPreserve
	default:
		return ActionKeep
	}
}

// UpdateOptions configures Update.
type UpdateOptions struct {
	// Root is the camp root.
	Root string
	// Shipped is the rendered set of shipped skill files.
	Shipped []ShippedFile
	// Slugs limits the update to these bundles; empty means every shipped bundle.
	Slugs []string
	// Force replaces modified and unknown files after securing an undo.
	Force bool
	// DryRun classifies and reports without writing anything.
	DryRun bool
	// CampVersion is recorded in the manifest.
	CampVersion string
	// Now stamps backup directories; nil means time.Now.
	Now func() time.Time
	// OnWrite, when set, observes each skill file right after it is written.
	OnWrite func(FileOutcome)
}

// FileOutcome is one shipped file, its action, and whether it was written.
type FileOutcome struct {
	FileStatus
	Action  FileAction
	Written bool
}

// Undo restores the files a forced update replaced in one bundle.
type Undo struct {
	Bundle string
	// Command is the one-command undo, run from the camp root.
	Command string
	// BackupDir is the camp-root-relative copy of the replaced files, empty
	// when git restores the bundle.
	BackupDir string
}

// UpdateResult reports what Update did, or would do under DryRun.
type UpdateResult struct {
	Files           []FileOutcome
	Undo            []Undo
	ManifestWritten bool
	DryRun          bool
}

// Update brings the shipped skill bundles under Root to the shipped content:
// it writes missing and outdated files, preserves modified and unknown files
// unless Force is set, never writes through a symlink, and then records the
// manifest. Before replacing any
// modified file it secures an undo per bundle. On error, including context
// cancellation, the result lists what was already written.
func Update(ctx context.Context, opts UpdateOptions) (*UpdateResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	scope, err := scopeBundles(opts.Shipped, opts.Slugs)
	if err != nil {
		return nil, err
	}
	skillsDir := filepath.Join(opts.Root, campaign.CampaignDir, SkillsSubdir)
	inv, err := Classify(ctx, os.DirFS(skillsDir), opts.Shipped)
	if err != nil {
		return nil, err
	}

	res := &UpdateResult{DryRun: opts.DryRun}
	for _, f := range inv.Files {
		if scope[f.Bundle] {
			res.Files = append(res.Files, FileOutcome{FileStatus: f, Action: actionFor(f.State, opts.Force)})
		}
	}
	if opts.DryRun {
		return res, nil
	}

	now := opts.Now
	if now == nil {
		now = time.Now
	}
	if res.Undo, err = secureUndo(ctx, opts.Root, skillsDir, now().UTC(), res.Files); err != nil {
		return res, err
	}

	written := make(map[string]bool)
	for i := range res.Files {
		out := &res.Files[i]
		if !out.Action.Writes() {
			continue
		}
		if err := ctx.Err(); err != nil {
			return res, err
		}
		if err := writeShipped(skillsDir, out.ShippedFile); err != nil {
			return res, err
		}
		out.Written = true
		written[out.RelPath()] = true
		if opts.OnWrite != nil {
			opts.OnWrite(*out)
		}
	}
	if err := ctx.Err(); err != nil {
		return res, err
	}

	next := inv.NextManifest(opts.CampVersion, written)
	if inv.Manifest != nil && inv.Manifest.sameRecords(next) {
		return res, nil
	}
	if err := writeManifest(skillsDir, next); err != nil {
		return res, err
	}
	res.ManifestWritten = true
	return res, nil
}

func scopeBundles(shipped []ShippedFile, slugs []string) (map[string]bool, error) {
	all := ShippedSlugs(shipped)
	if len(slugs) == 0 {
		slugs = all
	}
	scope := make(map[string]bool, len(slugs))
	var unknown []string
	for _, slug := range slugs {
		if !slices.Contains(all, slug) {
			unknown = append(unknown, slug)
		}
		scope[slug] = true
	}
	if len(unknown) > 0 {
		return nil, camperrors.Newf("unknown shipped skill bundle %s; shipped bundles: %s",
			strings.Join(unknown, ", "), strings.Join(all, ", "))
	}
	return scope, nil
}

func writeShipped(skillsDir string, f ShippedFile) error {
	dest := filepath.Join(skillsDir, f.Bundle, filepath.FromSlash(f.Path))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return camperrors.Wrapf(err, "create directory for %s", f.RelPath())
	}
	if err := fsutil.WriteFileAtomically(dest, f.Content, 0o644); err != nil {
		return camperrors.Wrapf(err, "write %s", f.RelPath())
	}
	return nil
}
