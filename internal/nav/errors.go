package nav

import (
	"errors"
	"fmt"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// Navigation errors.
// Sentinels marked with %w wrap the canonical sentinel from internal/errors
// to enable cross-package errors.Is() matching.
var (
	// ErrCategoryNotFound is returned when a category directory does not exist.
	ErrCategoryNotFound = fmt.Errorf("category directory not found: %w", camperrors.ErrNotFound)

	// ErrNotADirectory is returned when a category path exists but is not a directory.
	ErrNotADirectory = errors.New("category path is not a directory")
)

// DirectJumpError provides detailed context for direct jump failures.
type DirectJumpError struct {
	// Category that was being resolved.
	Category Category
	// Path that was checked.
	Path string
	// Err is the underlying error.
	Err error
}

func (e *DirectJumpError) Error() string {
	if errors.Is(e.Err, ErrCategoryNotFound) {
		return fmt.Sprintf("category directory not found: %s (expected at %s)", e.Category, e.Path)
	}
	if errors.Is(e.Err, ErrNotADirectory) {
		return fmt.Sprintf("category path is not a directory: %s", e.Path)
	}
	return fmt.Sprintf("failed to access category %s: %v", e.Category, e.Err)
}

func (e *DirectJumpError) Unwrap() error {
	return e.Err
}

// Is implements error matching for DirectJumpError.
func (e *DirectJumpError) Is(target error) bool {
	if errors.Is(e.Err, target) {
		return true
	}
	t, ok := target.(*DirectJumpError)
	if !ok {
		return false
	}
	return e.Category == t.Category && errors.Is(e.Err, t.Err)
}
