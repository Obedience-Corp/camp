package statusmove

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/pathutil"
)

const dateLayout = "2006-01-02"

// ErrAlreadyExists is returned when the destination path or logical dated item
// already exists.
var ErrAlreadyExists = camperrors.Wrap(camperrors.ErrAlreadyExists, "statusmove: destination already exists")

// MoveOptions configures a status-directory move.
type MoveOptions struct {
	// DatedBucket places the destination inside dst/YYYY-MM-DD/<base(src)>.
	DatedBucket bool
	// BoundaryRoot, if non-empty, validates that the final destination remains under it.
	BoundaryRoot string
	// Now overrides the current time for dated bucket naming.
	Now *time.Time
}

// Move moves src to dst with no-replace semantics and returns the final path.
// If DatedBucket is true, dst is treated as the status root and the final path
// is dst/YYYY-MM-DD/base(src). Cross-device moves fall back to copy-then-delete.
func Move(ctx context.Context, src, dst string, opts MoveOptions) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", camperrors.Wrap(err, "context cancelled")
	}

	if _, err := os.Lstat(src); err != nil {
		if os.IsNotExist(err) {
			return "", camperrors.WrapJoin(camperrors.ErrNotFound, err, "stat source "+src)
		}
		return "", camperrors.Wrapf(err, "stat source %s", src)
	}

	finalDst := dst
	if opts.DatedBucket {
		itemName := filepath.Base(src)
		if existing, exists, err := existingDatedItemPath(dst, itemName); err != nil {
			return "", camperrors.Wrapf(err, "checking destination %s", dst)
		} else if exists {
			return "", camperrors.Wrapf(ErrAlreadyExists, "destination already exists: %s", existing)
		}

		now := time.Now()
		if opts.Now != nil {
			now = *opts.Now
		}
		finalDst = filepath.Join(dst, now.Format(dateLayout), itemName)
	}

	if opts.BoundaryRoot != "" {
		if err := pathutil.ValidateBoundary(opts.BoundaryRoot, finalDst); err != nil {
			return "", camperrors.Wrapf(err, "move destination %s outside boundary", finalDst)
		}
	}

	if err := os.MkdirAll(filepath.Dir(finalDst), 0755); err != nil {
		return "", camperrors.Wrapf(err, "create destination directory for %s", finalDst)
	}

	if err := noReplaceMove(src, finalDst); err != nil {
		return "", err
	}
	return finalDst, nil
}

func existingDatedItemPath(statusRoot, itemName string) (string, bool, error) {
	legacyPath := filepath.Join(statusRoot, itemName)
	if _, err := os.Stat(legacyPath); err == nil {
		return legacyPath, true, nil
	} else if err != nil && !os.IsNotExist(err) {
		return "", false, err
	}

	entries, err := os.ReadDir(statusRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}

	var datedDirs []string
	for _, entry := range entries {
		if entry.IsDir() && isDateDir(entry.Name()) {
			datedDirs = append(datedDirs, entry.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(datedDirs)))

	for _, dirName := range datedDirs {
		candidate := filepath.Join(statusRoot, dirName, itemName)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, true, nil
		} else if err != nil && !os.IsNotExist(err) {
			return "", false, err
		}
	}
	return "", false, nil
}

func isDateDir(name string) bool {
	if len(name) != len(dateLayout) {
		return false
	}
	if name[4] != '-' || name[7] != '-' {
		return false
	}
	for i, r := range name {
		if i == 4 || i == 7 {
			continue
		}
		if !strings.ContainsRune("0123456789", r) {
			return false
		}
	}
	return true
}
