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

// RecordGather stamps gathered_into and gathered_at on the .workitem file of
// a gathered source. relPath is the source's campaign-relative location after
// the move into the gathered package. Existing keys are updated in place so
// the operation is idempotent.
func RecordGather(ctx context.Context, root, relPath, gatheredInto string, at time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	abs := filepath.Join(root, filepath.FromSlash(relPath), MetadataFilename)

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

	if err := insertScalarAfter(&doc, "type", "gathered_into", gatheredInto); err != nil {
		return err
	}
	if err := insertScalarAfter(&doc, "gathered_into", "gathered_at", at.UTC().Format(time.RFC3339)); err != nil {
		return err
	}

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return camperrors.Wrap(err, "marshal updated workitem")
	}
	return fsutil.WriteFileAtomically(abs, out, 0o644)
}
