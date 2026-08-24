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
//
// os.Getwd() (the usual source of path) resolves symlinks on macOS, so a
// worktree whose holder path crosses a symlink (e.g.
// projects/worktrees/<name> → ../other-project/<name>) produces a canonical
// cwd that no longer starts with the logical worktrees root. To stay robust
// against that, the logical prefix check is supplemented with a resolved
// comparison: if the logical root did not match, resolve both sides through
// EvalSymlinks and try again before declaring the path outside the worktrees
// area.
func (pm *PathManager) ParseWorktreePath(path string) (project, name string, err error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", "", err
	}

	wtRoot := filepath.Clean(pm.WorktreesRoot())

	rel, ok := matchWorktreeRoot(absPath, wtRoot)
	if !ok {
		return "", "", ErrNotInWorktree
	}

	// Parse: <project>/<worktree>/...
	parts := strings.SplitN(rel, string(filepath.Separator), 3)
	if len(parts) < 2 {
		return "", "", ErrNotInWorktree
	}

	return parts[0], parts[1], nil
}

// matchWorktreeRoot returns the path relative to wtRoot if absPath falls
// inside the worktrees directory. It tries the logical (un-resolved) path
// first, then falls back to EvalSymlinks on both sides so a symlinked worktree
// holder is still recognised. The returned bool is false when the path is not
// inside the worktrees area by either measure.
func matchWorktreeRoot(absPath, wtRoot string) (string, bool) {
	if absPath == wtRoot || strings.HasPrefix(absPath, wtRoot+string(filepath.Separator)) {
		rel, err := filepath.Rel(wtRoot, absPath)
		if err == nil {
			return rel, true
		}
	}

	resolvedPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return "", false
	}
	resolvedRoot, err := filepath.EvalSymlinks(wtRoot)
	if err != nil {
		return "", false
	}
	resolvedRoot = filepath.Clean(resolvedRoot)
	resolvedPath = filepath.Clean(resolvedPath)

	if resolvedPath != resolvedRoot && !strings.HasPrefix(resolvedPath, resolvedRoot+string(filepath.Separator)) {
		return "", false
	}

	rel, err := filepath.Rel(resolvedRoot, resolvedPath)
	if err != nil {
		return "", false
	}
	return rel, true
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
