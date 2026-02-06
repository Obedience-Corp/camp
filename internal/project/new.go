package project

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
)

// validProjectName matches alphanumeric, hyphens, and underscores.
var validProjectName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

// NewOptions configures new project creation.
type NewOptions struct {
	// Path overrides the destination path (defaults to projects/<name>).
	Path string
}

// New creates a new local project and adds it as a submodule to the campaign.
// The project is initialized as a git repo with a README and initial commit.
func New(ctx context.Context, campaignRoot, name string, opts NewOptions) (*AddResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if err := checkGitInstalled(ctx); err != nil {
		return nil, err
	}

	if err := checkIsGitRepo(ctx, campaignRoot); err != nil {
		return nil, err
	}

	if err := validateProjectName(name); err != nil {
		return nil, err
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

	// Create directory
	if err := os.MkdirAll(fullPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create project directory: %w", err)
	}

	// Initialize git repo
	if err := initProjectRepo(ctx, fullPath, name); err != nil {
		// Clean up on failure
		os.RemoveAll(fullPath)
		return nil, fmt.Errorf("failed to initialize project: %w", err)
	}

	// Add as local submodule
	if err := addLocalAsSubmodule(ctx, campaignRoot, fullPath, destPath, name); err != nil {
		os.RemoveAll(fullPath)
		return nil, err
	}

	// Create worktree directory
	worktreePath := filepath.Join(campaignRoot, "worktrees", name)
	if mkErr := os.MkdirAll(worktreePath, 0755); mkErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not create worktree directory: %v\n", mkErr)
	}

	return &AddResult{
		Name:   name,
		Path:   destPath,
		Source: "local (new)",
		Type:   detectProjectType(fullPath),
	}, nil
}

// validateProjectName checks that the name is valid for use as a project directory.
func validateProjectName(name string) error {
	if name == "" {
		return fmt.Errorf("project name is required")
	}

	if !validProjectName.MatchString(name) {
		return fmt.Errorf("invalid project name %q: must start with alphanumeric and contain only alphanumeric, hyphens, or underscores", name)
	}

	return nil
}

// initProjectRepo initializes a git repo with a README and initial commit.
func initProjectRepo(ctx context.Context, path, name string) error {
	// git init
	cmd := exec.CommandContext(ctx, "git", "init", path)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git init failed: %w (output: %s)", err, string(output))
	}

	// Write README
	readme := fmt.Sprintf("# %s\n", name)
	if err := os.WriteFile(filepath.Join(path, "README.md"), []byte(readme), 0644); err != nil {
		return fmt.Errorf("failed to write README: %w", err)
	}

	// git add + commit
	cmd = exec.CommandContext(ctx, "git", "-C", path, "add", ".")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git add failed: %w (output: %s)", err, string(output))
	}

	cmd = exec.CommandContext(ctx, "git", "-C", path, "commit", "-m", "Initial commit")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git commit failed: %w (output: %s)", err, string(output))
	}

	return nil
}
