// Package git provides git-specific error types and utilities for
// handling git operations including lock file management.
package git

import (
	"errors"
	"fmt"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// GitErrorType classifies git operation failures.
type GitErrorType int

const (
	// GitErrorUnknown indicates an unclassified git error.
	GitErrorUnknown GitErrorType = iota
	// GitErrorLock indicates an index.lock issue.
	GitErrorLock
	// GitErrorNoChanges indicates nothing to commit.
	GitErrorNoChanges
	// GitErrorNotRepo indicates the path is not a git repository.
	GitErrorNotRepo
	// GitErrorPermission indicates permission denied.
	GitErrorPermission
	// GitErrorNetwork indicates network/remote issues.
	GitErrorNetwork
	// GitErrorSubmodule indicates a submodule-specific issue.
	GitErrorSubmodule
)

// String returns human-readable error type name.
func (t GitErrorType) String() string {
	switch t {
	case GitErrorLock:
		return "lock"
	case GitErrorNoChanges:
		return "no_changes"
	case GitErrorNotRepo:
		return "not_repo"
	case GitErrorPermission:
		return "permission"
	case GitErrorNetwork:
		return "network"
	case GitErrorSubmodule:
		return "submodule"
	default:
		return "unknown"
	}
}

// LockError provides details about lock-related failures.
type LockError struct {
	// Path is the path to the lock file.
	Path string
	// ProcessID is the PID if lock is active (0 if stale or unknown).
	ProcessID int
	// Stale is true if the lock is orphaned (no active process).
	Stale bool
	// Err is the underlying error.
	Err error
}

// Error implements the error interface.
func (e *LockError) Error() string {
	if e.Stale {
		return fmt.Sprintf("stale lock at %s", e.Path)
	}
	if e.ProcessID > 0 {
		return fmt.Sprintf("lock at %s held by process %d", e.Path, e.ProcessID)
	}
	return fmt.Sprintf("lock file exists at %s", e.Path)
}

// Unwrap returns the underlying error for errors.Is/As support.
func (e *LockError) Unwrap() error {
	return e.Err
}

// GitOpError wraps errors from git command execution with structured context.
type GitOpError struct {
	// Op is the git operation that failed (e.g., "commit", "add", "diff").
	Op string
	// ErrType is the classified error type from git stderr.
	ErrType GitErrorType
	// Detail is the trimmed git stderr output.
	Detail string
	// Cause is the underlying exec error.
	Cause error
}

// Error implements the error interface.
func (e *GitOpError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("git %s failed (%s): %s", e.Op, e.ErrType.String(), e.Detail)
	}
	return fmt.Sprintf("git %s failed (%s)", e.Op, e.ErrType.String())
}

// Unwrap returns the underlying error for errors.Is/As support.
func (e *GitOpError) Unwrap() error {
	return e.Cause
}

