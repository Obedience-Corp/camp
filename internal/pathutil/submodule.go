package pathutil

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ValidateSubmodulePath checks that subPath, when joined with repoRoot, resolves
// to a location inside repoRoot — without requiring the path to exist on disk.
//
// This function is intended for use in git submodule operations where the
// submodule directory may not yet exist (e.g., during clone or first-time init).
// Because the path may not exist, filepath.EvalSymlinks cannot be used; instead,
// filepath.Clean is applied to collapse ".." segments before the containment check.
//
// Rejections:
//   - subPath is empty
//   - subPath is absolute
//   - subPath contains ".." segments
//   - the cleaned joined path escapes repoRoot
//
// For existing paths, prefer ValidateBoundary which also resolves symlinks.
func ValidateSubmodulePath(repoRoot, subPath string) error {
	if subPath == "" {
		return fmt.Errorf("%w: path must not be empty", ErrInvalidSubmodulePath)
	}

	if filepath.IsAbs(subPath) {
		return fmt.Errorf("%w: path must be relative, got %q", ErrInvalidSubmodulePath, subPath)
	}

	for _, segment := range strings.Split(filepath.ToSlash(subPath), "/") {
		if segment == ".." {
			return fmt.Errorf("%w: path must not contain \"..\" segments: %q", ErrInvalidSubmodulePath, subPath)
		}
	}

	cleaned := filepath.Clean(subPath)
	if strings.HasPrefix(cleaned, "..") {
		return fmt.Errorf("%w: path escapes root after cleaning: %q", ErrInvalidSubmodulePath, subPath)
	}

	fullPath := filepath.Join(repoRoot, cleaned)
	rel, err := filepath.Rel(repoRoot, fullPath)
	if err != nil {
		return fmt.Errorf("%w: cannot compute relative path: %q", ErrInvalidSubmodulePath, subPath)
	}
	if strings.HasPrefix(rel, "..") {
		return fmt.Errorf("%w: path escapes repository root: %q", ErrInvalidSubmodulePath, subPath)
	}

	return nil
}
