package git

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// FindIndexLocks returns all index.lock files in the given git directory
// and its submodules. The gitDir should be the path to the .git directory.
func FindIndexLocks(ctx context.Context, gitDir string) ([]string, error) {
	var locks []string

	// Check context
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Validate gitDir exists
	info, err := os.Stat(gitDir)
	if err != nil {
		return nil, fmt.Errorf("git directory not accessible: %s: %w", gitDir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("path is not a directory: %s", gitDir)
	}

	// Check main index.lock
	mainLock := filepath.Join(gitDir, "index.lock")
	if _, err := os.Stat(mainLock); err == nil {
		locks = append(locks, mainLock)
	}

	// Check modules directory for submodule locks
	modulesDir := filepath.Join(gitDir, "modules")
	if info, err := os.Stat(modulesDir); err == nil && info.IsDir() {
		subLocks, _ := findLocksInModules(ctx, modulesDir)
		// Errors are logged but not fatal - some modules might be inaccessible
		locks = append(locks, subLocks...)
	}

	return locks, nil
}

// findLocksInModules recursively searches for index.lock files in the modules directory.
func findLocksInModules(ctx context.Context, modulesDir string) ([]string, error) {
	var locks []string

	err := filepath.WalkDir(modulesDir, func(path string, d fs.DirEntry, err error) error {
		// Check context for cancellation
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Skip inaccessible paths
		if err != nil {
			return nil
		}

		// Look for index.lock files
		if d.Name() == "index.lock" && !d.IsDir() {
			locks = append(locks, path)
		}

		return nil
	})

	return locks, err
}

// FindLocksInRepository finds all index.lock files starting from a repository root.
// It automatically locates the .git directory (handling both regular repos and submodules).
func FindLocksInRepository(ctx context.Context, repoRoot string) ([]string, error) {
	gitDir, err := ResolveGitDir(repoRoot)
	if err != nil {
		return nil, err
	}
	return FindIndexLocks(ctx, gitDir)
}

// ResolveGitDir finds the actual .git directory path.
// For regular repos: <root>/.git (directory)
// For submodules: reads gitdir from <root>/.git (file)
func ResolveGitDir(repoRoot string) (string, error) {
	gitPath := filepath.Join(repoRoot, ".git")

	info, err := os.Stat(gitPath)
	if err != nil {
		return "", fmt.Errorf("cannot access .git at %s: %w", gitPath, err)
	}

	// Regular repository - .git is a directory
	if info.IsDir() {
		return gitPath, nil
	}

	// Submodule - .git is a file containing "gitdir: <path>"
	content, err := os.ReadFile(gitPath)
	if err != nil {
		return "", fmt.Errorf("cannot read .git file at %s: %w", gitPath, err)
	}

	return parseGitdirFile(repoRoot, string(content))
}

// parseGitdirFile extracts the git directory path from a gitdir file.
func parseGitdirFile(repoRoot, content string) (string, error) {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "gitdir: ") {
		return "", fmt.Errorf("invalid .git file format: %s", content)
	}

	relPath := strings.TrimPrefix(content, "gitdir: ")

	// If path is absolute, return it directly
	if filepath.IsAbs(relPath) {
		return relPath, nil
	}

	// Path is relative to the repo root
	return filepath.Join(repoRoot, relPath), nil
}

// HasLockFile checks if a lock file exists at the given git directory.
func HasLockFile(gitDir string) bool {
	lockPath := filepath.Join(gitDir, "index.lock")
	_, err := os.Stat(lockPath)
	return err == nil
}
