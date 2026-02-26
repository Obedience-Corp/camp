package project

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/git"
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
	cfg := git.SubmoduleRetryConfig()
	cfg.OperationName = "submodule add"

	err := git.WithLockRetry(ctx, campaignRoot, cfg, func() error {
		return executeRemoteSubmoduleAdd(ctx, campaignRoot, url, path)
	})
	if err != nil {
		return err
	}

	return initializeSubmodule(ctx, campaignRoot, path)
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

	cfg := git.SubmoduleRetryConfig()
	cfg.OperationName = "local submodule add"

	return git.WithLockRetry(ctx, campaignRoot, cfg, func() error {
		return executeLocalSubmoduleAdd(ctx, campaignRoot, absLocal, destPath)
	})
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
