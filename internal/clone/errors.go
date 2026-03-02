package clone

import (
	"errors"
	"fmt"
)

// Sentinel errors for clone operations.
var (
	// ErrCloneFailed indicates the git clone operation failed.
	ErrCloneFailed = errors.New("clone failed")

	// ErrGitmodulesParse indicates .gitmodules could not be parsed.
	ErrGitmodulesParse = errors.New("could not parse .gitmodules")

	// ErrCheckoutFailed indicates a git checkout operation failed.
	ErrCheckoutFailed = errors.New("git checkout failed")

	// ErrEmptyWorkingTree indicates a submodule has no checked-out files.
	ErrEmptyWorkingTree = errors.New("submodule has empty working tree")

	// ErrSubmoduleRead indicates a submodule directory could not be read.
	ErrSubmoduleRead = errors.New("cannot read submodule directory")
)

// SubmoduleError wraps submodule-specific errors with context.
type SubmoduleError struct {
	// Op is the operation that failed (e.g., "init", "checkout", "update").
	Op string
	// Submodule is the submodule path.
	Submodule string
	// Cause is the underlying error.
	Cause error
}

func (e *SubmoduleError) Error() string {
	if e.Submodule != "" {
		return fmt.Sprintf("%s %s: %v", e.Op, e.Submodule, e.Cause)
	}
	return fmt.Sprintf("%s: %v", e.Op, e.Cause)
}

func (e *SubmoduleError) Unwrap() error {
	return e.Cause
}
