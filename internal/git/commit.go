package git

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
)

const maxRetryAttempts = 3

// CommitOptions configures the commit operation.
type CommitOptions struct {
	Message    string
	Amend      bool
	AllowEmpty bool
	Author     string // Optional: "Name <email>"
}

// Validate checks if options are valid.
func (o *CommitOptions) Validate() error {
	if o.Message == "" && !o.Amend {
		return fmt.Errorf("commit message is required")
	}
	return nil
}

// Commit creates a git commit with automatic lock handling.
func Commit(ctx context.Context, repoPath string, opts *CommitOptions) error {
	if opts == nil {
		return fmt.Errorf("commit options required")
	}
	if err := opts.Validate(); err != nil {
		return err
	}

	var lastErr error
	for attempt := 1; attempt <= maxRetryAttempts; attempt++ {
		err := executeCommit(ctx, repoPath, opts)
		if err == nil {
			return nil // Success
		}

		// Check if it's a lock error
		if !isLockError(err) {
			return err // Non-lock error, don't retry
		}

		lastErr = err

		// Try to clean stale locks
		result, cleanErr := CleanStaleLocks(ctx, repoPath, nil)
		if cleanErr != nil {
			return fmt.Errorf("failed to clean locks during commit retry (attempt %d): %w", attempt, cleanErr)
		}

		// If we couldn't remove any locks, don't retry
		if len(result.Removed) == 0 && len(result.Skipped) > 0 {
			return fmt.Errorf("cannot commit: lock held by active process: %w", lastErr)
		}

		// Log retry
		slog.Info("retrying commit after lock cleanup",
			"attempt", attempt,
			"removed", len(result.Removed),
			"repoPath", repoPath)
	}

	return fmt.Errorf("commit failed after %d attempts: %w", maxRetryAttempts, lastErr)
}

// executeCommit runs the actual git commit command.
func executeCommit(ctx context.Context, repoPath string, opts *CommitOptions) error {
	args := []string{"-C", repoPath, "commit"}

	if opts.Amend {
		args = append(args, "--amend")
	}
	if opts.AllowEmpty {
		args = append(args, "--allow-empty")
	}
	if opts.Author != "" {
		args = append(args, "--author", opts.Author)
	}
	if opts.Message != "" {
		args = append(args, "-m", opts.Message)
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	output, err := cmd.CombinedOutput()

	if err != nil {
		errType := ClassifyGitError(string(output), cmd.ProcessState.ExitCode())

		switch errType {
		case GitErrorNoChanges:
			return ErrNoChanges
		case GitErrorLock:
			return &LockError{
				Path: "index.lock", // Will be found by cleanup
				Err:  err,
			}
		default:
			return fmt.Errorf("git commit failed (type=%s): %s: %w",
				errType.String(),
				strings.TrimSpace(string(output)),
				err)
		}
	}

	return nil
}

// isLockError checks if an error is a lock-related error.
func isLockError(err error) bool {
	var lockErr *LockError
	return errors.As(err, &lockErr)
}

// CommitAll stages all changes and commits with the given message.
func CommitAll(ctx context.Context, repoPath, message string) error {
	// Stage all changes first
	if err := StageAll(ctx, repoPath); err != nil {
		return fmt.Errorf("failed to stage changes: %w", err)
	}

	// Check if there's anything to commit
	hasChanges, err := HasStagedChanges(ctx, repoPath)
	if err != nil {
		return err
	}
	if !hasChanges {
		return ErrNoChanges
	}

	// Commit
	return Commit(ctx, repoPath, &CommitOptions{Message: message})
}

// Stage adds files to the git index (staging area).
// If files is empty, stages all changes (git add .).
func Stage(ctx context.Context, repoPath string, files []string) error {
	var args []string

	// Use -C to run git in the specified directory
	args = append(args, "-C", repoPath, "add")

	if len(files) == 0 {
		// Stage all changes
		args = append(args, ".")
	} else {
		// Stage specific files
		args = append(args, files...)
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	output, err := cmd.CombinedOutput()

	if err != nil {
		errType := ClassifyGitError(string(output), cmd.ProcessState.ExitCode())
		return fmt.Errorf("git add failed (type=%s): %s: %w",
			errType.String(),
			strings.TrimSpace(string(output)),
			err)
	}

	return nil
}

// StageAll is a convenience function to stage all changes.
func StageAll(ctx context.Context, repoPath string) error {
	return Stage(ctx, repoPath, nil)
}

// StageFiles stages specific files.
func StageFiles(ctx context.Context, repoPath string, files ...string) error {
	if len(files) == 0 {
		return fmt.Errorf("no files specified for staging in %s", repoPath)
	}
	return Stage(ctx, repoPath, files)
}

// HasStagedChanges checks if there are any staged changes ready to commit.
func HasStagedChanges(ctx context.Context, repoPath string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "diff", "--cached", "--quiet")
	err := cmd.Run()

	if err != nil {
		// Exit code 1 means there are differences (staged changes)
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				return true, nil
			}
		}
		return false, fmt.Errorf("git diff --cached failed: %w", err)
	}

	// Exit code 0 means no differences (nothing staged)
	return false, nil
}

// HasUnstagedChanges checks if there are any unstaged changes.
func HasUnstagedChanges(ctx context.Context, repoPath string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "diff", "--quiet")
	err := cmd.Run()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				return true, nil
			}
		}
		return false, fmt.Errorf("git diff failed: %w", err)
	}

	return false, nil
}

// HasUntrackedFiles checks if there are any untracked files.
func HasUntrackedFiles(ctx context.Context, repoPath string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "ls-files", "--others", "--exclude-standard")
	output, err := cmd.Output()

	if err != nil {
		return false, fmt.Errorf("git ls-files failed: %w", err)
	}

	return len(strings.TrimSpace(string(output))) > 0, nil
}

// HasChanges checks if there are any staged, unstaged, or untracked changes.
func HasChanges(ctx context.Context, repoPath string) (bool, error) {
	staged, err := HasStagedChanges(ctx, repoPath)
	if err != nil {
		return false, err
	}
	if staged {
		return true, nil
	}

	unstaged, err := HasUnstagedChanges(ctx, repoPath)
	if err != nil {
		return false, err
	}
	if unstaged {
		return true, nil
	}

	untracked, err := HasUntrackedFiles(ctx, repoPath)
	if err != nil {
		return false, err
	}

	return untracked, nil
}
