package intent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
)

// Rename updates an intent's title and regenerates its human-readable filename
// (<new-slug>-<stable-suffix>.md) while preserving its stable id. The file is
// moved on disk and title:/updated_at: are rewritten. Identity (id:) never
// changes, so existing references and id-based lookups survive the rename.
func (s *IntentService) Rename(ctx context.Context, id, newTitle string) (*Intent, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	newTitle = strings.TrimSpace(newTitle)
	if newTitle == "" {
		return nil, camperrors.Wrap(camperrors.ErrInvalidInput, "rename: new title is empty")
	}

	oldPath, err := s.resolveByID(id)
	if err != nil {
		// Notes live outside the lifecycle status dirs resolveByID scans; fall
		// back to the note store so renaming a note works too. The rename logic
		// below is directory-agnostic, so the note stays in notes/.
		notePath, nerr := s.resolveNoteByID(id)
		if nerr != nil {
			return nil, err
		}
		oldPath = notePath
	}
	it, err := s.loadIntent(oldPath)
	if err != nil {
		return nil, camperrors.Wrap(ErrNotFound, id)
	}

	newPath := s.uniqueRenamePath(filepath.Dir(oldPath), renameBase(it.ID, newTitle), oldPath)

	it.Title = newTitle
	it.UpdatedAt = time.Now()

	if errs := it.Validate(); len(errs) > 0 {
		return nil, intentValidationError(errs)
	}

	data, err := SerializeIntent(it)
	if err != nil {
		return nil, camperrors.Wrap(err, "serializing renamed intent")
	}

	if newPath == oldPath {
		// Slug unchanged: rewrite in place.
		if err := fsutil.WriteFileAtomically(oldPath, data, 0644); err != nil {
			return nil, camperrors.Wrap(err, "writing renamed intent")
		}
		it.Path = oldPath
		s.invalidateIDIndex()
		return it, nil
	}

	if err := fsutil.WriteFileAtomically(newPath, data, 0644); err != nil {
		return nil, camperrors.Wrap(err, "writing renamed intent")
	}
	if err := os.Remove(oldPath); err != nil {
		_ = os.Remove(newPath)
		return nil, camperrors.Wrap(err, "removing old intent file")
	}

	it.Path = newPath
	s.invalidateIDIndex()
	return it, nil
}

// renameBase builds <new-slug>-<stable-suffix> from the new title and the
// intent's stable id suffix.
func renameBase(id, newTitle string) string {
	suffix := stableSuffix(id)
	slug := SlugFromTitle(newTitle)
	switch {
	case slug == "":
		return suffix
	case suffix == "":
		return slug
	default:
		return slug + "-" + suffix
	}
}

// stableSuffix returns the timestamp portion (YYYYMMDD-HHMMSS) of an intent id,
// which is stable across renames. Ids are <slug>-YYYYMMDD-HHMMSS.
func stableSuffix(id string) string {
	parts := strings.Split(id, "-")
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "-" + parts[len(parts)-1]
	}
	return id
}

// uniqueRenamePath returns base.md in dir, appending -2, -3, ... when a
// different file already occupies the path.
func (s *IntentService) uniqueRenamePath(dir, base, oldPath string) string {
	candidate := filepath.Join(dir, base+".md")
	if candidate == oldPath {
		return candidate
	}
	if _, err := os.Stat(candidate); os.IsNotExist(err) {
		return candidate
	}
	for i := 2; ; i++ {
		c := filepath.Join(dir, fmt.Sprintf("%s-%d.md", base, i))
		if c == oldPath {
			return c
		}
		if _, err := os.Stat(c); os.IsNotExist(err) {
			return c
		}
	}
}
