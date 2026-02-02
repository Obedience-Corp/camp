package project

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/obediencecorp/camp/internal/git"
)

const (
	maxRetryAttempts   = 3
	initialBackoff     = 200 * time.Millisecond
	maxBackoff         = 2 * time.Second
	activeLockWaitTime = 5 * time.Second
)

// AddOptions configures the project add behavior.
type AddOptions struct {
	// Name overrides the project name (defaults to repo name).
	Name string
	// Path overrides the destination path (defaults to projects/<name>).
	Path string
	// Local is the path to an existing local repo to add.
	Local string
}

// AddResult contains information about the added project.
type AddResult struct {
	// Name is the project name.
	Name string
	// Path is the relative path from campaign root.
	Path string
	// Source is the original URL or path.
	Source string
	// Type is the detected project type.
	Type string
}

// ErrProjectExists is returned when a project already exists.
type ErrProjectExists struct {
	Name string
	Path string
}

func (e *ErrProjectExists) Error() string {
	return fmt.Sprintf("project already exists: %s at %s", e.Name, e.Path)
}

// Add adds a git repository as a submodule to the campaign.
func Add(ctx context.Context, campaignRoot, source string, opts AddOptions) (*AddResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Pre-flight check: is git installed?
	if err := checkGitInstalled(ctx); err != nil {
		return nil, err
	}

	// Pre-flight check: are we in a git repo?
	if err := checkIsGitRepo(ctx, campaignRoot); err != nil {
		return nil, err
	}

	// Validate source
	source = strings.TrimSpace(source)
	if source == "" && opts.Local == "" {
		return nil, fmt.Errorf("source URL is required\n" +
			"Hint: Provide a git URL like 'git@github.com:org/repo.git' or use --local for existing repos")
	}

	// Validate and parse the URL (unless it's a local path)
	if opts.Local == "" {
		if _, err := ParseGitURL(source); err != nil {
			return nil, err
		}
	}

	// Determine project name
	name := opts.Name
	if name == "" {
		if opts.Local != "" {
			name = filepath.Base(opts.Local)
		} else {
			name = extractRepoName(source)
		}
	}

	// Determine destination path
	destPath := opts.Path
	if destPath == "" {
		destPath = filepath.Join("projects", name)
	}

	fullPath := filepath.Join(campaignRoot, destPath)

	// Check if already exists
	if _, err := os.Stat(fullPath); err == nil {
		return nil, &ErrProjectExists{Name: name, Path: destPath}
	}

	// Add as submodule
	var err error
	if opts.Local != "" {
		err = addLocalAsSubmodule(ctx, campaignRoot, opts.Local, destPath, name)
	} else {
		err = addRemoteAsSubmodule(ctx, campaignRoot, source, destPath)
	}

	if err != nil {
		return nil, err
	}

	// Create worktree directory
	worktreePath := filepath.Join(campaignRoot, "worktrees", name)
	if mkErr := os.MkdirAll(worktreePath, 0755); mkErr != nil {
		// Log warning but don't fail
		fmt.Fprintf(os.Stderr, "Warning: could not create worktree directory: %v\n", mkErr)
	}

	// Detect project type
	projectType := detectProjectType(fullPath)

	result := &AddResult{
		Name:   name,
		Path:   destPath,
		Source: source,
		Type:   projectType,
	}
	if opts.Local != "" {
		result.Source = opts.Local
	}

	return result, nil
}

// extractRepoName extracts the repository name from a git URL.
// Supports both SSH (git@github.com:org/repo.git) and HTTPS (https://github.com/org/repo.git) URLs.
func extractRepoName(url string) string {
	// Handle SSH URLs: git@github.com:org/repo.git
	if strings.Contains(url, ":") && strings.HasPrefix(url, "git@") {
		parts := strings.Split(url, ":")
		if len(parts) > 1 {
			path := parts[len(parts)-1]
			return strings.TrimSuffix(filepath.Base(path), ".git")
		}
	}

	// Handle HTTPS URLs and file paths
	base := filepath.Base(url)
	return strings.TrimSuffix(base, ".git")
}

