package triage

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// Violation is one schema rule failure. Field is the exact JSON path of the
// offending value ("anchors[0].kind", "rows[3].policy.evidence") so a caller
// can point a user at the value to fix without re-deriving where it lives.
// Allowed carries the permitted values when the rule is an enum.
type Violation struct {
	Field   string   `json:"field"`
	Message string   `json:"message"`
	Allowed []string `json:"allowed,omitempty"`
}

// String renders one violation on a single line.
func (v Violation) String() string {
	var b strings.Builder
	b.WriteString(v.Field)
	b.WriteString(": ")
	b.WriteString(v.Message)
	if len(v.Allowed) > 0 {
		b.WriteString(" (allowed: ")
		b.WriteString(strings.Join(v.Allowed, ", "))
		b.WriteString(")")
	}
	return b.String()
}

// ValidationError reports every rule a triage document violated, not just the
// first. Camp's contract for agent- and user-supplied documents (spec doc 03:
// "Rejection lists every violated field") is that one submission produces one
// complete list of what to fix.
//
// It matches camperrors.ErrInvalidInput so CLI surfaces can map it to exit
// code 1 without type-asserting.
type ValidationError struct {
	// Kind names the document format, e.g. "evidence record".
	Kind string
	// Violations is the complete, field-ordered list of failures.
	Violations []Violation
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	var b strings.Builder
	b.WriteString("invalid ")
	b.WriteString(e.Kind)
	b.WriteString(": ")
	b.WriteString(strconv.Itoa(len(e.Violations)))
	b.WriteString(" problem")
	if len(e.Violations) != 1 {
		b.WriteString("s")
	}
	for _, v := range e.Violations {
		b.WriteString("\n  - ")
		b.WriteString(v.String())
	}
	return b.String()
}

// Is reports whether e matches target, so errors.Is(err, ErrInvalidInput)
// holds for every schema rejection.
func (e *ValidationError) Is(target error) bool {
	return target == camperrors.ErrInvalidInput
}

// newValidationError returns a *ValidationError for kind, or nil when there is
// nothing to report. Returning a typed nil would be a trap, so callers get an
// untyped nil error.
func newValidationError(kind string, violations []Violation) error {
	if len(violations) == 0 {
		return nil
	}
	return &ValidationError{Kind: kind, Violations: violations}
}

// Violations converts err to its list of field-level failures. It returns nil
// for any error that is not a schema rejection.
func Violations(err error) []Violation {
	var ve *ValidationError
	if camperrors.As(err, &ve) {
		return ve.Violations
	}
	return nil
}

// joinPath appends a field name to a JSON path. The root path is empty, so the
// first segment carries no separator.
func joinPath(path, field string) string {
	if path == "" {
		return field
	}
	return path + "." + field
}

// indexPath renders the i-th element of the slice at path.
func indexPath(path string, i int) string {
	return fmt.Sprintf("%s[%d]", path, i)
}

// checkRequired reports a violation when a required string is empty or blank.
func checkRequired(path, value string) []Violation {
	if strings.TrimSpace(value) == "" {
		return []Violation{{Field: path, Message: "is required"}}
	}
	return nil
}

// checkEnum reports a violation when value is not a member of allowed. An
// empty value reports as missing rather than as a bad enum so the message
// matches what the user actually did.
func checkEnum(path, value string, allowed []string) []Violation {
	if value == "" {
		return []Violation{{Field: path, Message: "is required", Allowed: allowed}}
	}
	for _, a := range allowed {
		if value == a {
			return nil
		}
	}
	return []Violation{{
		Field:   path,
		Message: "unknown value " + quote(value),
		Allowed: allowed,
	}}
}

// checkOptionalEnum is checkEnum that accepts an empty value.
func checkOptionalEnum(path, value string, allowed []string) []Violation {
	if value == "" {
		return nil
	}
	return checkEnum(path, value, allowed)
}

// checkTimeSet reports a violation when a required timestamp is the zero time.
func checkTimeSet(path string, t time.Time) []Violation {
	if t.IsZero() {
		return []Violation{{Field: path, Message: "is required"}}
	}
	return nil
}

// checkMinInt reports a violation when n is below min.
func checkMinInt(path string, n, minimum int) []Violation {
	if n < minimum {
		return []Violation{{
			Field:   path,
			Message: fmt.Sprintf("must be >= %d, got %d", minimum, n),
		}}
	}
	return nil
}

// checkEmptySlice reports a violation when a slice that must be empty is not.
// Used by conditional rules such as a no-evidence record carrying judgment.
func checkEmptySlice[T any](path string, values []T, because string) []Violation {
	if len(values) == 0 {
		return nil
	}
	return []Violation{{Field: path, Message: "must be empty " + because}}
}

// checkEmptyString reports a violation when a string that must be empty is not.
func checkEmptyString(path, value, because string) []Violation {
	if value == "" {
		return nil
	}
	return []Violation{{Field: path, Message: "must be empty " + because}}
}

// sortedKeys returns the keys of m in lexical order, so diagnostics over maps
// are stable run to run.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// quote wraps a value in double quotes for diagnostics.
func quote(s string) string {
	return `"` + s + `"`
}
