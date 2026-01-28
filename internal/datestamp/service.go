package datestamp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	ErrNotFound      = errors.New("path not found")
	ErrAlreadyExists = errors.New("target already exists")
	ErrInvalidPath   = errors.New("invalid path")
)

// Options configures the datestamp operation.
type Options struct {
	DateFormat string // Go time format string (default: 2006-01-02)
	DaysAgo    int    // Subtract this many days from today
	UseMtime   bool   // Use file's last modified time instead of today
	DryRun     bool   // Preview only, don't execute
}

// Result contains the outcome of a datestamp operation.
type Result struct {
	OriginalPath string
	NewPath      string
	IsDirectory  bool
	DateUsed     time.Time
	Executed     bool
}

// Datestamp appends a date suffix to a file or directory name.
func Datestamp(ctx context.Context, path string, opts Options) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	// Set defaults
	if opts.DateFormat == "" {
		opts.DateFormat = "2006-01-02"
	}

	// Resolve and validate path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidPath, err)
	}

	// Clean trailing slashes
	absPath = filepath.Clean(absPath)

	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, path)
		}
		return nil, fmt.Errorf("stat failed: %w", err)
	}

	// Determine which date to use
	dateToUse := determineDate(info, opts)

	// Build new name
	newPath := buildNewPath(absPath, opts.DateFormat, dateToUse, info.IsDir())

	// Check if target exists
	if _, err := os.Stat(newPath); err == nil {
		return nil, fmt.Errorf("%w: %s", ErrAlreadyExists, newPath)
	}

	result := &Result{
		OriginalPath: absPath,
		NewPath:      newPath,
		IsDirectory:  info.IsDir(),
		DateUsed:     dateToUse,
		Executed:     false,
	}

	if opts.DryRun {
		return result, nil
	}

	// Perform rename
	if err := os.Rename(absPath, newPath); err != nil {
		return nil, fmt.Errorf("rename failed: %w", err)
	}
	result.Executed = true

	return result, nil
}

// determineDate returns the date to use based on options.
// Priority: UseMtime > DaysAgo > today
func determineDate(info os.FileInfo, opts Options) time.Time {
	if opts.UseMtime {
		return info.ModTime()
	}

	now := time.Now()
	if opts.DaysAgo > 0 {
		return now.AddDate(0, 0, -opts.DaysAgo)
	}

	return now
}

// buildNewPath constructs the new path with date suffix.
func buildNewPath(path, format string, date time.Time, isDir bool) string {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	suffix := date.Format(format)

	if isDir {
		// Directory: append suffix directly
		return filepath.Join(dir, base+"-"+suffix)
	}

	// File: insert suffix before extension
	// Handle hidden files specially - if starts with dot and no other dots,
	// treat as having no extension (e.g., .gitignore -> .gitignore-DATE)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)

	// If name is empty after trimming, the "extension" is actually the whole
	// filename (hidden file with no real extension like .gitignore)
	if name == "" || (strings.HasPrefix(base, ".") && !strings.Contains(base[1:], ".")) {
		return filepath.Join(dir, base+"-"+suffix)
	}

	return filepath.Join(dir, name+"-"+suffix+ext)
}
