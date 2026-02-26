package project

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/Obedience-Corp/camp/internal/git"
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
	cfg := git.SubmoduleRetryConfig()
	cfg.OperationName = "submodule deinit"

	return git.WithLockRetry(ctx, campaignRoot, cfg, func() error {
		cmd := exec.CommandContext(ctx, "git", "submodule", "deinit", "-f", submodulePath)
		cmd.Dir = campaignRoot
		output, err := cmd.CombinedOutput()
		if err != nil {
			// Check if it's a lock error
			errType := git.ClassifyGitError(string(output), cmd.ProcessState.ExitCode())
			if errType == git.GitErrorLock {
				return &git.LockError{Path: "index.lock", Err: err}
			}
			return fmt.Errorf("submodule deinit failed: %w", err)
		}
		return nil
	})
}

// executeSubmoduleRm runs git rm with lock handling.
func executeSubmoduleRm(ctx context.Context, campaignRoot, submodulePath string) error {
	cfg := git.SubmoduleRetryConfig()
	cfg.OperationName = "git rm"

	return git.WithLockRetry(ctx, campaignRoot, cfg, func() error {
		cmd := exec.CommandContext(ctx, "git", "rm", "-f", submodulePath)
		cmd.Dir = campaignRoot
		output, err := cmd.CombinedOutput()
		if err != nil {
			// Check if it's a lock error
			errType := git.ClassifyGitError(string(output), cmd.ProcessState.ExitCode())
			if errType == git.GitErrorLock {
				return &git.LockError{Path: "index.lock", Err: err}
			}
			return fmt.Errorf("git rm failed: %w", err)
		}
		return nil
	})
}
