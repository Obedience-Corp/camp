// Package errors provides typed error types, sentinel errors, and wrapping
// utilities for the camp CLI.
package errors

import (
	"fmt"
	"strings"
)

// ValidationError indicates invalid input or state.
type ValidationError struct {
	// Field is the name of the invalid field or parameter.
	Field string
	// Message describes what is wrong.
	Message string
	// Err is the underlying error, if any.
	Err error
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("validation error on %s: %s: %v", e.Field, e.Message, e.Err)
	}
	return fmt.Sprintf("validation error on %s: %s", e.Field, e.Message)
}

// Unwrap returns the underlying error for errors.Is/As support.
func (e *ValidationError) Unwrap() error { return e.Err }

// NewValidation creates a ValidationError.
func NewValidation(field, message string, err error) *ValidationError {
	return &ValidationError{Field: field, Message: message, Err: err}
}

// NotFoundError indicates a resource could not be located.
type NotFoundError struct {
	// Resource is the type of resource (e.g., "project", "campaign").
	Resource string
	// ID is the identifier that was looked up.
	ID string
	// Err is the underlying error, if any.
	Err error
}

// Error implements the error interface.
func (e *NotFoundError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s not found: %s: %v", e.Resource, e.ID, e.Err)
	}
	return fmt.Sprintf("%s not found: %s", e.Resource, e.ID)
}

// Unwrap returns the underlying error for errors.Is/As support.
func (e *NotFoundError) Unwrap() error { return e.Err }

// NewNotFound creates a NotFoundError.
func NewNotFound(resource, id string, err error) *NotFoundError {
	return &NotFoundError{Resource: resource, ID: id, Err: err}
}

// ConfigError indicates a configuration problem.
type ConfigError struct {
	// Key is the configuration key or file that caused the error.
	Key string
	// Message describes the problem.
	Message string
	// Err is the underlying error, if any.
	Err error
}

// Error implements the error interface.
func (e *ConfigError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("config error (%s): %s: %v", e.Key, e.Message, e.Err)
	}
	return fmt.Sprintf("config error (%s): %s", e.Key, e.Message)
}

// Unwrap returns the underlying error for errors.Is/As support.
func (e *ConfigError) Unwrap() error { return e.Err }

// NewConfig creates a ConfigError.
func NewConfig(key, message string, err error) *ConfigError {
	return &ConfigError{Key: key, Message: message, Err: err}
}

// IOError indicates a filesystem or network I/O failure.
type IOError struct {
	// Op is the operation that failed (e.g., "read", "write", "open").
	Op string
	// Path is the file or resource path involved.
	Path string
	// Err is the underlying error.
	Err error
}

// Error implements the error interface.
func (e *IOError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("io %s failed on %s: %v", e.Op, e.Path, e.Err)
	}
	return fmt.Sprintf("io %s failed on %s", e.Op, e.Path)
}

// Unwrap returns the underlying error for errors.Is/As support.
func (e *IOError) Unwrap() error { return e.Err }

// NewIO creates an IOError.
func NewIO(op, path string, err error) *IOError {
	return &IOError{Op: op, Path: path, Err: err}
}

// PermissionError indicates an unauthorized or forbidden operation.
type PermissionError struct {
	// Action is the operation that was denied.
	Action string
	// Resource is the resource being accessed.
	Resource string
	// Err is the underlying error, if any.
	Err error
}

// Error implements the error interface.
func (e *PermissionError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("permission denied: %s on %s: %v", e.Action, e.Resource, e.Err)
	}
	return fmt.Sprintf("permission denied: %s on %s", e.Action, e.Resource)
}

// Unwrap returns the underlying error for errors.Is/As support.
func (e *PermissionError) Unwrap() error { return e.Err }

// NewPermission creates a PermissionError.
func NewPermission(action, resource string, err error) *PermissionError {
	return &PermissionError{Action: action, Resource: resource, Err: err}
}

// BoundaryError indicates a path escaped the campaign boundary (path containment violation).
// This is returned when a resolved path is not under the campaign root, including
// after symlink resolution.
type BoundaryError struct {
	// Op is the operation that detected the violation (e.g., "validate", "remove", "write").
	Op string
	// Path is the path that violated the boundary.
	Path string
	// Root is the campaign root that was used as the boundary.
	Root string
	// Err is the underlying error, if any.
	Err error
}

