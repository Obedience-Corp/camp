package worktree

import (
	"errors"
	"fmt"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// Error codes for worktree operations.
const (
	ErrCodeProjectNotFound  = "WORKTREE_PROJECT_NOT_FOUND"
	ErrCodeWorktreeExists   = "WORKTREE_ALREADY_EXISTS"
	ErrCodeWorktreeNotFound = "WORKTREE_NOT_FOUND"
	ErrCodeBranchNotFound   = "WORKTREE_BRANCH_NOT_FOUND"
	ErrCodeInvalidName      = "WORKTREE_INVALID_NAME"
	ErrCodeGitFailed        = "WORKTREE_GIT_FAILED"
	ErrCodeStaleWorktree    = "WORKTREE_STALE"
	ErrCodeCorrupted        = "WORKTREE_CORRUPTED"
	ErrCodeNotInWorktree    = "WORKTREE_NOT_IN_WORKTREE"
	ErrCodeRemovalFailed    = "WORKTREE_REMOVAL_FAILED"
)

// Sentinel errors for common cases.
// Sentinels marked with %w wrap the canonical sentinel from internal/errors
// to enable cross-package errors.Is() matching.
var (
	ErrProjectNotFound  = camperrors.Wrap(camperrors.ErrNotFound, "project not found")
	ErrWorktreeExists   = camperrors.Wrap(camperrors.ErrAlreadyExists, "worktree already exists")
	ErrWorktreeNotFound = camperrors.Wrap(camperrors.ErrNotFound, "worktree not found")
	ErrBranchNotFound   = camperrors.Wrap(camperrors.ErrNotFound, "branch not found")
	ErrInvalidName      = camperrors.Wrap(camperrors.ErrInvalidInput, "invalid worktree name")
	ErrNotInWorktree    = errors.New("not inside a worktree")
	ErrStaleWorktree    = errors.New("worktree is stale")
	ErrCorrupted        = errors.New("worktree is corrupted")
)

// WorktreeError wraps worktree-specific errors with context.
type WorktreeError struct {
	Code     string
	Project  string
	Worktree string
	Branch   string
	Path     string
	Cause    error
}

func (e *WorktreeError) Error() string {
	msg := fmt.Sprintf("worktree error [%s]", e.Code)
	if e.Project != "" {
		msg += fmt.Sprintf(" project=%s", e.Project)
	}
	if e.Worktree != "" {
		msg += fmt.Sprintf(" worktree=%s", e.Worktree)
	}
	if e.Cause != nil {
		msg += fmt.Sprintf(": %v", e.Cause)
	}
	return msg
}

func (e *WorktreeError) Unwrap() error {
	return e.Cause
}

// NewError creates a WorktreeError with the given code.
func NewError(code string) *WorktreeError {
	return &WorktreeError{Code: code}
}

// WithProject adds project context.
func (e *WorktreeError) WithProject(project string) *WorktreeError {
	e.Project = project
	return e
}

// WithWorktree adds worktree context.
func (e *WorktreeError) WithWorktree(name string) *WorktreeError {
	e.Worktree = name
	return e
}

// WithBranch adds branch context.
func (e *WorktreeError) WithBranch(branch string) *WorktreeError {
	e.Branch = branch
	return e
}

// WithPath adds path context.
func (e *WorktreeError) WithPath(path string) *WorktreeError {
	e.Path = path
	return e
}

// WithCause wraps an underlying error.
func (e *WorktreeError) WithCause(err error) *WorktreeError {
	e.Cause = err
	return e
}

// Helper constructors for common errors.

// ProjectNotFound creates an error for missing project.
func ProjectNotFound(project string) error {
	return NewError(ErrCodeProjectNotFound).
		WithProject(project).
		WithCause(ErrProjectNotFound)
}

// WorktreeAlreadyExists creates an error for duplicate worktree.
func WorktreeAlreadyExists(project, worktree string) error {
	return NewError(ErrCodeWorktreeExists).
		WithProject(project).
		WithWorktree(worktree).
		WithCause(ErrWorktreeExists)
}

// WorktreeNotFoundError creates an error for missing worktree.
func WorktreeNotFoundError(project, worktree string) error {
	return NewError(ErrCodeWorktreeNotFound).
		WithProject(project).
		WithWorktree(worktree).
		WithCause(ErrWorktreeNotFound)
}

// BranchNotFoundError creates an error for missing branch.
func BranchNotFoundError(project, branch string) error {
	return NewError(ErrCodeBranchNotFound).
		WithProject(project).
		WithBranch(branch).
		WithCause(ErrBranchNotFound)
}

// InvalidWorktreeName creates an error for invalid name.
func InvalidWorktreeName(name, reason string) error {
	return NewError(ErrCodeInvalidName).
		WithWorktree(name).
		WithCause(camperrors.Wrap(ErrInvalidName, reason))
}

// GitOperationFailed wraps a git command failure.
func GitOperationFailed(project, operation string, err error) error {
	return NewError(ErrCodeGitFailed).
		WithProject(project).
		WithCause(camperrors.Wrap(err, operation))
}

// StaleWorktreeError creates an error for a stale worktree.
func StaleWorktreeError(project, worktree, reason string) error {
	return NewError(ErrCodeStaleWorktree).
		WithProject(project).
		WithWorktree(worktree).
		WithCause(camperrors.Wrap(ErrStaleWorktree, reason))
}

// RemovalFailed creates an error for worktree removal failure.
func RemovalFailed(project, worktree string, err error) error {
	return NewError(ErrCodeRemovalFailed).
		WithProject(project).
		WithWorktree(worktree).
		WithCause(err)
}
