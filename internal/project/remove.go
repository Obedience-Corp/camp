package project

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/pathutil"
)

// ErrDirtyProject is returned when a submodule has uncommitted changes and
// --force was not specified.
var ErrDirtyProject = errors.New("project has uncommitted changes")

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
	// Steps records each completed operation for visibility.
	Steps []string
	// RecoveryInstructions describes manual steps needed if removal was partial.
	RecoveryInstructions []string
}

func addStep(result *RemoveResult, msg string) {
	result.Steps = append(result.Steps, msg)
}

// ErrProjectNotFound is returned when a project doesn't exist.
type ErrProjectNotFound struct {
	Name string
}

func (e *ErrProjectNotFound) Error() string {
	return fmt.Sprintf("project not found: %s", e.Name)
}

// Unwrap returns ErrNotFound so errors.Is(err, camperrors.ErrNotFound) works.
func (e *ErrProjectNotFound) Unwrap() error {
	return camperrors.ErrNotFound
}

// Remove removes a project from the campaign.
// By default it only removes git submodule tracking.
// With Delete=true, it also removes all files.
func Remove(ctx context.Context, campaignRoot, name string, opts RemoveOptions) (*RemoveResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if err := ValidateProjectName(name); err != nil {
		return nil, err
	}

	projectPath := filepath.Join(campaignRoot, "projects", name)

	// Enforce boundary: projectPath must stay within campaignRoot.
	if err := pathutil.ValidateBoundary(campaignRoot, projectPath); err != nil {
		return nil, camperrors.Wrap(err, "project path boundary violation")
	}

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
		addStep(result, "would deinit and remove submodule from git tracking")
		result.SubmoduleRemoved = true
		if opts.Delete {
			addStep(result, fmt.Sprintf("would delete project directory %s", projectPath))
			result.FilesDeleted = true
			worktreePath := campaignProjectWorktreePath(ctx, campaignRoot, name)
			if _, err := os.Stat(worktreePath); err == nil {
				addStep(result, fmt.Sprintf("would delete worktree directory %s", worktreePath))
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
		if !opts.Force {
			dirty, changed, dirtyErr := isDirtySubmodule(ctx, projectPath)
			if dirtyErr != nil {
				return nil, camperrors.Wrap(dirtyErr, "dirty check failed")
			}
			if dirty {
				msg := fmt.Sprintf(
					"project %q has uncommitted changes; commit, stash, or pass --force to override:\n  %s",
					name, strings.Join(changed, "\n  "),
				)
				return nil, camperrors.Wrap(ErrDirtyProject, msg)
			}
		}

		if err := removeSubmodule(ctx, campaignRoot, name); err != nil {
			result.RecoveryInstructions = buildRecoveryInstructions(result, campaignRoot, name, opts)
			return result, camperrors.Wrap(err, "failed to remove submodule")
		}
		addStep(result, "submodule deinitialized and removed from index")
		result.SubmoduleRemoved = true

		// Always clean up .git/modules after a successful submodule removal,
		// regardless of whether --delete was requested. Leaving stale module
		// metadata causes re-add failures.
		modulesPath := filepath.Join(campaignRoot, ".git", "modules", "projects", name)
		if boundErr := pathutil.ValidateBoundary(campaignRoot, modulesPath); boundErr == nil {
			if removeErr := os.RemoveAll(modulesPath); removeErr == nil {
				addStep(result, fmt.Sprintf("removed .git/modules/projects/%s", name))
			}
		}
	}

	if opts.Delete {
		var errs []error

		if err := os.RemoveAll(projectPath); err != nil {
			errs = append(errs, camperrors.Wrapf(err, "delete project files %q", projectPath))
		} else {
			addStep(result, fmt.Sprintf("deleted project directory %s", projectPath))
			result.FilesDeleted = true
		}

		worktreePath := campaignProjectWorktreePath(ctx, campaignRoot, name)
		if boundErr := pathutil.ValidateBoundary(campaignRoot, worktreePath); boundErr != nil {
			errs = append(errs, camperrors.Wrap(boundErr, "worktree path boundary violation"))
		} else if _, statErr := os.Stat(worktreePath); statErr == nil {
			if removeErr := os.RemoveAll(worktreePath); removeErr != nil {
				errs = append(errs, camperrors.Wrapf(removeErr, "delete worktree %q", worktreePath))
			} else {
				addStep(result, fmt.Sprintf("deleted worktree directory %s", worktreePath))
				result.WorktreeDeleted = true
			}
		}

		if len(errs) > 0 {
			result.RecoveryInstructions = buildRecoveryInstructions(result, campaignRoot, name, opts)
			return result, errors.Join(errs...)
		}
	}

	return result, nil
}

// buildRecoveryInstructions generates manual recovery commands based on which
// steps have already been completed and which remain.
func buildRecoveryInstructions(result *RemoveResult, campaignRoot, name string, opts RemoveOptions) []string {
	var instructions []string
	submodulePath := filepath.Join("projects", name)
	projectPath := filepath.Join(campaignRoot, "projects", name)
	modulesPath := filepath.Join(campaignRoot, ".git", "modules", "projects", name)

	if !result.SubmoduleRemoved {
		instructions = append(instructions,
			"# Deinit the submodule:",
			fmt.Sprintf("  git -C %s submodule deinit -f %s", campaignRoot, submodulePath),
			fmt.Sprintf("  git -C %s rm -f %s", campaignRoot, submodulePath),
			fmt.Sprintf("  rm -rf %s", modulesPath),
		)
	} else if _, err := os.Stat(modulesPath); err == nil {
		instructions = append(instructions,
			"# Clean up stale module metadata:",
			fmt.Sprintf("  rm -rf %s", modulesPath),
		)
	}

	if opts.Delete && !result.FilesDeleted {
		instructions = append(instructions,
			"# Delete project files:",
			fmt.Sprintf("  rm -rf %s", projectPath),
		)
	}

	if opts.Delete && !result.WorktreeDeleted {
		worktreePath := filepath.Join(campaignRoot, "projects", "worktrees", name)
		if _, err := os.Stat(worktreePath); err == nil {
			instructions = append(instructions,
				"# Delete worktree:",
				fmt.Sprintf("  rm -rf %s", worktreePath),
			)
		}
	}

	return instructions
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

// isDirtySubmodule reports whether the submodule at path has uncommitted changes.
// Returns (false, nil, nil) if the directory has no git repo or is clean.
func isDirtySubmodule(ctx context.Context, path string) (bool, []string, error) {
	if _, err := os.Stat(filepath.Join(path, ".git")); os.IsNotExist(err) {
		return false, nil, nil
	}

	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	cmd.Dir = path
	output, err := cmd.Output()
	if err != nil {
		return false, nil, camperrors.Wrap(err, "git status failed in submodule")
	}

	var changed []string
	for _, line := range strings.Split(strings.TrimRight(string(output), "\n"), "\n") {
		if line != "" {
			changed = append(changed, strings.TrimSpace(line))
		}
	}
	return len(changed) > 0, changed, nil
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
			return camperrors.Wrap(err, "submodule deinit failed")
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
			return camperrors.Wrap(err, "git rm failed")
		}
		return nil
	})
}
