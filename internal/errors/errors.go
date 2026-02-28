// Package errors provides typed error types, sentinel errors, and wrapping
// utilities for the camp CLI.
package errors

import "fmt"

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
