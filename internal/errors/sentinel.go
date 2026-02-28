package errors

import "errors"

// Sentinel errors for common error categories.
var (
	// ErrNotFound indicates a resource could not be located.
	ErrNotFound = errors.New("not found")

	// ErrAlreadyExists indicates a resource already exists.
	ErrAlreadyExists = errors.New("already exists")

	// ErrInvalidInput indicates input failed validation.
	ErrInvalidInput = errors.New("invalid input")

	// ErrPermission indicates an operation was not permitted.
	ErrPermission = errors.New("permission denied")

	// ErrTimeout indicates an operation exceeded its deadline.
	ErrTimeout = errors.New("operation timed out")

	// ErrCancelled indicates an operation was cancelled.
	ErrCancelled = errors.New("operation cancelled")

	// ErrConflict indicates a conflicting state or resource.
	ErrConflict = errors.New("conflict")

	// ErrNotInitialized indicates a required resource has not been set up.
	ErrNotInitialized = errors.New("not initialized")
)
