package doctor

import "context"

// DoctorOptions configures doctor behavior.
type DoctorOptions struct {
	// Fix enables automatic repair of detected issues.
	Fix bool
	// Verbose shows detailed information about each check.
	Verbose bool
	// JSON outputs results in machine-readable JSON format.
	JSON bool
	// SubmodulesOnly limits checks to submodule-related issues.
	SubmodulesOnly bool
	// Checks limits which checks to run (empty = all checks).
	Checks []string
}

// DoctorResult contains the outcome of a health check.
type DoctorResult struct {
	// Success is true when no errors are found (warnings are OK).
	Success bool
	// Passed is the number of checks that passed without issues.
	Passed int
	// Warned is the number of checks that found warnings.
	Warned int
	// Failed is the number of checks that found errors.
	Failed int
	// Issues contains all detected problems.
	Issues []Issue
	// Fixed contains issues that were successfully repaired (when Fix=true).
	Fixed []Issue
	// CheckResults contains per-check pass/fail status.
	CheckResults map[string]bool
}

// CheckResult contains findings from a single health check.
type CheckResult struct {
	// Passed is true when no errors were found.
	Passed bool
	// Total is the number of items checked.
	Total int
	// Issues contains problems found by this check.
	Issues []Issue
	// Details contains additional check-specific information.
	Details map[string]any
}

// Check defines a single health check operation.
type Check interface {
	// ID returns the unique identifier for this check.
	ID() string
	// Name returns the human-readable name.
	Name() string
	// Description returns a brief explanation of what this check does.
	Description() string
	// Run executes the check and returns issues found.
	Run(ctx context.Context, repoPath string) (*CheckResult, error)
	// Fix attempts to repair the given issues.
	// Returns the list of issues that were successfully fixed.
	Fix(ctx context.Context, repoPath string, issues []Issue) ([]Issue, error)
}

// DoctorOption configures a Doctor instance.
type DoctorOption func(*Doctor)

// Doctor orchestrates health checks for a campaign.
type Doctor struct {
	repoRoot string
	options  DoctorOptions
	checks   []Check
}

// NewDoctor creates a new Doctor for the given repository root.
func NewDoctor(repoRoot string, opts ...DoctorOption) *Doctor {
	d := &Doctor{
		repoRoot: repoRoot,
		options:  DoctorOptions{},
		checks:   make([]Check, 0),
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// Options returns the current doctor options.
func (d *Doctor) Options() DoctorOptions {
	return d.options
}

// RegisterCheck adds a health check to the doctor.
func (d *Doctor) RegisterCheck(check Check) {
	d.checks = append(d.checks, check)
}

// Checks returns the list of registered checks.
func (d *Doctor) Checks() []Check {
	return d.checks
}

// WithFix enables automatic repair of detected issues.
func WithFix(fix bool) DoctorOption {
	return func(d *Doctor) {
		d.options.Fix = fix
	}
}

// WithVerbose enables detailed output.
func WithVerbose(verbose bool) DoctorOption {
	return func(d *Doctor) {
		d.options.Verbose = verbose
	}
}

// WithJSON enables JSON output.
func WithJSON(json bool) DoctorOption {
	return func(d *Doctor) {
		d.options.JSON = json
	}
}

// WithSubmodulesOnly limits checks to submodule-related issues.
func WithSubmodulesOnly(only bool) DoctorOption {
	return func(d *Doctor) {
		d.options.SubmodulesOnly = only
	}
}

// WithChecks sets specific checks to run.
func WithChecks(checks []string) DoctorOption {
	return func(d *Doctor) {
		d.options.Checks = checks
	}
}

// Exit codes for doctor operations.
const (
	// ExitSuccess indicates all checks passed.
	ExitSuccess = 0
	// ExitWarnings indicates warnings were found (but no errors).
	ExitWarnings = 1
	// ExitFailures indicates errors were found.
	ExitFailures = 2
	// ExitPartialFix indicates some but not all issues were fixed.
	ExitPartialFix = 3
	// ExitInvalidArgs indicates invalid command-line arguments.
	ExitInvalidArgs = 4
)
