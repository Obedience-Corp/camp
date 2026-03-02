package project

import (
	"errors"
	"regexp"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// ErrInvalidProjectName is returned when a project name fails validation.
var ErrInvalidProjectName = errors.New("invalid project name")

// projectNameRe is the strict allowlist for project names.
// Names must start with alphanumeric and contain only alphanumeric, dot, underscore, or hyphen.
var projectNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// ValidateProjectName checks that name is a safe, non-traversal project identifier.
// It rejects empty names, names with path separators, ".." sequences, and names
// that do not match the strict alphanumeric-plus-safe-chars pattern.
func ValidateProjectName(name string) error {
	if name == "" {
		return camperrors.Wrap(ErrInvalidProjectName, "name must not be empty")
	}
	if strings.Contains(name, "..") {
		return camperrors.Wrapf(ErrInvalidProjectName, "name must not contain \"..\": %q", name)
	}
	if strings.ContainsAny(name, `/\`) {
		return camperrors.Wrapf(ErrInvalidProjectName, "name must not contain path separators: %q", name)
	}
	if !projectNameRe.MatchString(name) {
		return camperrors.Wrapf(ErrInvalidProjectName, "name contains invalid characters: %q", name)
	}
	return nil
}
