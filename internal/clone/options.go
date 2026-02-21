// Package clone provides single-command campaign setup with submodule initialization.
//
// The clone package implements a four-phase approach to campaign setup:
//  1. Clone the repository with recursive submodules
//  2. Synchronize URLs from .gitmodules to .git/config
//  3. Update submodules to ensure all are at correct commits
//  4. Validate the setup (all initialized, correct commits, matching URLs)
//
// This provides a reliable single-command bootstrap experience for new devices.
package clone

import "github.com/obediencecorp/camp/internal/sync"

// CloneOptions configures clone behavior.
type CloneOptions struct {
	// URL is the Git URL of the campaign repository (required).
	URL string
	// Directory is the target directory name (optional, defaults to repo name).
	Directory string
	// Branch specifies a specific branch to clone (optional, defaults to default branch).
	Branch string
	// Depth creates a shallow clone with the specified history depth (0 = full history).
	Depth int
	// NoSubmodules clones without initializing submodules.
	NoSubmodules bool
	// NoValidate skips post-clone validation.
	NoValidate bool
	// Verbose shows detailed output for each operation.
	Verbose bool
	// JSON outputs results as JSON for scripting.
	JSON bool
	// Parallel is the number of concurrent submodule initializations (default 4).
	Parallel int
	// NoRegister skips auto-registration in global campaign registry.
	NoRegister bool
}

// CloneResult contains the outcome of a clone operation.
type CloneResult struct {
	// Success indicates whether the overall clone succeeded.
	Success bool
	// Directory is the absolute path to the cloned campaign.
	Directory string
	// Branch is the branch that was cloned.
	Branch string
	// Submodules contains the outcome for each submodule.
	Submodules []SubmoduleResult
	// URLChanges lists any URLs that were updated during post-clone sync.
	URLChanges []URLChange
	// Validation contains post-clone validation results (nil if skipped).
	Validation *ValidationResult
	// Errors contains fatal errors that occurred.
	Errors []error
	// Warnings contains non-fatal issues encountered.
	Warnings []string
	// Registration contains auto-registration results (nil if skipped or not a campaign).
	Registration *RegistrationResult
}

// RegistrationResult contains the outcome of auto-registration.
type RegistrationResult struct {
	// Registered indicates whether the campaign was registered.
	Registered bool
	// CampaignID is the registered campaign's ID.
	CampaignID string
	// CampaignName is the registered campaign's name.
	CampaignName string
	// Error contains any registration error (non-fatal).
	Error error
}

// URLChange represents a URL that was updated during synchronization.
type URLChange struct {
	// Submodule is the submodule path.
	Submodule string
	// OldURL is the URL before sync.
	OldURL string
	// NewURL is the URL after sync.
	NewURL string
}

// SubmoduleResult tracks the outcome for a single submodule.
type SubmoduleResult struct {
	// Name is the submodule name from .gitmodules.
	Name string
	// Path is the filesystem path to the submodule.
	Path string
	// URL is the submodule's remote URL.
	URL string
	// Success indicates whether this submodule was initialized successfully.
	Success bool
	// Commit is the HEAD commit hash after initialization.
	Commit string
	// Error contains any error that occurred during initialization.
	Error error
}

// ValidationResult tracks post-clone validation checks.
type ValidationResult struct {
	// Passed indicates whether all validation checks passed.
	Passed bool
	// AllInitialized indicates all declared submodules have content.
	AllInitialized bool
	// CorrectCommits indicates all submodules are at expected commits.
	CorrectCommits bool
	// URLsMatch indicates .gitmodules and .git/config URLs match.
	URLsMatch bool
	// Issues contains any validation problems found.
	Issues []ValidationIssue
	// CheckResults contains per-check pass/fail status.
	CheckResults map[string]bool
}

