package git

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
)

// GitExecutor defines the interface for git operations.
// This enables mocking in tests.
type GitExecutor interface {
	// Commit creates a commit with the given options
	Commit(ctx context.Context, opts *CommitOptions) error

	// CommitAll stages all changes and commits
	CommitAll(ctx context.Context, message string) error

	// Stage adds files to the staging area
	Stage(ctx context.Context, files []string) error

	// StageAll stages all changes
	StageAll(ctx context.Context) error

	// StageAllExcludingSubmodules stages all changes but excludes submodule refs
	StageAllExcludingSubmodules(ctx context.Context) error

	// HasChanges returns true if there are uncommitted changes
	HasChanges(ctx context.Context) (bool, error)

	// CleanLocks removes stale lock files
	CleanLocks(ctx context.Context) (*RemovalResult, error)

	// Path returns the repository path
	Path() string
}

// Executor implements GitExecutor for a specific repository.
type Executor struct {
	path   string
	logger *slog.Logger
	config *ExecutorConfig
}

// ExecutorConfig contains configuration for the executor.
type ExecutorConfig struct {
	// AutoStage automatically stages all changes before commit
	AutoStage bool

	// MaxRetries for lock-related failures
	MaxRetries int

	// Verbose enables detailed logging
	Verbose bool
}

// DefaultExecutorConfig returns sensible defaults.
func DefaultExecutorConfig() *ExecutorConfig {
	return &ExecutorConfig{
		AutoStage:  true,
		MaxRetries: 3,
		Verbose:    false,
	}
}

// ExecutorOption is a functional option for configuring Executor.
type ExecutorOption func(*Executor)

// WithLogger sets a custom logger.
func WithLogger(logger *slog.Logger) ExecutorOption {
	return func(e *Executor) {
		e.logger = logger
	}
}

// WithConfig sets executor configuration.
func WithConfig(cfg *ExecutorConfig) ExecutorOption {
	return func(e *Executor) {
		e.config = cfg
	}
}

// NewExecutor creates an Executor for the given repository path.
func NewExecutor(path string, opts ...ExecutorOption) (*Executor, error) {
	// Validate path is a git repository
	root, err := FindProjectRoot(path)
	if err != nil {
		return nil, fmt.Errorf("path is not a git repository %s: %w", path, err)
	}

	e := &Executor{
		path:   root,
		logger: slog.Default(),
		config: DefaultExecutorConfig(),
	}

	for _, opt := range opts {
		opt(e)
	}

	return e, nil
}

// Path returns the repository path.
func (e *Executor) Path() string {
	return e.path
}

// Logger returns the executor's logger.
func (e *Executor) Logger() *slog.Logger {
	return e.logger
}

// Config returns the executor's configuration.
func (e *Executor) Config() *ExecutorConfig {
	return e.config
}

// Commit creates a commit with automatic lock handling.
func (e *Executor) Commit(ctx context.Context, opts *CommitOptions) error {
	if e.config.Verbose {
		e.logger.Debug("executing commit",
			"path", e.path,
			"message", opts.Message,
			"amend", opts.Amend)
	}

	err := Commit(ctx, e.path, opts)
	if err != nil {
		e.logger.Error("commit failed",
			"path", e.path,
			"error", err)
		return err
	}

	if e.config.Verbose {
		e.logger.Info("commit successful", "path", e.path)
	}
	return nil
}

// CommitAll stages all changes and commits.
func (e *Executor) CommitAll(ctx context.Context, message string) error {
	if e.config.AutoStage {
		if err := e.StageAll(ctx); err != nil {
			return err
		}
	}

	return e.Commit(ctx, &CommitOptions{Message: message})
}

// Stage adds files to the staging area.
func (e *Executor) Stage(ctx context.Context, files []string) error {
	if e.config.Verbose {
		e.logger.Debug("staging files",
			"path", e.path,
			"count", len(files))
	}

	return Stage(ctx, e.path, files)
}

// StageAll stages all changes.
func (e *Executor) StageAll(ctx context.Context) error {
	return e.Stage(ctx, nil)
}

// StageAllExcludingSubmodules stages all changes but excludes submodule ref updates.
func (e *Executor) StageAllExcludingSubmodules(ctx context.Context) error {
	if e.config.Verbose {
		e.logger.Debug("staging all excluding submodules", "path", e.path)
	}
	return StageAllExcludingSubmodules(ctx, e.path)
}

// HasChanges returns true if there are uncommitted changes.
func (e *Executor) HasChanges(ctx context.Context) (bool, error) {
	hasStaged, err := HasStagedChanges(ctx, e.path)
	if err != nil {
		return false, err
	}
	if hasStaged {
		return true, nil
	}

	hasUnstaged, err := HasUnstagedChanges(ctx, e.path)
	if err != nil {
		return false, err
	}
	if hasUnstaged {
		return true, nil
	}

	return HasUntrackedFiles(ctx, e.path)
}

// CleanLocks removes stale lock files.
func (e *Executor) CleanLocks(ctx context.Context) (*RemovalResult, error) {
	if e.config.Verbose {
		e.logger.Debug("cleaning locks", "path", e.path)
	}
	return CleanStaleLocks(ctx, e.path, e.logger)
}

// SubmoduleExecutor wraps Executor with submodule-specific behavior.
type SubmoduleExecutor struct {
	*Executor
	info *SubmoduleInfo
}

// NewSubmoduleExecutor creates an executor for a submodule.
func NewSubmoduleExecutor(path string, opts ...ExecutorOption) (*SubmoduleExecutor, error) {
	info, err := GetSubmoduleInfo(path)
	if err != nil {
		return nil, err
	}

	exec, err := NewExecutor(info.Path, opts...)
	if err != nil {
		return nil, err
	}

	return &SubmoduleExecutor{
		Executor: exec,
		info:     info,
	}, nil
}

// Info returns submodule information.
func (e *SubmoduleExecutor) Info() *SubmoduleInfo {
	return e.info
}

// ParentNeedsCommit checks if parent repo shows this submodule as modified.
func (e *SubmoduleExecutor) ParentNeedsCommit(ctx context.Context) (bool, error) {
	if e.info.ParentRepo == "" {
		return false, nil
	}

	// Check if submodule is modified in parent
	cmd := exec.CommandContext(ctx, "git", "-C", e.info.ParentRepo,
		"diff", "--quiet", "--", e.info.Path)
	err := cmd.Run()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return true, nil // Submodule modified in parent
		}
		return false, fmt.Errorf("failed to check parent status: %w", err)
	}

	return false, nil
}

// Ensure Executor implements GitExecutor.
var _ GitExecutor = (*Executor)(nil)
