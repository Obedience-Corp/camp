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

	projectRoot, isSubmodule, err := git.FindProjectRootWithType(cwd)
	if err != nil {
		return nil, camperrors.Wrap(err, "not inside a project directory")
	}

	// Resolve symlinks for reliable comparison (e.g., macOS /var → /private/var)
	resolvedRoot, _ := filepath.EvalSymlinks(projectRoot)
	resolvedCamp, _ := filepath.EvalSymlinks(campRoot)
	if resolvedRoot == resolvedCamp || projectRoot == campRoot {
		return nil, errors.New("you're in the campaign root, not a project\nUse 'camp commit' for campaign-level commits")
	}

	// Look up the project in the dynamic list
	projects, listErr := List(ctx, campRoot)
	if listErr != nil {
		return nil, camperrors.Wrap(listErr, "failed to list projects")
	}

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
