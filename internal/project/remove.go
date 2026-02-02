package project

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/obediencecorp/camp/internal/git"
)

// RemoveOptions configures the project removal behavior.
type RemoveOptions struct {
	// Delete also removes project files (destructive).
	Delete bool
	// Force skips confirmation prompts.
	Force bool
	// DryRun shows what would be done without making changes.
	DryRun bool
}

// RemoveResult contains information about what was removed.
type RemoveResult struct {
	// Name is the project name.
	Name string
	// Path is the project path.
	Path string
	// SubmoduleRemoved indicates if git submodule was deinitialized.
	SubmoduleRemoved bool
	// FilesDeleted indicates if project files were deleted.
	FilesDeleted bool
	// WorktreeDeleted indicates if worktree directory was deleted.
	WorktreeDeleted bool
}

// ErrProjectNotFound is returned when a project doesn't exist.
type ErrProjectNotFound struct {
	Name string
}

func (e *ErrProjectNotFound) Error() string {
	return fmt.Sprintf("project not found: %s", e.Name)
}

// Remove removes a project from the campaign.
// By default it only removes git submodule tracking.
// With Delete=true, it also removes all files.
func Remove(ctx context.Context, campaignRoot, name string, opts RemoveOptions) (*RemoveResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	projectPath := filepath.Join(campaignRoot, "projects", name)

	// Check project exists
	if _, err := os.Stat(projectPath); os.IsNotExist(err) {
		return nil, &ErrProjectNotFound{Name: name}
	}

	result := &RemoveResult{
		Name: name,
		Path: projectPath,
	}

	// Dry run just reports what would happen
	if opts.DryRun {
		result.SubmoduleRemoved = true
		if opts.Delete {
			result.FilesDeleted = true
			worktreePath := filepath.Join(campaignRoot, "worktrees", name)
			if _, err := os.Stat(worktreePath); err == nil {
				result.WorktreeDeleted = true
			}
		}
		return result, nil
	}

	// Check if it's a git submodule
	isSubmodule, err := isGitSubmodule(ctx, campaignRoot, name)
	if err != nil {
		return nil, err
	}

	// Remove from git submodules if applicable
	if isSubmodule {
		if err := removeSubmodule(ctx, campaignRoot, name); err != nil {
			return nil, fmt.Errorf("failed to remove submodule: %w", err)
		}
		result.SubmoduleRemoved = true
	}

	// Delete files if requested
	if opts.Delete {
		if err := os.RemoveAll(projectPath); err != nil {
			return nil, fmt.Errorf("failed to delete project files: %w", err)
		}
		result.FilesDeleted = true

		// Also remove worktree directory if exists
		worktreePath := filepath.Join(campaignRoot, "worktrees", name)
		if _, err := os.Stat(worktreePath); err == nil {
			if err := os.RemoveAll(worktreePath); err == nil {
				result.WorktreeDeleted = true
			}
		}

		// Clean up .git/modules/<name>
		modulesPath := filepath.Join(campaignRoot, ".git", "modules", "projects", name)
		os.RemoveAll(modulesPath) // Ignore errors
	}

	return result, nil
}

// isGitSubmodule checks if a project is registered as a git submodule.
func isGitSubmodule(ctx context.Context, campaignRoot, name string) (bool, error) {
	gitmodulesPath := filepath.Join(campaignRoot, ".gitmodules")
	if _, err := os.Stat(gitmodulesPath); os.IsNotExist(err) {
		return false, nil
	}

	// Check if submodule is registered
	cmd := exec.CommandContext(ctx, "git", "config", "--file", ".gitmodules",
		"--get", fmt.Sprintf("submodule.projects/%s.path", name))
	cmd.Dir = campaignRoot
	if err := cmd.Run(); err != nil {
		// Error means not found, which is OK
		return false, nil
	}

	return true, nil
}

// removeSubmodule properly removes a git submodule.
// It handles stale and active lock files with intelligent retry logic:
// - Stale locks are removed immediately
// - Active locks are waited on (up to 5 seconds) before retrying
// - Exponential backoff between retry attempts
func removeSubmodule(ctx context.Context, campaignRoot, name string) error {
	submodulePath := filepath.Join("projects", name)

	// Execute deinit with lock handling
	if err := executeSubmoduleDeinit(ctx, campaignRoot, submodulePath); err != nil {
		return err
	}

	// Execute rm with lock handling
	if err := executeSubmoduleRm(ctx, campaignRoot, submodulePath); err != nil {
		return err
	}

	return nil
}