// Sentinel errors for common git error cases.
// Sentinels marked with %w wrap the canonical sentinel from internal/errors
// to enable cross-package errors.Is() matching.
var (
	// ErrLockActive indicates a lock file is held by a running process.
	ErrLockActive = errors.New("lock file held by active process")

	// ErrLockRemovalFailed indicates we couldn't remove a stale lock.
	ErrLockRemovalFailed = errors.New("failed to remove stale lock")

	// ErrLockTimeout indicates the timeout was exceeded waiting for a lock to release.
	ErrLockTimeout = fmt.Errorf("timeout waiting for lock release: %w", camperrors.ErrTimeout)

	// ErrNotRepository indicates the path is not a git repository.
	ErrNotRepository = fmt.Errorf("not a git repository: %w", camperrors.ErrNotInitialized)

	// ErrNoChanges indicates there are no changes to commit.
	ErrNoChanges = errors.New("nothing to commit")

	// ErrStaleRef indicates a stale commit reference (commit no longer exists on remote).
	ErrStaleRef = errors.New("stale commit reference")

	// ErrBranchDetection indicates the default branch could not be determined.
	ErrBranchDetection = errors.New("could not determine default branch")

	// ErrBranchCheckout indicates a branch checkout failed.
	ErrBranchCheckout = errors.New("branch checkout failed")

	// ErrSubmoduleUpdate indicates a submodule update operation failed.
	ErrSubmoduleUpdate = errors.New("submodule update failed")

	// ErrSubmoduleInit indicates a submodule init operation failed.
	ErrSubmoduleInit = errors.New("submodule init failed")

	// ErrSubmoduleURL indicates a submodule URL could not be resolved.
	ErrSubmoduleURL = errors.New("could not resolve submodule URL")

	// ErrSubmoduleClone indicates a submodule clone at default branch failed.
	ErrSubmoduleClone = errors.New("submodule clone failed")

	// ErrSubmoduleRemove indicates a stale submodule directory could not be removed.
	ErrSubmoduleRemove = errors.New("failed to remove submodule directory")

	// ErrOrphanedGitlink indicates a gitlink exists in the index but has no entry in .gitmodules.
	ErrOrphanedGitlink = errors.New("orphaned gitlink in index")

	// ErrSubmoduleSync indicates a submodule sync operation failed.
	ErrSubmoduleSync = errors.New("submodule sync failed")

	// ErrSubmoduleNotInitialized indicates a submodule was not properly initialized.
	ErrSubmoduleNotInitialized = fmt.Errorf("submodule not initialized: %w", camperrors.ErrNotInitialized)

	// ErrStage indicates a staging (git add) operation failed.
	ErrStage = errors.New("staging failed")

	// ErrCommitFailed indicates a git commit operation failed.
	ErrCommitFailed = errors.New("commit failed")

	// ErrCommitCancelled indicates the user cancelled the commit.
	ErrCommitCancelled = fmt.Errorf("commit cancelled: %w", camperrors.ErrCancelled)

	// ErrCommitOptionsRequired indicates nil commit options were provided.
	ErrCommitOptionsRequired = fmt.Errorf("commit options required: %w", camperrors.ErrInvalidInput)

	// ErrCommitMessageRequired indicates a commit message was not provided.
	ErrCommitMessageRequired = fmt.Errorf("commit message is required: %w", camperrors.ErrInvalidInput)

	// ErrNoFilesSpecified indicates an empty file list was provided for staging.
	ErrNoFilesSpecified = fmt.Errorf("no files specified for staging: %w", camperrors.ErrInvalidInput)
)

// ClassifyGitError determines the error type from git stderr output.
func ClassifyGitError(stderr string, exitCode int) GitErrorType {
	lower := strings.ToLower(stderr)

	switch {
	case strings.Contains(lower, "index.lock"):
		return GitErrorLock
	case strings.Contains(lower, "nothing to commit"):
		return GitErrorNoChanges
	case strings.Contains(lower, "not a git repository"):
		return GitErrorNotRepo
	case strings.Contains(lower, "permission denied"):
		return GitErrorPermission
	case strings.Contains(lower, "could not resolve host"),
		strings.Contains(lower, "connection refused"),
		strings.Contains(lower, "failed to connect"),
		strings.Contains(lower, "connection timed out"):
		return GitErrorNetwork
	case strings.Contains(lower, "submodule"):
		return GitErrorSubmodule
	default:
		return GitErrorUnknown
	}
}

// NewLockError creates a new LockError with the given path.
func NewLockError(path string, err error) *LockError {
	return &LockError{
		Path: path,
		Err:  err,
	}
}

// NewStaleLockError creates a new LockError marked as stale.
func NewStaleLockError(path string) *LockError {
	return &LockError{
		Path:  path,
		Stale: true,
	}
}

// NewActiveLockError creates a new LockError with an active process ID.
func NewActiveLockError(path string, pid int) *LockError {
	return &LockError{
		Path:      path,
		ProcessID: pid,
		Stale:     false,
		Err:       ErrLockActive,
	}
}
