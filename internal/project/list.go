package project

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// List returns all projects in the campaign's projects directory.
// It identifies git repositories and detects their project type.
func List(ctx context.Context, campaignRoot string) ([]Project, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	projectsDir := filepath.Join(campaignRoot, "projects")

	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var projects []Project
	for _, entry := range entries {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		if !entry.IsDir() {
			continue
		}

		projectPath := filepath.Join(projectsDir, entry.Name())

		// Check if it's a git repo (has .git file or directory)
		gitPath := filepath.Join(projectPath, ".git")
		if _, err := os.Stat(gitPath); err != nil {
			continue
		}

		project := Project{
			Name: entry.Name(),
			Path: filepath.Join("projects", entry.Name()),
			Type: detectProjectType(projectPath),
			URL:  getGitRemoteURL(ctx, projectPath),
		}

		projects = append(projects, project)
	}

	return projects, nil
}

// detectProjectType attempts to determine the project type based on marker files.
func detectProjectType(path string) string {
	// Check for Go
	if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
		return TypeGo
	}

	// Check for Rust
	if _, err := os.Stat(filepath.Join(path, "Cargo.toml")); err == nil {
		return TypeRust
	}

	// Check for TypeScript/JavaScript (package.json)
	if _, err := os.Stat(filepath.Join(path, "package.json")); err == nil {
		return TypeTypeScript
	}

	// Check for Python
	if _, err := os.Stat(filepath.Join(path, "pyproject.toml")); err == nil {
		return TypePython
	}
	if _, err := os.Stat(filepath.Join(path, "setup.py")); err == nil {
		return TypePython
	}
	if _, err := os.Stat(filepath.Join(path, "requirements.txt")); err == nil {
		return TypePython
	}

	return TypeUnknown
}

// getGitRemoteURL retrieves the origin remote URL for a git repository.
func getGitRemoteURL(ctx context.Context, path string) string {
	cmd := exec.CommandContext(ctx, "git", "-C", path, "remote", "get-url", "origin")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}
