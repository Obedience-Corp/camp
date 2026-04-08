package project

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
)

// ResolveResult holds the resolved project name and absolute path.
type ResolveResult struct {
	Name string
	Path string
}

// Resolve determines the project from a flag value or the current working directory.
// If flagProject is non-empty, it resolves by name via project.List().
// Otherwise, it auto-detects the project from cwd.
func Resolve(ctx context.Context, campRoot, flagProject string) (*ResolveResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if flagProject != "" {
		absPath, err := ResolveByName(ctx, campRoot, flagProject)
		if err != nil {
			return nil, err
		}
		return &ResolveResult{Name: flagProject, Path: absPath}, nil
	}

	return ResolveFromCwd(ctx, campRoot)
}

// ResolveByName looks up a project by name using project.List().
// Returns the absolute path to the project directory.
func ResolveByName(ctx context.Context, campRoot, name string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	projects, err := List(ctx, campRoot)
	if err != nil {
		return "", camperrors.Wrap(err, "failed to list projects")
	}

	for _, proj := range projects {
		if proj.Name == name {
			return filepath.Join(campRoot, proj.Path), nil
		}
	}

	return "", &ProjectNotFoundError{
		Name:     name,
		CampRoot: campRoot,
		Projects: projects,
	}
}

// ResolveFromCwd detects the project from the current working directory
// and validates it against project.List().
func ResolveFromCwd(ctx context.Context, campRoot string) (*ResolveResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil, camperrors.Wrap(err, "failed to get working directory")
	}

	// Resolve symlinks in cwd so we can match against both symlink and target paths
	resolvedCwd, _ := filepath.EvalSymlinks(cwd)
	resolvedCamp, _ := filepath.EvalSymlinks(campRoot)

	// Look up the project in the dynamic list first — this covers linked projects
	// (symlinks) which may not have a .git directory at all.
	projects, listErr := List(ctx, campRoot)
	if listErr != nil {
		return nil, camperrors.Wrap(listErr, "failed to list projects")
	}

	for _, proj := range projects {
		projPath := filepath.Join(campRoot, proj.Path)
		resolvedProj, _ := filepath.EvalSymlinks(projPath)

		// Check if cwd is the project root or inside it
		if isPathWithin(resolvedCwd, resolvedProj) || isPathWithin(cwd, projPath) {
			return &ResolveResult{Name: proj.Name, Path: resolvedProj}, nil
		}

		// For linked projects, also check against the linked target path
		if proj.LinkedPath != "" {
			if isPathWithin(resolvedCwd, proj.LinkedPath) {
				return &ResolveResult{Name: proj.Name, Path: proj.LinkedPath}, nil
			}
		}
	}

	// Fall back to git-based detection for submodules
	projectRoot, isSubmodule, err := git.FindProjectRootWithType(cwd)
	if err != nil {
		return nil, camperrors.Wrap(err, "not inside a project directory")
	}

	resolvedRoot, _ := filepath.EvalSymlinks(projectRoot)
	if resolvedRoot == resolvedCamp || projectRoot == campRoot {
		return nil, errors.New("you're in the campaign root, not a project\nUse 'camp commit' for campaign-level commits")
	}

	// Check again against the list with the git-detected root
	for _, proj := range projects {
		projPath := filepath.Join(campRoot, proj.Path)
		resolvedProj, _ := filepath.EvalSymlinks(projPath)
		if resolvedProj == resolvedRoot || projPath == projectRoot {
			return &ResolveResult{Name: proj.Name, Path: projectRoot}, nil
		}
	}

	// If it's a submodule but not in our list, still accept it
	if isSubmodule {
		name := nameFromPath(campRoot, projectRoot)
		return &ResolveResult{Name: name, Path: projectRoot}, nil
	}

	return nil, &ProjectNotFoundError{
		Name:     projectRoot,
		CampRoot: campRoot,
		Projects: projects,
	}
}

// isPathWithin returns true if child is equal to or a subdirectory of parent.
func isPathWithin(child, parent string) bool {
	if child == parent {
		return true
	}
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".."
}

// nameFromPath extracts a project name from its absolute path relative to
// the campaign's projects directory.
func nameFromPath(campRoot, absPath string) string {
	projectsDir := filepath.Join(campRoot, "projects")
	if rel, err := filepath.Rel(projectsDir, absPath); err == nil {
		parts := strings.SplitN(rel, string(filepath.Separator), 2)
		if len(parts) > 0 && parts[0] != ".." && parts[0] != "." {
			return parts[0]
		}
	}
	return filepath.Base(absPath)
}

// ProjectNotFoundError is returned when a project name cannot be resolved.
type ProjectNotFoundError struct {
	Name     string
	CampRoot string
	Projects []Project
}

func (e *ProjectNotFoundError) Error() string {
	return fmt.Sprintf("project %q not found in campaign", e.Name)
}

// Unwrap returns ErrNotFound so errors.Is(err, camperrors.ErrNotFound) works.
func (e *ProjectNotFoundError) Unwrap() error {
	return camperrors.ErrNotFound
}

// AvailableProjects returns the list of projects for display in error messages.
func (e *ProjectNotFoundError) AvailableProjects() []Project {
	return e.Projects
}

// FormatProjectList returns a formatted string listing available projects.
func FormatProjectList(projects []Project) string {
	if len(projects) == 0 {
		return "No projects found in this campaign."
	}

	var b strings.Builder
	b.WriteString("Available projects:\n")
	for _, proj := range projects {
		b.WriteString(fmt.Sprintf("  - %s (%s)\n", proj.Name, proj.Path))
	}
	b.WriteString("\nUse --project to specify a project, or navigate into one first.")
	return b.String()
}
