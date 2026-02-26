package worktree

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Obedience-Corp/camp/internal/paths"
)

// Valid worktree name pattern: alphanumeric start, then alphanumeric, hyphens, underscores.
var validNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

// PathManager handles worktree path operations.
type PathManager struct {
	resolver *paths.Resolver
}

// NewPathManager creates a PathManager with the given resolver.
func NewPathManager(resolver *paths.Resolver) *PathManager {
	return &PathManager{resolver: resolver}
}

// WorktreesRoot returns the root directory for all worktrees.
func (pm *PathManager) WorktreesRoot() string {
	return pm.resolver.Worktrees()
}

// ProjectWorktreesDir returns the worktrees directory for a project.
// e.g., projects/worktrees/my-api/
func (pm *PathManager) ProjectWorktreesDir(project string) string {
	return filepath.Join(pm.WorktreesRoot(), project)
}

// WorktreePath returns the full path for a specific worktree.
// e.g., projects/worktrees/my-api/feature-auth/
func (pm *PathManager) WorktreePath(project, name string) string {
	return filepath.Join(pm.ProjectWorktreesDir(project), name)
}

// RelativeWorktreePath returns the path relative to campaign root.
func (pm *PathManager) RelativeWorktreePath(project, name string) string {
	relWorktrees := strings.TrimPrefix(pm.resolver.Worktrees(), pm.resolver.Root()+string(filepath.Separator))
	return filepath.Join(relWorktrees, project, name)
}

// ParseWorktreePath extracts project and worktree name from a path.
// Returns ("", "", error) if path is not a worktree.
func (pm *PathManager) ParseWorktreePath(path string) (project, name string, err error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", "", err
	}

	wtRoot := pm.WorktreesRoot()
	if !strings.HasPrefix(absPath, wtRoot) {
		return "", "", ErrNotInWorktree
	}

	// Get path relative to worktrees root
	rel, err := filepath.Rel(wtRoot, absPath)
	if err != nil {
		return "", "", err
	}

	// Parse: <project>/<worktree>/...
	parts := strings.SplitN(rel, string(filepath.Separator), 3)
	if len(parts) < 2 {
		return "", "", ErrNotInWorktree
	}

	return parts[0], parts[1], nil
}

// ValidateName checks if a worktree name is valid.
func ValidateName(name string) error {
	if name == "" {
		return InvalidWorktreeName(name, "name cannot be empty")
	}

	if len(name) > 64 {
		return InvalidWorktreeName(name, "name too long (max 64 characters)")
	}

	if !validNamePattern.MatchString(name) {
		return InvalidWorktreeName(name,
			"name must start with alphanumeric and contain only alphanumeric, hyphens, underscores")
	}

	// Reserved names
	reserved := []string{".", "..", ".git", ".gitignore"}
	for _, r := range reserved {
		if strings.EqualFold(name, r) {
			return InvalidWorktreeName(name, "reserved name")
		}
	}

	return nil
}

// WorktreeExists checks if a worktree directory exists.
func (pm *PathManager) WorktreeExists(project, name string) bool {
	path := pm.WorktreePath(project, name)
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// EnsureWorktreesDir creates the worktrees directory structure if needed.
func (pm *PathManager) EnsureWorktreesDir(project string) error {
	dir := pm.ProjectWorktreesDir(project)
	return os.MkdirAll(dir, 0755)
}

// ListProjectWorktrees returns all worktree names for a project.
func (pm *PathManager) ListProjectWorktrees(project string) ([]string, error) {
	dir := pm.ProjectWorktreesDir(project)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var names []string
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			names = append(names, entry.Name())
		}
	}
	return names, nil
}

// ListAllProjects returns all project names that have worktrees.
func (pm *PathManager) ListAllProjects() ([]string, error) {
	root := pm.WorktreesRoot()
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var projects []string
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			projects = append(projects, entry.Name())
		}
	}
	return projects, nil
}
