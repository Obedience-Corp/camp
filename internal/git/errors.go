// Package git provides git-specific error types and utilities for
// handling git operations including lock file management.
package git

import (
	"errors"
	"fmt"
	"strings"
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

// Sentinel errors for common git error cases.
var (
	// ErrLockActive indicates a lock file is held by a running process.
	ErrLockActive = errors.New("lock file held by active process")

	// ErrLockRemovalFailed indicates we couldn't remove a stale lock.
	ErrLockRemovalFailed = errors.New("failed to remove stale lock")

	// ErrNotRepository indicates the path is not a git repository.
	ErrNotRepository = errors.New("not a git repository")

	// ErrNoChanges indicates there are no changes to commit.
	ErrNoChanges = errors.New("nothing to commit")
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
