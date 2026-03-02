package pathutil

import (
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
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
		return camperrors.Wrap(ErrInvalidSubmodulePath, "path must not be empty")
	}

	if filepath.IsAbs(subPath) {
		return camperrors.Wrapf(ErrInvalidSubmodulePath, "path must be relative, got %q", subPath)
	}

	for _, segment := range strings.Split(filepath.ToSlash(subPath), "/") {
		if segment == ".." {
			return camperrors.Wrapf(ErrInvalidSubmodulePath, "path must not contain \"..\" segments: %q", subPath)
		}
	}

	cleaned := filepath.Clean(subPath)
	if strings.HasPrefix(cleaned, "..") {
		return camperrors.Wrapf(ErrInvalidSubmodulePath, "path escapes root after cleaning: %q", subPath)
	}

	fullPath := filepath.Join(repoRoot, cleaned)
	rel, err := filepath.Rel(repoRoot, fullPath)
	if err != nil {
		return camperrors.Wrapf(ErrInvalidSubmodulePath, "cannot compute relative path: %q", subPath)
	}
	if strings.HasPrefix(rel, "..") {
		return camperrors.Wrapf(ErrInvalidSubmodulePath, "path escapes repository root: %q", subPath)
	}

	return nil
}
