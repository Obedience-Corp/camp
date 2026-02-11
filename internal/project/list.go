package project

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/obediencecorp/camp/internal/git"
)

// List returns all projects in the campaign's projects directory.
// It identifies git repositories, detects their project type, and expands
// repos with .gitmodules into root + submodule entries.
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

	projects = deduplicateByRemoteURL(ctx, campaignRoot, projects)

	return projects, nil
}

// resolveProject returns one or more Project entries for a discovered git repo.
// Repos with .gitmodules are expanded into a root entry plus one entry per
// submodule. Repos without .gitmodules are treated as standalone.
func resolveProject(ctx context.Context, name, projectPath string) []Project {
	url := getGitRemoteURL(ctx, projectPath)
	relPath := filepath.Join("projects", name)

	submodulePaths, _ := git.ListSubmodulePaths(ctx, projectPath)
	if len(submodulePaths) == 0 {
		// Standalone repo — single entry, scc scans entire directory.
		return []Project{{
			Name: name,
			Path: relPath,
			Type: detectProjectType(projectPath),
			URL:  url,
		}}
	}

	// Submodule-based repo — root entry + one entry per submodule.
	expanded := make([]Project, 0, len(submodulePaths)+1)

	// Root entry captures root-level code (configs, docs, shared tooling).
	expanded = append(expanded, Project{
		Name:        name,
		Path:        relPath,
		Type:        detectProjectType(projectPath),
		URL:         url,
		ExcludeDirs: submodulePaths,
	})

	for _, subPath := range submodulePaths {
		subFullPath := filepath.Join(projectPath, subPath)
		expanded = append(expanded, Project{
			Name:         name + "@" + subPath,
			Path:         filepath.Join(relPath, subPath),
			Type:         detectProjectType(subFullPath),
			URL:          url,
			MonorepoRoot: relPath,
		})
	}
	return expanded
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

// latestCommitDate returns the ISO 8601 date of the most recent commit in the
// given project directory. Returns empty string on error.
func latestCommitDate(ctx context.Context, absPath string) string {
	cmd := exec.CommandContext(ctx, "git", "-C", absPath, "log", "-1", "--format=%cI")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// deduplicateByRemoteURL groups projects by git remote URL and keeps only the
// copy with the most recent commit. Projects with empty URL are always kept.
// Monorepo subprojects skip URL-based dedup (they share the parent's URL) but
// are dropped if a standalone project with the same base name exists.
func deduplicateByRemoteURL(ctx context.Context, campaignRoot string, projects []Project) []Project {
	if ctx.Err() != nil {
		return projects
	}

	// Index of the best (most recent) project per URL.
	bestIdx := make(map[string]int)
	bestDate := make(map[string]string)

	// Collect standalone project names for monorepo dedup.
	standaloneNames := make(map[string]bool)
	for _, p := range projects {
		if p.MonorepoRoot == "" {
			standaloneNames[p.Name] = true
		}
	}

	for i, p := range projects {
		// Skip monorepo subprojects — they share the parent's URL and are
		// deduped by name against standalone repos instead.
		if p.URL == "" || p.MonorepoRoot != "" {
			continue
		}

		_, seen := bestIdx[p.URL]
		if !seen {
			bestIdx[p.URL] = i
			bestDate[p.URL] = latestCommitDate(ctx, filepath.Join(campaignRoot, p.Path))
			continue
		}

		// Compare commit dates to decide which copy to keep.
		date := latestCommitDate(ctx, filepath.Join(campaignRoot, p.Path))
		if date > bestDate[p.URL] {
			bestIdx[p.URL] = i
			bestDate[p.URL] = date
		}
	}

	// Build set of indices to keep.
	keep := make(map[int]bool, len(projects))
	for _, idx := range bestIdx {
		keep[idx] = true
	}

	result := make([]Project, 0, len(projects))
	for i, p := range projects {
		switch {
		case p.URL == "":
			result = append(result, p)
		case p.MonorepoRoot != "":
			// Keep monorepo subproject only if no standalone repo has the same base name.
			baseName := filepath.Base(p.Path)
			if !standaloneNames[baseName] {
				result = append(result, p)
			}
		case keep[i]:
			result = append(result, p)
		}
	}
	return result
}