// ValidationIssue represents a validation problem found during post-clone checks.
type ValidationIssue struct {
	// CheckID identifies which check found this issue.
	CheckID string
	// Submodule is the submodule path where the issue was found.
	Submodule string
	// Severity indicates the importance of this issue.
	Severity Severity
	// Description explains the validation failure.
	Description string
	// FixCommand is a suggested command to fix the issue (if any).
	FixCommand string
	// AutoFixable indicates if this issue can be automatically fixed.
	AutoFixable bool
}

// ClonerOption configures a Cloner.
type ClonerOption func(*Cloner)

// Cloner orchestrates clone operations for a campaign repository.
type Cloner struct {
	options  CloneOptions
	syncer   *sync.Syncer     // Optional syncer for post-clone URL synchronization
	progress ProgressReporter // Progress reporter for output
}

// NewCloner creates a new Cloner with the given options.
func NewCloner(opts ...ClonerOption) *Cloner {
	c := &Cloner{}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Options returns the current clone options.
func (c *Cloner) Options() CloneOptions {
	return c.options
}

// WithURL sets the repository URL to clone.
func WithURL(url string) ClonerOption {
	return func(c *Cloner) {
		c.options.URL = url
	}
}

// WithDirectory sets the target directory for the clone.
func WithDirectory(dir string) ClonerOption {
	return func(c *Cloner) {
		c.options.Directory = dir
	}
}

// WithBranch sets a specific branch to clone.
func WithBranch(branch string) ClonerOption {
	return func(c *Cloner) {
		c.options.Branch = branch
	}
}

// WithDepth creates a shallow clone with the specified history depth.
func WithDepth(depth int) ClonerOption {
	return func(c *Cloner) {
		if depth >= 0 {
			c.options.Depth = depth
		}
	}
}

// WithNoSubmodules clones without initializing submodules.
func WithNoSubmodules(noSubmodules bool) ClonerOption {
	return func(c *Cloner) {
		c.options.NoSubmodules = noSubmodules
	}
}

// WithNoValidate skips post-clone validation.
func WithNoValidate(noValidate bool) ClonerOption {
	return func(c *Cloner) {
		c.options.NoValidate = noValidate
	}
}

// WithVerbose enables verbose output.
func WithVerbose(verbose bool) ClonerOption {
	return func(c *Cloner) {
		c.options.Verbose = verbose
	}
}

// WithJSON enables JSON output.
func WithJSON(json bool) ClonerOption {
	return func(c *Cloner) {
		c.options.JSON = json
	}
}

// WithParallel sets the number of concurrent submodule initializations.
func WithParallel(n int) ClonerOption {
	return func(c *Cloner) {
		if n > 0 {
			c.options.Parallel = n
		}
	}
}

// WithSyncer sets an optional syncer for post-clone URL synchronization.
// If provided, the syncer will be used instead of basic git commands for URL sync.
func WithSyncer(s *sync.Syncer) ClonerOption {
	return func(c *Cloner) {
		c.syncer = s
	}
}

// WithNoRegister skips auto-registration in the global campaign registry.
func WithNoRegister(noRegister bool) ClonerOption {
	return func(c *Cloner) {
		c.options.NoRegister = noRegister
	}
}

// WithProgress sets the progress reporter for clone operations.
// If not set, a SilentReporter is used.
func WithProgress(p ProgressReporter) ClonerOption {
	return func(c *Cloner) {
		c.progress = p
	}
}

// Exit codes for clone operations.
const (
	// ExitSuccess indicates the clone completed successfully.
	ExitSuccess = 0
	// ExitCloneFailed indicates the clone failed (no campaign created).
	ExitCloneFailed = 1
	// ExitPartialSuccess indicates some submodules failed to initialize.
	ExitPartialSuccess = 2
	// ExitValidationFailed indicates post-clone validation failed.
	ExitValidationFailed = 3
	// ExitInvalidArgs indicates invalid command-line arguments.
	ExitInvalidArgs = 4
)
