// Package pathutil provides filesystem path utilities for camp, including
// boundary enforcement to prevent path traversal attacks.
//
// macOS note: /var is a symlink to /private/var. Always resolve symlinks via
// filepath.EvalSymlinks before comparing paths to avoid false boundary escapes.
package pathutil

import (
	"errors"
	"fmt"
)

// ErrOutsideBoundary is returned when a target path resolves outside the root.
var ErrOutsideBoundary = errors.New("path escapes boundary")

// ErrBoundaryRootInvalid is returned when root cannot be resolved or is empty.
var ErrBoundaryRootInvalid = errors.New("boundary root is invalid")

// BoundaryError wraps a boundary violation with the specific paths involved.
type BoundaryError struct {
	Root   string
	Target string
	Cause  error
}

func (e *BoundaryError) Error() string {
	return fmt.Sprintf("%v: target %q escapes root %q", e.Cause, e.Target, e.Root)
}

func (e *BoundaryError) Unwrap() error {
	return e.Cause
}
