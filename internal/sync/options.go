// Package sync provides safe submodule synchronization with pre-flight checks.
//
// The sync package implements a three-phase approach to submodule management:
//  1. Pre-flight checks for uncommitted changes, unpushed commits, and URL mismatches
//  2. URL synchronization from .gitmodules to .git/config
//  3. Submodule update with validation
//
// This prevents data loss from stale URLs or uncommitted changes during sync operations.
package sync

// SyncOptions configures sync behavior.
type SyncOptions struct {
	// DryRun shows what would happen without making changes.
	DryRun bool
	// Force skips safety checks (warnings still shown).
	Force bool
	// Verbose shows detailed output for each submodule.
	Verbose bool
	// Parallel is the number of parallel operations (default 4).
	Parallel int
	// NoFetch skips fetching from remotes.
	NoFetch bool
	// JSON outputs results as JSON.
	JSON bool
	// Submodules lists specific submodules to sync (empty = all).
	Submodules []string
}

// SyncResult contains the outcome of a sync operation.
type SyncResult struct {
	// Success indicates whether the overall sync succeeded.
	Success bool
	// PreflightPassed indicates whether pre-flight checks passed.
	PreflightPassed bool
	// URLChanges lists URLs that were synchronized.
	URLChanges []URLChange
	// UpdateResults contains the outcome for each submodule.
	UpdateResults []SubmoduleResult
	// Warnings contains non-fatal issues encountered.
	Warnings []string
	// Errors contains fatal errors that occurred.
	Errors []error
}

// SubmoduleResult tracks the outcome for a single submodule.
type SubmoduleResult struct {
	// Name is the submodule name (e.g., "projects/camp").
	Name string
	// Path is the filesystem path to the submodule.
	Path string
	// Success indicates whether this submodule synced successfully.
	Success bool
	// Error contains any error that occurred during sync.
	Error error
	// WasClean indicates the submodule had no uncommitted changes.
	WasClean bool
	// HeadDetached indicates the submodule was in detached HEAD state.
	HeadDetached bool
	// URLChanged indicates the URL was updated during sync.
	URLChanged bool
	// OldURL is the previous URL (if URLChanged is true).
	OldURL string
	// NewURL is the new URL (if URLChanged is true).
	NewURL string
}

// URLChange represents a URL that was updated during synchronization.
type URLChange struct {
	// Submodule is the submodule name.
	Submodule string
	// OldURL is the URL before sync.
	OldURL string
	// NewURL is the URL after sync.
	NewURL string
}

// SyncerOption configures a Syncer.
type SyncerOption func(*Syncer)

// Syncer orchestrates sync operations for a repository.
type Syncer struct {
	repoRoot        string
	options         SyncOptions
	cachedPreflight *PreflightResult // Optional pre-computed preflight result
}

// NewSyncer creates a new Syncer for the given repository root.
func NewSyncer(repoRoot string, opts ...SyncerOption) *Syncer {
	s := &Syncer{
		repoRoot: repoRoot,
		options: SyncOptions{
			Parallel: 4, // default
		},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// RepoRoot returns the repository root path.
func (s *Syncer) RepoRoot() string {
	return s.repoRoot
}

// Options returns the current sync options.
func (s *Syncer) Options() SyncOptions {
	return s.options
}

// SetPreflightResult injects a pre-computed preflight result so Sync()
// won't re-run preflight checks.
func (s *Syncer) SetPreflightResult(pr *PreflightResult) {
	s.cachedPreflight = pr
}

// WithDryRun sets dry-run mode.
func WithDryRun(dryRun bool) SyncerOption {
	return func(s *Syncer) {
		s.options.DryRun = dryRun
	}
}

// WithForce sets force mode to skip safety checks.
func WithForce(force bool) SyncerOption {
	return func(s *Syncer) {
		s.options.Force = force
	}
}

// WithVerbose enables verbose output.
func WithVerbose(verbose bool) SyncerOption {
	return func(s *Syncer) {
		s.options.Verbose = verbose
	}
}

// WithParallel sets the number of parallel operations.
func WithParallel(n int) SyncerOption {
	return func(s *Syncer) {
		if n > 0 {
			s.options.Parallel = n
		}
	}
}

// WithNoFetch disables fetching from remotes.
func WithNoFetch(noFetch bool) SyncerOption {
	return func(s *Syncer) {
		s.options.NoFetch = noFetch
	}
}

// WithJSON enables JSON output.
func WithJSON(json bool) SyncerOption {
	return func(s *Syncer) {
		s.options.JSON = json
	}
}

// WithPreflightResult provides a pre-computed preflight result to avoid running
// preflight checks twice. If set, Sync() will use this result instead of
// running RunPreflight() internally.
func WithPreflightResult(pr *PreflightResult) SyncerOption {
	return func(s *Syncer) {
		s.cachedPreflight = pr
	}
}

// WithSubmodules sets specific submodules to sync.
func WithSubmodules(submodules []string) SyncerOption {
	return func(s *Syncer) {
		s.options.Submodules = submodules
	}
}

// Exit codes for sync operations.
const (
	// ExitSuccess indicates the sync completed successfully.
	ExitSuccess = 0
	// ExitPreflightFailed indicates a pre-flight check failed in safe mode.
	ExitPreflightFailed = 1
	// ExitSyncFailed indicates the sync or update operation failed.
	ExitSyncFailed = 2
	// ExitValidationFailed indicates post-sync validation failed.
	ExitValidationFailed = 3
	// ExitInvalidArgs indicates invalid command-line arguments.
	ExitInvalidArgs = 4
)
