package workitem

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// FreshWriteWindow is how recently a workitem directory must have been written
// for the sweep to treat it as a live session's working directory and leave it
// where it is.
//
// A completed workflow run says the authoring loop finished, not that every
// agent working in the directory has stopped. On 2026-08-17 the sweep moved
// workflow/explore/ollama-obey-provider to the dungeon while three agents were
// still writing into it; their later writes landed in an orphaned untracked
// directory at the old path and recovery took a git mv plus a new workflow run.
// Ten minutes is long enough to cover the gap between two writes in an active
// session and short enough that finished work is not held back for a whole
// working session.
const FreshWriteWindow = 10 * time.Minute

// IsFreshlyWritten is the guard's decision, separated from the walk that feeds
// it so the rule is testable without touching a filesystem. newest is the newest
// content modification time found under the workitem directory (zero when there
// is none) and now is the single clock reading taken for this sweep.
//
// A modification time in the future counts as fresh: it means the clock moved
// backward or the file came from another machine, and neither is evidence that
// writing has stopped.
func IsFreshlyWritten(newest, now time.Time) bool {
	if newest.IsZero() {
		return false
	}
	if newest.After(now) {
		return true
	}
	return now.Sub(newest) < FreshWriteWindow
}

// freshWriteSkipDirs are directory names the freshness walk does not descend
// into.
//
// .workflow/ has to be excluded or the guard would block everything it is meant
// to protect: the last thing a completed run writes is its own run record under
// .workflow/, and that write is the very evidence that made the workitem
// eligible. Counting it would make every candidate look freshly written forever
// and the guard would be a permanent block rather than a safety net. What the
// guard is actually looking for is the authored content beside it.
var freshWriteSkipDirs = map[string]bool{
	".workflow": true,
	".git":      true,
}

// NewestContentModTime returns the newest modification time among the content
// files under dir, ignoring the run bookkeeping in .workflow/ and any nested
// git metadata. It returns the zero time when dir holds no content files.
//
// Walk errors on individual entries are skipped rather than failed: an
// unreadable file is not a reason to refuse to sweep a workitem, and the guard
// is conservative in the other direction already (anything it does read that
// looks recent stops the move).
func NewestContentModTime(ctx context.Context, dir string) (time.Time, error) {
	if err := ctx.Err(); err != nil {
		return time.Time{}, err
	}

	info, err := os.Stat(dir)
	if err != nil {
		return time.Time{}, camperrors.Wrapf(err, "stat workitem directory %s", dir)
	}
	if !info.IsDir() {
		return info.ModTime(), nil
	}

	var newest time.Time
	walkErr := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if err != nil {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if p != dir && freshWriteSkipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		fi, statErr := d.Info()
		if statErr != nil {
			return nil
		}
		if fi.ModTime().After(newest) {
			newest = fi.ModTime()
		}
		return nil
	})
	if walkErr != nil {
		if ctx.Err() != nil {
			return time.Time{}, walkErr
		}
		return time.Time{}, camperrors.Wrapf(walkErr, "scanning %s for recent writes", dir)
	}
	return newest, nil
}

// FreshWriteDetail renders the skip reason's human half: how long ago the
// directory was last written, so the report says why it was left alone rather
// than only that it was.
func FreshWriteDetail(newest, now time.Time) string {
	age := now.Sub(newest)
	if age < 0 {
		return "last written in the future (clock skew); a session may still be writing here"
	}
	mins := int(age.Round(time.Minute) / time.Minute)
	unit := "minutes"
	if mins == 1 {
		unit = "minute"
	}
	return fmt.Sprintf("last written %d %s ago; a session may still be writing here", mins, unit)
}
