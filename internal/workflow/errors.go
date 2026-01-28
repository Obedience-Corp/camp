package workflow

import "errors"

// Package-level errors for workflow operations.
var (
	// ErrNoSchema is returned when no .workflow.yaml file is found.
	ErrNoSchema = errors.New("no workflow schema found")

	// ErrSchemaExists is returned when trying to init an already-initialized workflow.
	ErrSchemaExists = errors.New("workflow already initialized")

	// ErrInvalidSchema is returned when the schema file is malformed.
	ErrInvalidSchema = errors.New("invalid workflow schema")

	// ErrInvalidTransition is returned when a move violates transition rules.
	ErrInvalidTransition = errors.New("transition not allowed")

	// ErrItemNotFound is returned when an item doesn't exist.
	ErrItemNotFound = errors.New("item not found")

	// ErrInvalidStatus is returned when a status path is invalid.
	ErrInvalidStatus = errors.New("invalid status")

	// ErrStatusNotFound is returned when a status directory doesn't exist.
	ErrStatusNotFound = errors.New("status not found")

	// ErrAlreadyExists is returned when an item already exists at the destination.
	ErrAlreadyExists = errors.New("item already exists at destination")
)
