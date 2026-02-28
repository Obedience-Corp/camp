package sync

import (
	"errors"
	"fmt"

	"github.com/Obedience-Corp/camp/internal/git"
)

// Sentinel errors for sync operations.
// Shared sentinels are aliased from the git package to avoid duplication.
var (
	// ErrPreflightFailed indicates pre-flight checks failed.
	ErrPreflightFailed = errors.New("pre-flight checks failed")

	// ErrListSubmodules indicates submodule listing from .gitmodules failed.
	ErrListSubmodules = errors.New("failed to list submodules")

	// ErrURLCapture indicates URL state capture failed.
	ErrURLCapture = errors.New("failed to capture URL state")

	// ErrSubmoduleValidation indicates post-update validation failed.
	ErrSubmoduleValidation = errors.New("submodule validation failed")

	// ErrNestedSubmodules indicates nested submodule initialization failed.
	ErrNestedSubmodules = errors.New("nested submodule init failed")

	// Shared with git package -- aliased to avoid duplicate sentinel errors.
	ErrSubmoduleSync           = git.ErrSubmoduleSync
	ErrSubmoduleInit           = git.ErrSubmoduleInit
	ErrSubmoduleNotInitialized = git.ErrSubmoduleNotInitialized
)

// SyncError wraps sync-specific errors with context.
type SyncError struct {
	// Op is the operation that failed.
	Op string
	// Submodule is the submodule path (empty for repo-level operations).
	Submodule string
	// Cause is the underlying error.
	Cause error
}

func (e *SyncError) Error() string {
	if e.Submodule != "" {
		return fmt.Sprintf("%s %s: %v", e.Op, e.Submodule, e.Cause)
	}
	return fmt.Sprintf("%s: %v", e.Op, e.Cause)
}

func (e *SyncError) Unwrap() error {
	return e.Cause
}
