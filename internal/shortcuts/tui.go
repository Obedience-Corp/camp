// Package shortcuts provides TUI components for adding shortcuts interactively.
package shortcuts

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/ktr0731/go-fuzzyfinder"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
)

// ErrAborted is returned when the user cancels selection.
var ErrAborted = errors.New("selection aborted")

// AddSubShortcutResult contains the result of the add sub-shortcut TUI.
type AddSubShortcutResult struct {
	ProjectName  string
	ShortcutName string
	ShortcutPath string
}

// AddJumpResult contains the result of the add-jump TUI.
type AddJumpResult struct {
	Name        string
	Path        string
	Description string
	Concept     string
}

// RunAddSubShortcutTUI launches an interactive TUI for adding a project sub-shortcut.
func RunAddSubShortcutTUI(ctx context.Context, root string) (*AddSubShortcutResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Discover projects from filesystem
	projects, err := project.List(ctx, root)
	if err != nil {
		return nil, camperrors.Wrap(err, "failed to list projects")
	}

	if len(projects) == 0 {
		return nil, errors.New("no projects found in campaign")
	}

	// Step 1: Pick project using fuzzy finder
	projectIdx, err := pickProject(projects)
	if err != nil {
		if errors.Is(err, fuzzyfinder.ErrAbort) {
			return nil, ErrAborted
		}
		return nil, camperrors.Wrap(err, "failed to pick project")
	}

	proj := projects[projectIdx]
	projectPath := filepath.Join(root, proj.Path)

	// Step 2: Get directory suggestions within project
	dirs := listDirsUnder(projectPath, 3)

	// Step 3: Form for name and path
	var name, path string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Shortcut Name").
				Description("Use 'default' for the default jump location").
				Placeholder("cli").
				Value(&name).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return errors.New("shortcut name is required")
					}
					return nil
				}),
			huh.NewInput().
				Title("Path").
				Description(fmt.Sprintf("Relative path within %s/", proj.Name)).
				Placeholder("cmd/app/").
				Suggestions(dirs).
				Value(&path).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return errors.New("path is required")
					}
					// Validate path exists
					fullPath := filepath.Join(projectPath, s)
					if stat, err := os.Stat(fullPath); err != nil || !stat.IsDir() {
						return fmt.Errorf("path does not exist: %s", s)
					}
					return nil
				}),
		),
	)

	if err := theme.RunForm(ctx, form); err != nil {
		if theme.IsCancelled(err) {
			return nil, ErrAborted
		}
		return nil, err
	}

	return &AddSubShortcutResult{
		ProjectName:  proj.Name,
		ShortcutName: strings.TrimSpace(name),
		ShortcutPath: strings.TrimSpace(path),
	}, nil
}

// RunAddJumpTUI launches an interactive TUI for adding a campaign-level shortcut.
func RunAddJumpTUI(ctx context.Context, root string) (*AddJumpResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Get directory suggestions from campaign root
	dirs := listDirsUnder(root, 3)

	var name, path, description, concept string

	// Step 1: Required fields - name and path
	form1 := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Shortcut Name").
				Description("Short key for navigation (e.g., 'api')").
				Placeholder("api").
				Value(&name).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return errors.New("shortcut name is required")
					}
					return nil
				}),
			huh.NewInput().
				Title("Path").
				Description("Relative path from campaign root (leave empty for concept-only)").
				Placeholder("projects/api-service/").
				Suggestions(dirs).
				Value(&path).
				Validate(func(s string) error {
					// Path can be empty for concept-only shortcuts
					if strings.TrimSpace(s) == "" {
						return nil
					}
					// Validate path exists if provided
					fullPath := filepath.Join(root, s)
					if stat, err := os.Stat(fullPath); err != nil || !stat.IsDir() {
						return fmt.Errorf("path does not exist: %s", s)
					}
					return nil
				}),
		),
	)

	if err := theme.RunForm(ctx, form1); err != nil {
		if theme.IsCancelled(err) {
			return nil, ErrAborted
		}
		return nil, err
	}

	// Step 2: Optional fields - description and concept
	form2 := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Description").
				Description("Help text shown in shortcuts list (optional)").
				Placeholder("Jump to API service").
				Value(&description),
			huh.NewInput().
				Title("Concept").
				Description("Command group for expansion, e.g., 'project' (optional)").
				Placeholder("project").
				Value(&concept),
		),
	)

	if err := theme.RunForm(ctx, form2); err != nil {
		if theme.IsCancelled(err) {
			return nil, ErrAborted
		}
		return nil, err
	}

	// Validate: must have path or concept
	path = strings.TrimSpace(path)
	concept = strings.TrimSpace(concept)
	if path == "" && concept == "" {
		return nil, errors.New("shortcut must have a path or concept")
	}

	return &AddJumpResult{
		Name:        strings.TrimSpace(name),
		Path:        path,
		Description: strings.TrimSpace(description),
		Concept:     concept,
	}, nil
}

// pickProject shows a fuzzy picker for selecting a project.
func pickProject(projects []project.Project) (int, error) {
	idx, err := fuzzyfinder.Find(
		projects,
		func(i int) string {
			return projects[i].Name
		},
		fuzzyfinder.WithPromptString("Select project: "),
		fuzzyfinder.WithPreviewWindow(func(i, w, h int) string {
			if i < 0 || i >= len(projects) {
				return ""
			}
			p := projects[i]
			preview := fmt.Sprintf("Name: %s\nPath: %s\nType: %s", p.Name, p.Path, p.Type)
			if p.URL != "" {
				preview += fmt.Sprintf("\nURL: %s", p.URL)
			}
			return preview
		}),
	)
	return idx, err
}

// listDirsUnder lists directories under the given path up to maxDepth levels.
// Returns relative paths suitable for suggestions.
func listDirsUnder(basePath string, maxDepth int) []string {
	var dirs []string

	walkFunc := func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		// Skip the root itself
		if path == basePath {
			return nil
		}

		// Calculate depth
		rel, err := filepath.Rel(basePath, path)
		if err != nil {
			return nil
		}

		// Skip hidden directories
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Only include directories
		if !d.IsDir() {
			return nil
		}

		// Check depth
		depth := strings.Count(rel, string(os.PathSeparator)) + 1
		if depth > maxDepth {
			return filepath.SkipDir
		}

		// Add trailing slash to indicate directory
		dirs = append(dirs, rel+"/")

		return nil
	}

	_ = filepath.WalkDir(basePath, walkFunc)

	// Sort for consistent ordering
	sort.Strings(dirs)

	return dirs
}
