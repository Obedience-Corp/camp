package clone

// Severity indicates the importance of a validation issue.
type Severity int

const (
	// SeverityInfo indicates informational messages, not failures.
	SeverityInfo Severity = iota
	// SeverityWarning indicates potential issues that don't prevent operation.
	SeverityWarning
	// SeverityError indicates problems that should be addressed.
	SeverityError
)

// String returns the string representation of a severity level.
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
