package worktree

import (
	"path/filepath"
	"strings"
)

// IsLinkedWorktree reports whether entry represents a real, navigable linked
// worktree for the project rooted at projectPath: not the project's own main
// working tree, not bare, not a git-internal path (including the submodule
// main-worktree case reported under <superproject>/.git/modules/<name>), and
// not a hidden directory.
//
// Callers that enumerate worktrees for a project should filter git worktree
// list entries through this before displaying or navigating to them, rather
// than intersecting with a directory scan of a conventional location: git is
// the source of truth for which worktrees exist and where they live.
func IsLinkedWorktree(projectPath string, entry GitWorktreeEntry) bool {
	if entry.IsBare || entry.Path == "" {
		return false
	}

	clean := filepath.Clean(entry.Path)
	if clean == filepath.Clean(projectPath) || containsGitDir(clean) {
		return false
	}

	name := filepath.Base(clean)
	return name != "" && name != "." && !strings.HasPrefix(name, ".")
}

// containsGitDir reports whether path has a ".git" path component.
func containsGitDir(path string) bool {
	for _, part := range strings.Split(path, string(filepath.Separator)) {
		if part == ".git" {
			return true
		}
	}
	return false
}