// addRemoteAsSubmodule adds a remote git repository as a submodule.
// It handles stale and active lock files with intelligent retry logic:
// - Stale locks are removed immediately
// - Active locks are waited on (up to 5 seconds) before retrying
// - Exponential backoff between retry attempts
func addRemoteAsSubmodule(ctx context.Context, campaignRoot, url, path string) error {
	var lastErr error
	backoff := initialBackoff

	for attempt := 1; attempt <= maxRetryAttempts; attempt++ {
		err := executeRemoteSubmoduleAdd(ctx, campaignRoot, url, path)
		if err == nil {
			return initializeSubmodule(ctx, campaignRoot, path)
		}

		// Check if it's a lock error
		if !isSubmoduleLockError(err) {
			return err // Non-lock error, don't retry
		}

		lastErr = err

		// Try to clean stale locks
		result, cleanErr := git.CleanStaleLocks(ctx, campaignRoot, nil)
		if cleanErr != nil {
			return fmt.Errorf("failed to clean locks (attempt %d): %w", attempt, cleanErr)
		}

		// If we removed stale locks, retry after brief delay
		if len(result.Removed) > 0 {
			slog.Info("retrying after stale lock cleanup",
				"attempt", attempt,
				"removed", len(result.Removed),
				"path", path)
			time.Sleep(backoff)
			backoff = min(backoff*2, maxBackoff)
			continue
		}

		// If active locks found, wait for them to release
		if len(result.Skipped) > 0 {
			slog.Info("waiting for active lock to release",
				"attempt", attempt,
				"active_locks", len(result.Skipped),
				"path", path)

			// Wait for the first active lock (usually there's only one)
			waitErr := git.WaitForLockRelease(ctx, result.Skipped[0].Path, activeLockWaitTime, slog.Default())
			if waitErr == nil {
				// Lock released! Clean up any remaining stale locks and retry
				git.CleanStaleLocks(ctx, campaignRoot, nil)
				continue
			}

			// Timeout waiting for lock - but still have attempts left
			if attempt < maxRetryAttempts {
				slog.Warn("lock wait timeout, will retry",
					"attempt", attempt,
					"pid", result.Skipped[0].ProcessID,
					"path", result.Skipped[0].Path)
				time.Sleep(backoff)
				backoff = min(backoff*2, maxBackoff)
				continue
			}

			// Final attempt failed
			return fmt.Errorf("cannot add submodule: lock held by active process (PID %d) after waiting: %w",
				result.Skipped[0].ProcessID, lastErr)
		}

		// No locks found but still failed - apply backoff and retry
		slog.Info("retrying submodule add",
			"attempt", attempt,
			"path", path)
		time.Sleep(backoff)
		backoff = min(backoff*2, maxBackoff)
	}

	return fmt.Errorf("submodule add failed after %d attempts: %w", maxRetryAttempts, lastErr)
}

// executeRemoteSubmoduleAdd runs the git submodule add command for a remote URL.
func executeRemoteSubmoduleAdd(ctx context.Context, campaignRoot, url, path string) error {
	cmd := exec.CommandContext(ctx, "git", "submodule", "add", url, path)
	cmd.Dir = campaignRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if it's a lock error first
		errType := git.ClassifyGitError(string(output), cmd.ProcessState.ExitCode())
		if errType == git.GitErrorLock {
			return &git.LockError{
				Path: "index.lock",
				Err:  err,
			}
		}
		// Diagnose the git error and provide helpful guidance
		return DiagnoseGitError(cmd.String(), string(output), cmd.ProcessState.ExitCode())
	}
	return nil
}

// initializeSubmodule runs git submodule update --init for the given path.
func initializeSubmodule(ctx context.Context, campaignRoot, path string) error {
	cmd := exec.CommandContext(ctx, "git", "submodule", "update", "--init", path)
	cmd.Dir = campaignRoot
	if err := cmd.Run(); err != nil {
		// Warning only - submodule was added successfully
		fmt.Fprintf(os.Stderr, "Warning: could not initialize submodule: %v\n", err)
	}
	return nil
}