// executeSubmoduleDeinit runs git submodule deinit with lock handling.
func executeSubmoduleDeinit(ctx context.Context, campaignRoot, submodulePath string) error {
	var lastErr error
	backoff := initialBackoff

	for attempt := 1; attempt <= maxRetryAttempts; attempt++ {
		cmd := exec.CommandContext(ctx, "git", "submodule", "deinit", "-f", submodulePath)
		cmd.Dir = campaignRoot
		output, err := cmd.CombinedOutput()
		if err == nil {
			return nil
		}

		// Check if it's a lock error
		errType := git.ClassifyGitError(string(output), cmd.ProcessState.ExitCode())
		if errType != git.GitErrorLock {
			return fmt.Errorf("submodule deinit failed: %w", err)
		}

		lastErr = err

		// Try to clean stale locks
		result, cleanErr := git.CleanStaleLocks(ctx, campaignRoot, nil)
		if cleanErr != nil {
			return fmt.Errorf("failed to clean locks (attempt %d): %w", attempt, cleanErr)
		}

		// If we removed stale locks, retry after brief delay
		if len(result.Removed) > 0 {
			slog.Info("retrying deinit after stale lock cleanup",
				"attempt", attempt,
				"removed", len(result.Removed),
				"path", submodulePath)
			time.Sleep(backoff)
			backoff = min(backoff*2, maxBackoff)
			continue
		}

		// If active locks found, wait for them to release
		if len(result.Skipped) > 0 {
			slog.Info("waiting for active lock to release",
				"attempt", attempt,
				"active_locks", len(result.Skipped),
				"path", submodulePath)

			waitErr := git.WaitForLockRelease(ctx, result.Skipped[0].Path, activeLockWaitTime, slog.Default())
			if waitErr == nil {
				git.CleanStaleLocks(ctx, campaignRoot, nil)
				continue
			}

			if attempt < maxRetryAttempts {
				slog.Warn("lock wait timeout, will retry",
					"attempt", attempt,
					"pid", result.Skipped[0].ProcessID,
					"path", result.Skipped[0].Path)
				time.Sleep(backoff)
				backoff = min(backoff*2, maxBackoff)
				continue
			}

			return fmt.Errorf("submodule deinit failed: lock held by active process (PID %d) after waiting: %w",
				result.Skipped[0].ProcessID, lastErr)
		}

		slog.Info("retrying submodule deinit",
			"attempt", attempt,
			"path", submodulePath)
		time.Sleep(backoff)
		backoff = min(backoff*2, maxBackoff)
	}

	return fmt.Errorf("submodule deinit failed after %d attempts: %w", maxRetryAttempts, lastErr)
}

// executeSubmoduleRm runs git rm with lock handling.
func executeSubmoduleRm(ctx context.Context, campaignRoot, submodulePath string) error {
	var lastErr error
	backoff := initialBackoff

	for attempt := 1; attempt <= maxRetryAttempts; attempt++ {
		cmd := exec.CommandContext(ctx, "git", "rm", "-f", submodulePath)
		cmd.Dir = campaignRoot
		output, err := cmd.CombinedOutput()
		if err == nil {
			return nil
		}

		// Check if it's a lock error
		errType := git.ClassifyGitError(string(output), cmd.ProcessState.ExitCode())
		if errType != git.GitErrorLock {
			return fmt.Errorf("git rm failed: %w", err)
		}

		lastErr = err

		// Try to clean stale locks
		result, cleanErr := git.CleanStaleLocks(ctx, campaignRoot, nil)
		if cleanErr != nil {
			return fmt.Errorf("failed to clean locks (attempt %d): %w", attempt, cleanErr)
		}

		// If we removed stale locks, retry after brief delay
		if len(result.Removed) > 0 {
			slog.Info("retrying git rm after stale lock cleanup",
				"attempt", attempt,
				"removed", len(result.Removed),
				"path", submodulePath)
			time.Sleep(backoff)
			backoff = min(backoff*2, maxBackoff)
			continue
		}

		// If active locks found, wait for them to release
		if len(result.Skipped) > 0 {
			slog.Info("waiting for active lock to release",
				"attempt", attempt,
				"active_locks", len(result.Skipped),
				"path", submodulePath)

			waitErr := git.WaitForLockRelease(ctx, result.Skipped[0].Path, activeLockWaitTime, slog.Default())
			if waitErr == nil {
				git.CleanStaleLocks(ctx, campaignRoot, nil)
				continue
			}

			if attempt < maxRetryAttempts {
				slog.Warn("lock wait timeout, will retry",
					"attempt", attempt,
					"pid", result.Skipped[0].ProcessID,
					"path", result.Skipped[0].Path)
				time.Sleep(backoff)
				backoff = min(backoff*2, maxBackoff)
				continue
			}

			return fmt.Errorf("git rm failed: lock held by active process (PID %d) after waiting: %w",
				result.Skipped[0].ProcessID, lastErr)
		}

		slog.Info("retrying git rm",
			"attempt", attempt,
			"path", submodulePath)
		time.Sleep(backoff)
		backoff = min(backoff*2, maxBackoff)
	}

	return fmt.Errorf("git rm failed after %d attempts: %w", maxRetryAttempts, lastErr)
}
