package project

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// languageMarkers are files that indicate an independently buildable subproject.
var languageMarkers = []string{
	"go.mod",
	"Cargo.toml",
	"package.json",
	"pyproject.toml",
	"setup.py",
	"pom.xml",
	"build.gradle",
	"mix.exs",
}

// excludedSubdirs are directories that should never be treated as subprojects.
var excludedSubdirs = map[string]bool{
	"vendor":       true,
	"node_modules": true,
	".git":         true,
	"testdata":     true,
	"test":         true,
	"tests":        true,
}

// List returns all projects in the campaign's projects directory.
// It identifies git repositories, detects their project type, and expands
// monorepos into individual subproject entries.
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

		projects = append(projects, resolveProject(ctx, entry.Name(), projectPath)...)
	}

	return projects, nil
}

// resolveProject returns one or more Project entries for a discovered git repo.
// Monorepos with 2+ language-marker subdirectories are expanded into individual
// subproject entries; standalone projects return a single entry.
func resolveProject(ctx context.Context, name, projectPath string) []Project {
	url := getGitRemoteURL(ctx, projectPath)
	relPath := filepath.Join("projects", name)

	subprojects := detectMonorepoSubprojects(projectPath)
	if len(subprojects) >= 2 {
		expanded := make([]Project, 0, len(subprojects))
		for _, sub := range subprojects {
			expanded = append(expanded, Project{
				Name:         name + "/" + sub.name,
				Path:         filepath.Join(relPath, sub.name),
				Type:         sub.projectType,
				URL:          url,
				MonorepoRoot: relPath,
			})
		}
		return expanded
	}

	return []Project{{
		Name: name,
		Path: relPath,
		Type: detectProjectType(projectPath),
		URL:  url,
	}}
}

type subproject struct {
	name        string
	projectType string
}

// detectMonorepoSubprojects scans immediate subdirectories for language markers.
// Returns the list of subdirectories that have markers. If fewer than 2 are
// found, the caller should treat the project as standalone.
func detectMonorepoSubprojects(projectPath string) []subproject {
	entries, err := os.ReadDir(projectPath)
	if err != nil {
		return nil
	}

	var subs []subproject
	for _, entry := range entries {
		name := entry.Name()

		// Skip non-directories
		if !entry.IsDir() {
			continue
		}

		// Skip excluded directories
		if excludedSubdirs[name] {
			continue
		}

		// Skip hidden and underscore-prefixed directories
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}

		// Skip symlinks (avoid double-counting repos tracked independently)
		info, err := os.Lstat(filepath.Join(projectPath, name))
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}

		subPath := filepath.Join(projectPath, name)
		if marker := findLanguageMarker(subPath); marker != "" {
			subs = append(subs, subproject{
				name:        name,
				projectType: detectProjectType(subPath),
			})
		}
	}

	return subs
}

// findLanguageMarker checks if a directory contains any language project marker.
// Returns the marker filename if found, empty string otherwise.
func findLanguageMarker(dir string) string {
	for _, marker := range languageMarkers {
		if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
			return marker
		}
	}
	return ""
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
