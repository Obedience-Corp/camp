package clone

import "context"

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

// ValidationCheck defines a single validation check that can be run against a repository.
type ValidationCheck interface {
	// ID returns the unique identifier for this check.
	ID() string
	// Name returns the human-readable name.
	Name() string
	// Run executes the check and returns issues found.
	Run(ctx context.Context, repoPath string) ([]ValidationIssue, error)
}

// Validator runs validation checks on a repository.
type Validator interface {
	// Validate runs all registered checks and returns the combined result.
	Validate(ctx context.Context, repoPath string) (*ValidationResult, error)
	// RegisterCheck adds a check to the validator.
	RegisterCheck(check ValidationCheck)
}

// DefaultValidator implements Validator with configurable checks.
type DefaultValidator struct {
	checks []ValidationCheck
}

// NewValidator creates a new DefaultValidator.
func NewValidator() *DefaultValidator {
	return &DefaultValidator{
		checks: make([]ValidationCheck, 0),
	}
}

// RegisterCheck adds a validation check to the validator.
func (v *DefaultValidator) RegisterCheck(check ValidationCheck) {
	v.checks = append(v.checks, check)
}

// Validate runs all registered checks against the repository.
func (v *DefaultValidator) Validate(ctx context.Context, repoPath string) (*ValidationResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	result := &ValidationResult{
		Passed:         true,
		AllInitialized: true,
		CorrectCommits: true,
		URLsMatch:      true,
		CheckResults:   make(map[string]bool),
	}

	for _, check := range v.checks {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		issues, err := check.Run(ctx, repoPath)
		if err != nil {
			return nil, err
		}

		hasError := false
		for _, issue := range issues {
			result.Issues = append(result.Issues, issue)
			if issue.Severity == SeverityError {
				hasError = true
				result.Passed = false
			}
		}
		result.CheckResults[check.ID()] = !hasError
	}

	return result, nil
}

// Checks returns the list of registered checks.
func (v *DefaultValidator) Checks() []ValidationCheck {
	return v.checks
}
