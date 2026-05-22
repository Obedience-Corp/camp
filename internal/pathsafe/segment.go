// Package pathsafe owns path segment validation shared by campaign workflows.
package pathsafe

import (
	"regexp"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// SegmentPattern is a path-safety check, not a style enforcer. It permits any
// naming convention users already use (kebab-case, snake_case, camelCase,
// PascalCase, dotted versions like v1.2, etc.) and only rejects values that
// would be unsafe as filesystem path segments or confuse shell tooling:
//
//   - empty
//   - contains '/' or '\' (would split into multiple path segments; '\' is the
//     path separator on Windows)
//   - starts with '.' (reserved for hidden / "." / ".." traversal)
//   - starts with '-' (would parse as a CLI flag in downstream tools)
//   - contains whitespace, NUL, or ASCII control characters
//   - longer than 80 chars (cross-fs name-length headroom)
//
// Anything else, including uppercase, dots, and unicode letters, is accepted
// because the project does not own its users' naming conventions.
var SegmentPattern = regexp.MustCompile(`^[^\s/\\.\-\x00-\x1f][^\s/\\\x00-\x1f]{0,79}$`)

// ValidateSegment verifies value is safe as one filesystem path segment.
func ValidateSegment(field, value string) error {
	if value == "" {
		return camperrors.NewValidation(field, field+" must not be empty", nil)
	}
	if !SegmentPattern.MatchString(value) {
		return camperrors.NewValidation(field,
			"invalid "+field+" "+value+": must not be empty, must not contain '/', '\\', whitespace, or control characters, must not start with '.' or '-', max 80 chars", nil)
	}
	return nil
}
