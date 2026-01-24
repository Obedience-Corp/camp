package git

import (
	"fmt"
	"os"
	"path/filepath"
)

// IsSubmodule checks if the given path is a git submodule.
// Returns true if .git is a file (gitdir pointer) rather than a directory.
func IsSubmodule(path string) (bool, error) {
	gitPath := filepath.Join(path, ".git")

	info, err := os.Stat(gitPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil // No .git at all
		}
		return false, fmt.Errorf("cannot access .git at %s: %w", path, err)
	}

	// Submodules have .git as a file, regular repos have it as a directory
	return !info.IsDir(), nil
}

// GetSubmoduleGitDir resolves the actual .git directory for a submodule.
// For regular repos, returns <path>/.git.
// For submodules, parses the gitdir file and resolves the path.
// This is an alias for ResolveGitDir for clarity in submodule-specific code.
func GetSubmoduleGitDir(path string) (string, error) {
	return ResolveGitDir(path)
}

// FindProjectRoot walks up from the given path to find the git repository root.
// Returns the path containing the .git directory/file.
func FindProjectRoot(path string) (string, error) {
	// Make path absolute
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("cannot resolve absolute path for %s: %w", path, err)
	}

	current := absPath
	for {
		gitPath := filepath.Join(current, ".git")
		if _, err := os.Stat(gitPath); err == nil {
			return current, nil
		}

		// Move up one directory
		parent := filepath.Dir(current)
		if parent == current {
			// Reached filesystem root
			return "", fmt.Errorf("not inside a git repository: %s", path)
		}
		current = parent
	}
}

// FindProjectRootWithType returns the project root and whether it's a submodule.
func FindProjectRootWithType(path string) (root string, isSubmodule bool, err error) {
	root, err = FindProjectRoot(path)
	if err != nil {
		return "", false, err
	}

	isSubmodule, err = IsSubmodule(root)
	if err != nil {
		return root, false, err
	}

	return root, isSubmodule, nil
}

// FindParentRepository finds the parent repository of a submodule.
// Returns empty string if the submodule is not nested in another repo.
func FindParentRepository(submodulePath string) (string, error) {
	root, err := FindProjectRoot(submodulePath)
	if err != nil {
		return "", err
	}

	// Check if this root is itself within another repository
	parent := filepath.Dir(root)
	parentRoot, err := FindProjectRoot(parent)
	if err != nil {
		// Not nested - no parent repository
		return "", nil
	}

	return parentRoot, nil
}

// SubmoduleInfo contains information about a submodule.
type SubmoduleInfo struct {
	Path       string // Path to submodule root
	GitDir     string // Actual .git directory path
	ParentRepo string // Path to parent repository (if nested)
}

// GetSubmoduleInfo returns complete information about a submodule.
func GetSubmoduleInfo(path string) (*SubmoduleInfo, error) {
	root, isSubmodule, err := FindProjectRootWithType(path)
	if err != nil {
		return nil, err
	}

	if !isSubmodule {
		return nil, fmt.Errorf("path is not in a submodule: %s", path)
	}

	gitDir, err := GetSubmoduleGitDir(root)
	if err != nil {
		return nil, err
	}

	parentRepo, _ := FindParentRepository(root) // Ignore error - may not have parent

	return &SubmoduleInfo{
		Path:       root,
		GitDir:     gitDir,
		ParentRepo: parentRepo,
	}, nil
}
