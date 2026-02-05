// Package doctor provides campaign health diagnostics and automated repair.
//
// The doctor package implements a comprehensive health check system for git
// campaigns with submodules. It can diagnose issues such as:
//   - Uninitialized submodules
//   - Uncommitted changes
//   - Unpushed commits
//   - URL mismatches between .gitmodules and .git/config
//   - Missing branches
//
// When run with --fix, it can automatically repair common issues.
package doctor

// Severity indicates the level of concern for an issue.
type Severity int

const (
	// SeverityInfo indicates informational messages that don't require action.
	SeverityInfo Severity = iota
	// SeverityWarning indicates potential problems that should be addressed.
	SeverityWarning
	// SeverityError indicates critical problems that require attention.
	SeverityError
)

// String returns the string representation of severity.
func (s Severity) String() string {
	switch s {
	case SeverityInfo:
		return "info"
	case SeverityWarning:
		return "warning"
	case SeverityError:
		return "error"
	default:
		return "unknown"
	}
}

// Issue represents a detected problem in the campaign.
type Issue struct {
	// Severity indicates the importance of this issue.
	Severity Severity
	// CheckID identifies which check found this issue.
	CheckID string
	// Submodule is the affected submodule path (empty for root-level issues).
	Submodule string
	// Description explains what's wrong.
	Description string
	// FixCommand suggests a command to fix this issue.
	FixCommand string
	// AutoFixable indicates whether this issue can be automatically fixed.
	AutoFixable bool
	// Details contains additional context for the issue.
	Details map[string]any
}

// IsError returns true if this is an error-level issue.
func (i Issue) IsError() bool {
	return i.Severity == SeverityError
}

// IsWarning returns true if this is a warning-level issue.
func (i Issue) IsWarning() bool {
	return i.Severity == SeverityWarning
}

// CanFix returns true if this issue can be automatically fixed.
func (i Issue) CanFix() bool {
	return i.AutoFixable && i.FixCommand != ""
}