// Error implements the error interface.
func (e *BoundaryError) Error() string {
	msg := fmt.Sprintf("boundary violation: %s: path %q is outside campaign root %q", e.Op, e.Path, e.Root)
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

// Unwrap returns the underlying error for errors.Is/As support.
func (e *BoundaryError) Unwrap() error { return e.Err }

// Is reports whether e matches target.
// A BoundaryError matches ErrBoundaryViolation, or another *BoundaryError with the same Op+Path+Root.
func (e *BoundaryError) Is(target error) bool {
	if target == ErrBoundaryViolation {
		return true
	}
	t, ok := target.(*BoundaryError)
	if !ok {
		return false
	}
	return (t.Op == "" || e.Op == t.Op) &&
		(t.Path == "" || e.Path == t.Path) &&
		(t.Root == "" || e.Root == t.Root)
}

// NewBoundary creates a BoundaryError for the given operation and paths.
func NewBoundary(op, path, root string, err error) *BoundaryError {
	return &BoundaryError{Op: op, Path: path, Root: root, Err: err}
}

// GitError represents a structured git operation failure.
// It consolidates project.GitError and similar typed errors
// into a single canonical type for the central errors package.
type GitError struct {
	// Op is the git operation that failed (e.g., "commit", "submodule add", "checkout").
	Op string
	// Path is the repository or file path involved (empty if not applicable).
	Path string
	// ErrType classifies the error category: "lock", "network", "permission",
	// "not_repo", "no_changes", "submodule", or "unknown".
	ErrType string
	// Detail is the trimmed stderr output or a human-readable explanation.
	Detail string
	// Err is the underlying error, if any.
	Err error
}

// Error implements the error interface.
func (e *GitError) Error() string {
	var b strings.Builder
	b.WriteString("git ")
	b.WriteString(e.Op)
	b.WriteString(" failed")
	if e.ErrType != "" && e.ErrType != "unknown" {
		b.WriteString(" (")
		b.WriteString(e.ErrType)
		b.WriteString(")")
	}
	if e.Path != "" {
		b.WriteString(": ")
		b.WriteString(e.Path)
	}
	if e.Detail != "" {
		b.WriteString(": ")
		b.WriteString(e.Detail)
	}
	return b.String()
}

// Unwrap returns the underlying error for errors.Is/As support.
func (e *GitError) Unwrap() error { return e.Err }

// Is reports whether e matches target.
// A GitError matches ErrGitFailed, or another *GitError with matching Op and ErrType.
func (e *GitError) Is(target error) bool {
	if target == ErrGitFailed {
		return true
	}
	t, ok := target.(*GitError)
	if !ok {
		return false
	}
	return (t.Op == "" || e.Op == t.Op) &&
		(t.ErrType == "" || e.ErrType == t.ErrType)
}

// NewGit creates a GitError with the given operation context.
func NewGit(op, path, errType, detail string, err error) *GitError {
	return &GitError{Op: op, Path: path, ErrType: errType, Detail: detail, Err: err}
}

// CommandError represents a subprocess execution failure.
// It covers both launch failures (Err set, ExitCode 0) and
// non-zero exit codes (ExitCode non-zero, Err may be set).
type CommandError struct {
	// Command is the command name or full command string that failed.
	Command string
	// ExitCode is the process exit code (0 if the command could not be launched).
	ExitCode int
	// Stderr is the captured standard error output, if available.
	Stderr string
	// Err is the underlying error (exec.ExitError, exec.Error, or a wrapped error).
	Err error
}

// Error implements the error interface.
func (e *CommandError) Error() string {
	if e.ExitCode != 0 {
		msg := fmt.Sprintf("command %q exited with code %d", e.Command, e.ExitCode)
		if e.Stderr != "" {
			msg += ": " + strings.TrimSpace(e.Stderr)
		}
		return msg
	}
	if e.Err != nil {
		return fmt.Sprintf("failed to execute %q: %v", e.Command, e.Err)
	}
	return fmt.Sprintf("command %q failed", e.Command)
}

// Unwrap returns the underlying error for errors.Is/As support.
func (e *CommandError) Unwrap() error { return e.Err }

// Is reports whether e matches target.
// A CommandError matches ErrCommandFailed, or another *CommandError with the same Command and ExitCode.
func (e *CommandError) Is(target error) bool {
	if target == ErrCommandFailed {
		return true
	}
	t, ok := target.(*CommandError)
	if !ok {
		return false
	}
	return (t.Command == "" || e.Command == t.Command) &&
		(t.ExitCode == 0 || e.ExitCode == t.ExitCode)
}

// NewCommand creates a CommandError.
// Use exitCode=0 and a non-nil err for launch failures.
// Use exitCode!=0 for process exit-code failures.
func NewCommand(command string, exitCode int, stderr string, err error) *CommandError {
	return &CommandError{Command: command, ExitCode: exitCode, Stderr: stderr, Err: err}
}
