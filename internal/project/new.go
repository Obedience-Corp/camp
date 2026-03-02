package project

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

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

	if err := ValidateProjectName(name); err != nil {
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
		return nil, camperrors.Wrap(err, "failed to create project directory")
	}

	// Initialize git repo
	if err := initProjectRepo(ctx, fullPath, name); err != nil {
		// Clean up on failure
		os.RemoveAll(fullPath)
		return nil, camperrors.Wrap(err, "failed to initialize project")
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
		return camperrors.Wrap(err, "failed to write README")
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
