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

// FreshWriteWindow: a workitem directory written inside this window is treated
// as a live session's working directory and is never moved.
const FreshWriteWindow = 10 * time.Minute

// IsFreshlyWritten decides the guard from the newest content mtime (zero when
// there is none) and one clock reading. A future mtime counts as fresh.
func IsFreshlyWritten(newest, now time.Time) bool {
	if newest.IsZero() {
		return false
	}
	if newest.After(now) {
		return true
	}
	return now.Sub(newest) < FreshWriteWindow
}

// freshWriteSkipDirs are excluded from the freshness walk. .workflow/ must be:
// a completed run's own record is both the newest write and the evidence that
// made the item eligible, so counting it would make every candidate fresh.
var freshWriteSkipDirs = map[string]bool{
	".workflow": true,
	".git":      true,
}

// NewestContentModTime returns the newest mtime among dir's content files,
// skipping freshWriteSkipDirs, or the zero time when there are none. Unreadable
// entries are skipped rather than failing the scan.
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

// FreshWriteDetail renders how long ago the directory was last written.
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
