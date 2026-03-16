package workitem

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// ScanDirTimestamps returns the earliest and latest file mtimes in a directory,
// using git ls-files for file enumeration with a filepath.WalkDir fallback.
// Returns (earliest, latest). If no files are found, returns the directory mtime.
func ScanDirTimestamps(ctx context.Context, dir string) (earliest, latest time.Time) {
	files, err := listTrackedFiles(ctx, dir)
	if err != nil || len(files) == 0 {
		// Fallback to directory mtime
		if info, statErr := os.Stat(dir); statErr == nil {
			return info.ModTime(), info.ModTime()
		}
		return time.Time{}, time.Time{}
	}

	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			continue
		}
		mtime := info.ModTime()
		if earliest.IsZero() || mtime.Before(earliest) {
			earliest = mtime
		}
		if latest.IsZero() || mtime.After(latest) {
			latest = mtime
		}
	}

	if earliest.IsZero() {
		if info, err := os.Stat(dir); err == nil {
			return info.ModTime(), info.ModTime()
		}
	}
	return earliest, latest
}

// listTrackedFiles returns tracked + untracked-but-not-ignored files under dir.
// Falls back to filepath.WalkDir if git is unavailable.
func listTrackedFiles(ctx context.Context, dir string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "ls-files", "-z",
		"--cached", "--others", "--exclude-standard", "--", ".")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return walkDirFallback(dir)
	}

	var files []string
	for _, part := range bytes.Split(out, []byte{0}) {
		name := string(part)
		if name == "" {
			continue
		}
		// git ls-files returns paths relative to the repo root when run with --,
		// but we ran with dir as the argument, so resolve properly
		abs := name
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(dir, abs)
		}
		if shouldIgnoreFile(abs) {
			continue
		}
		files = append(files, abs)
	}
	return files, nil
}

// walkDirFallback scans dir recursively for files when git is unavailable.
func walkDirFallback(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "dungeon" {
				return filepath.SkipDir
			}
			return nil
		}
		if shouldIgnoreFile(path) {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, camperrors.Wrapf(err, "walking directory %s", dir)
	}
	return files, nil
}

// shouldIgnoreFile returns true for files that should be excluded from timestamp scanning.
func shouldIgnoreFile(path string) bool {
	base := filepath.Base(path)
	return base == ".gitkeep" || strings.HasPrefix(base, ".")
}
