// Package sync provides safe submodule synchronization with pre-flight checks.
//
// The sync package implements a three-phase approach to submodule management:
//  1. Pre-flight checks for uncommitted changes, unpushed commits, and URL mismatches
//  2. URL synchronization from .gitmodules to .git/config
//  3. Submodule update with validation
//
// This prevents data loss from stale URLs or uncommitted changes during sync operations.
package sync

import (
	"github.com/Obedience-Corp/camp/internal/artifacts"
	"github.com/Obedience-Corp/camp/internal/peer"
)

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
	// GitOnly skips artifact transfer on a peer-assisted sync.
	GitOnly bool
	// ArtifactsOnly skips the git phases and only pulls declared artifact
	// roots from the peer (all policies, since the ask is explicit).
	ArtifactsOnly bool
	// VerifyArtifacts checks artifact roots against last-transfer snapshots
	// (no transfer, no git phases). VerifyPeer scopes it to one peer id;
	// empty means every peer with a snapshot.
	VerifyArtifacts bool
	// VerifyPeer scopes VerifyArtifacts to one peer id.
	VerifyPeer string
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
	// Artifacts contains per-root outcomes of the peer artifact pull.
	Artifacts []*artifacts.PullResult
	// ArtifactVerifies contains per-root, per-peer verification outcomes.
	ArtifactVerifies []ArtifactVerify
	// Warnings contains non-fatal issues encountered.
	Warnings []string
	// Errors contains fatal errors that occurred.
	Errors []error
}

// ArtifactVerify pairs a verification result with the peer snapshot it ran
// against.
type ArtifactVerify struct {
	Peer   string
	Result *artifacts.VerifyResult
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
	// CheckedOutBranch is the default branch checked out after submodule update.
	CheckedOutBranch string
	// DriftWarning describes detected gitlink drift for this submodule.
	DriftWarning string
	// PeerFetched indicates objects were fetched from the configured peer
	// before the origin-based update.
	PeerFetched bool
	// PeerWarning describes a peer fetch failure; the submodule still syncs
	// through origin when this is set.
	PeerWarning string
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
	peer            *peer.Source     // Optional peer to fetch objects from before origin
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

// WithPeer configures a peer source to fetch git objects from before each
// submodule's origin-based update. The peer is a transfer accelerator only:
// preflight checks, origin URLs, and post-sync validation are unchanged, and
// a failed peer fetch degrades to the normal origin path with a warning.
func WithPeer(p *peer.Source) SyncerOption {
	return func(s *Syncer) {
		s.peer = p
	}
}

// WithSubmodules sets specific submodules to sync.
func WithSubmodules(submodules []string) SyncerOption {
	return func(s *Syncer) {
		s.options.Submodules = submodules
	}
}

// WithGitOnly skips artifact transfer on a peer-assisted sync.
func WithGitOnly(gitOnly bool) SyncerOption {
	return func(s *Syncer) {
		s.options.GitOnly = gitOnly
	}
}

// WithArtifactsOnly skips the git phases and only pulls declared artifact
// roots from the configured peer.
func WithArtifactsOnly(artifactsOnly bool) SyncerOption {
	return func(s *Syncer) {
		s.options.ArtifactsOnly = artifactsOnly
	}
}

// WithVerifyArtifacts checks artifact roots against last-transfer snapshots
// instead of syncing. peerID scopes the check to one peer; empty checks every
// peer with a snapshot.
func WithVerifyArtifacts(verify bool, peerID string) SyncerOption {
	return func(s *Syncer) {
		s.options.VerifyArtifacts = verify
		s.options.VerifyPeer = peerID
	}
}

// Exit codes for sync operations.
const (
	// ExitSuccess indicates the sync completed successfully.
	ExitSuccess = 0
	// ExitPreflightFailed indicates a pre-flight check failed in safe mode.
	ExitPreflightFailed = 1
	// ExitSyncFailed indicates the sync or update operation failed.
	ExitSyncFailed = 1
	// ExitValidationFailed indicates post-sync validation failed.
	ExitValidationFailed = 3
	// ExitInvalidArgs indicates invalid command-line arguments.
	ExitInvalidArgs = 2
)
