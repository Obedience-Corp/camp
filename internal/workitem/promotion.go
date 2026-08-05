package workitem

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
)

func RecordPromotion(ctx context.Context, root, relPath, promotedTo string, at time.Time) error {
	return recordLifecycleFields(ctx, root, relPath, []FrontmatterField{
		{After: "type", Key: "promoted_to", Value: promotedTo},
		{After: "promoted_to", Key: "promoted_at", Value: at.UTC().Format(time.RFC3339)},
	})
}

// recordLifecycleFields stamps scalar lifecycle keys onto a workitem, choosing
// the right surface by shape: a directory workitem's .workitem marker, or a file
// workitem's own frontmatter. Existing keys are updated in place so the
// operation is idempotent. Both paths hold a per-file lock and write atomically.
func recordLifecycleFields(ctx context.Context, root, relPath string, fields []FrontmatterField) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	target := filepath.Join(root, filepath.FromSlash(relPath))
	info, err := os.Stat(target)
	if err != nil {
		return camperrors.Wrapf(err, "stat %s", relPath)
	}
	if !info.IsDir() {
		return StampFrontmatterFields(ctx, target, fields)
	}

	abs := filepath.Join(target, MetadataFilename)
	release, err := fsutil.AcquireFileLock(ctx, abs+".lock")
	if err != nil {
		return err
	}
	defer release()

	raw, err := os.ReadFile(abs)
	if err != nil {
		return camperrors.Wrapf(err, "read %s", abs)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return camperrors.Wrapf(err, "parse %s", abs)
	}
	for _, f := range fields {
		// Node-shaped rather than scalar-only: split stamps a list
		// (split_into) onto the parent's marker, and a parallel writer for
		// that one key would mean two pieces of code deciding how camp's
		// frontmatter is edited. FrontmatterField.node() already picks the
		// right shape, which is what the file-backed path has always used.
		if err := insertNodeAfter(&doc, f.After, f.Key, f.node()); err != nil {
			return err
		}
	}
	out, err := yaml.Marshal(&doc)
	if err != nil {
		return camperrors.Wrap(err, "marshal updated workitem")
	}
	return fsutil.WriteFileAtomically(abs, out, 0o644)
}
