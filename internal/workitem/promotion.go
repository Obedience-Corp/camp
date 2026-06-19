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

	if err := insertScalarAfter(&doc, "type", "promoted_to", promotedTo); err != nil {
		return err
	}
	if err := insertScalarAfter(&doc, "promoted_to", "promoted_at", at.UTC().Format(time.RFC3339)); err != nil {
		return err
	}

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return camperrors.Wrap(err, "marshal updated workitem")
	}
	return fsutil.WriteFileAtomically(abs, out, 0o644)
}
