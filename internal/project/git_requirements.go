package project

import (
	"fmt"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// NonGitOperationError indicates a linked non-git project was used with a git-only operation.
type NonGitOperationError struct {
	Name      string
	Operation string
}

// Error implements the error interface.
func (e *NonGitOperationError) Error() string {
	return fmt.Sprintf("project %q is a linked non-git directory and does not support %s", e.Name, e.Operation)
}

// Unwrap returns ErrInvalidInput so errors.Is matches the canonical camp category.
func (e *NonGitOperationError) Unwrap() error {
	return camperrors.ErrInvalidInput
}

// RequireGit rejects operations that require a git repository when the resolved
// project is a linked non-git directory.
func (r *ResolveResult) RequireGit(operation string) error {
	if r == nil {
		return camperrors.NewValidation("project", "project resolution result is required", nil)
	}
	if r.Source != SourceLinkedNonGit {
		return nil
	}
	return &NonGitOperationError{
		Name:      r.Name,
		Operation: operation,
	}
}
