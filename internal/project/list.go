package project

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/git"
)

// List returns all projects in the campaign's projects directory.
// It identifies git repositories, detects their project type, and expands
// repos with .gitmodules into root + submodule entries. Symlinked project
// entries are treated as linked projects.
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

		name := entry.Name()
		projectPath := filepath.Join(projectsDir, name)

		if entry.Type()&os.ModeSymlink != 0 {
			resolvedPath, err := filepath.EvalSymlinks(projectPath)
			if err != nil {
				continue
			}
			info, err := os.Stat(resolvedPath)
			if err != nil || !info.IsDir() {
				continue
			}
			projects = append(projects, resolveLinkedProject(ctx, name, projectPath, resolvedPath)...)
			continue
		}

		if !entry.IsDir() {
			continue
		}

		// Check if it's a git repo (has .git file or directory)
		gitPath := filepath.Join(projectPath, ".git")
		if _, err := os.Stat(gitPath); err != nil {
			continue
		}

		resolved := resolveProject(ctx, name, projectPath)
		for i := range resolved {
			resolved[i].Source = SourceSubmodule
		}
		projects = append(projects, resolved...)
	}

	projects = deduplicateByRemoteURL(ctx, campaignRoot, projects)

	return projects, nil
}

func resolveLinkedProject(ctx context.Context, name, logicalPath, resolvedPath string) []Project {
	source := SourceLinked
	if !isGitRepo(resolvedPath) {
		source = SourceLinkedNonGit
	}

	url := ""
	if source == SourceLinked {
		url = getGitRemoteURL(ctx, resolvedPath)
	}

	relPath := filepath.Join("projects", name)

	submodulePaths, _ := git.ListSubmodulePaths(ctx, resolvedPath)
	if source == SourceLinked && len(submodulePaths) > 0 {
		expanded := make([]Project, 0, len(submodulePaths)+1)
		expanded = append(expanded, Project{
			Name:        name,
			Path:        relPath,
			Type:        detectProjectType(resolvedPath),
			URL:         url,
			Source:      source,
			LinkedPath:  resolvedPath,
			ExcludeDirs: submodulePaths,
		})
		for _, subPath := range submodulePaths {
			subFullPath := filepath.Join(resolvedPath, subPath)
			// Each submodule has its own remote. Recording the
			// submodule's own URL (rather than the parent's) lets
			// downstream consumers like leverage scoring dedup a
			// submodule entry against a standalone clone of the
			// same repo. Empty URL is fine — uninitialised submodules
			// are kept as-is.
			expanded = append(expanded, Project{
				Name:         name + "@" + subPath,
				Path:         filepath.Join(relPath, subPath),
				Type:         detectProjectType(subFullPath),
				URL:          getGitRemoteURL(ctx, subFullPath),
				Source:       source,
				LinkedPath:   resolvedPath,
				MonorepoRoot: relPath,
			})
		}
		return expanded
	}

	return []Project{{
		Name:       name,
		Path:       relPath,
		Type:       detectProjectType(resolvedPath),
		URL:        url,
		Source:     source,
		LinkedPath: resolvedPath,
	}}
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
		// Use the submodule's own remote URL rather than the
		// parent monorepo's URL. This makes the URL field honest
		// (each entry's URL identifies its own repo) and lets
		// leverage-level dedup recognise when a submodule and a
		// standalone clone refer to the same repo. Empty URL is
		// fine — uninitialised submodules are kept as-is.
		expanded = append(expanded, Project{
			Name:         name + "@" + subPath,
			Path:         filepath.Join(relPath, subPath),
			Type:         detectProjectType(subFullPath),
			URL:          getGitRemoteURL(ctx, subFullPath),
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

func isGitRepo(path string) bool {
	_, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil
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

// deduplicateByRemoteURL groups standalone projects by git remote URL and keeps
// only the copy with the most recent commit. Projects with empty URL are always
// kept. Monorepo subprojects are always kept — they are separate checkouts that
// can sit on different branches/commits than any standalone project with the
// same base name, and callers (e.g. `camp fresh`) need to target them directly.
func deduplicateByRemoteURL(ctx context.Context, campaignRoot string, projects []Project) []Project {
	if ctx.Err() != nil {
		return projects
	}

	// Index of the best (most recent) project per URL.
	bestIdx := make(map[string]int)
	bestDate := make(map[string]string)

	for i, p := range projects {
		// Monorepo subprojects share the parent's URL. They are not subject to
		// URL-based dedup — each nested submodule is an independent checkout.
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
			result = append(result, p)
		case keep[i]:
			result = append(result, p)
		}
	}
	return result
}