// addLocalAsSubmodule adds an existing local git repository as a submodule.
// It handles stale and active lock files with intelligent retry logic:
// - Stale locks are removed immediately
// - Active locks are waited on (up to 5 seconds) before retrying
// - Exponential backoff between retry attempts
func addLocalAsSubmodule(ctx context.Context, campaignRoot, localPath, destPath, name string) error {
	// Resolve to absolute path
	absLocal, err := filepath.Abs(localPath)
	if err != nil {
		return fmt.Errorf("failed to resolve local path: %w", err)
	}

	// Verify it's a git repo
	gitPath := filepath.Join(absLocal, ".git")
	if _, err := os.Stat(gitPath); os.IsNotExist(err) {
		return fmt.Errorf("local path is not a git repository: %s\n"+
			"Hint: Run 'git init' in the directory first, or provide a git repository URL instead", localPath)
	}

	var lastErr error
	backoff := initialBackoff

	for attempt := 1; attempt <= maxRetryAttempts; attempt++ {
		err := executeLocalSubmoduleAdd(ctx, campaignRoot, absLocal, destPath)
		if err == nil {
			return nil // Success
		}

		// Check if it's a lock error
		if !isSubmoduleLockError(err) {
			return err // Non-lock error, don't retry
		}

		lastErr = err

		// Try to clean stale locks
		result, cleanErr := git.CleanStaleLocks(ctx, campaignRoot, nil)
		if cleanErr != nil {
			return fmt.Errorf("failed to clean locks (attempt %d): %w", attempt, cleanErr)
		}

		// If we removed stale locks, retry after brief delay
		if len(result.Removed) > 0 {
			slog.Info("retrying after stale lock cleanup",
				"attempt", attempt,
				"removed", len(result.Removed),
				"path", destPath)
			time.Sleep(backoff)
			backoff = min(backoff*2, maxBackoff)
			continue
		}

		// If active locks found, wait for them to release
		if len(result.Skipped) > 0 {
			slog.Info("waiting for active lock to release",
				"attempt", attempt,
				"active_locks", len(result.Skipped),
				"path", destPath)

			// Wait for the first active lock (usually there's only one)
			waitErr := git.WaitForLockRelease(ctx, result.Skipped[0].Path, activeLockWaitTime, slog.Default())
			if waitErr == nil {
				// Lock released! Clean up any remaining stale locks and retry
				git.CleanStaleLocks(ctx, campaignRoot, nil)
				continue
			}

			// Timeout waiting for lock - but still have attempts left
			if attempt < maxRetryAttempts {
				slog.Warn("lock wait timeout, will retry",
					"attempt", attempt,
					"pid", result.Skipped[0].ProcessID,
					"path", result.Skipped[0].Path)
				time.Sleep(backoff)
				backoff = min(backoff*2, maxBackoff)
				continue
			}

			// Final attempt failed
			return fmt.Errorf("cannot add submodule: lock held by active process (PID %d) after waiting: %w",
				result.Skipped[0].ProcessID, lastErr)
		}

		// No locks found but still failed - apply backoff and retry
		slog.Info("retrying local submodule add",
			"attempt", attempt,
			"path", destPath)
		time.Sleep(backoff)
		backoff = min(backoff*2, maxBackoff)
	}

	return fmt.Errorf("local submodule add failed after %d attempts: %w", maxRetryAttempts, lastErr)
}

// executeLocalSubmoduleAdd runs the git submodule add command for a local path.
func executeLocalSubmoduleAdd(ctx context.Context, campaignRoot, absLocal, destPath string) error {
	// Note: -c protocol.file.allow=always is needed for local repos due to CVE-2022-39253 restrictions
	cmd := exec.CommandContext(ctx, "git", "-c", "protocol.file.allow=always", "submodule", "add", absLocal, destPath)
	cmd.Dir = campaignRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if it's a lock error first
		errType := git.ClassifyGitError(string(output), cmd.ProcessState.ExitCode())
		if errType == git.GitErrorLock {
			return &git.LockError{
				Path: "index.lock",
				Err:  err,
			}
		}
		// Diagnose the git error and provide helpful guidance
		return DiagnoseGitError(cmd.String(), string(output), cmd.ProcessState.ExitCode())
	}
	return nil
}

// checkGitInstalled verifies that git is installed and available.
func checkGitInstalled(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "git", "--version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git is not installed or not in PATH\n" +
			"Please install git: https://git-scm.com/downloads")
	}
	return nil
}

// checkIsGitRepo verifies that the campaign root is a git repository.
func checkIsGitRepo(ctx context.Context, campaignRoot string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", campaignRoot, "rev-parse", "--git-dir")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("campaign directory is not a git repository\n" +
			"Hint: Run 'git init' in the campaign root, or use 'camp init' to create a new campaign")
	}
	return nil
}

// isSubmoduleLockError checks if an error is a git lock-related error.
func isSubmoduleLockError(err error) bool {
	var lockErr *git.LockError
	return errors.As(err, &lockErr)
}
