package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
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
		return false, camperrors.Wrapf(err, "cannot access .git at %s", path)
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
		return "", camperrors.Wrapf(err, "cannot resolve absolute path for %s", path)
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

// GetDeclaredURL returns the URL declared in .gitmodules for a submodule.
// This is the shared, tracked URL that all clones should use.
func GetDeclaredURL(ctx context.Context, repoRoot, submodulePath string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot,
		"config", "-f", ".gitmodules",
		fmt.Sprintf("submodule.%s.url", submodulePath))

	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return "", fmt.Errorf("submodule %q not found in .gitmodules", submodulePath)
		}
		return "", camperrors.Wrapf(err, "get declared URL for %s", submodulePath)
	}

	return strings.TrimSpace(string(output)), nil
}

// GetActiveURL returns the URL currently configured in .git/config for a submodule.
// This is the local URL that git actually uses for fetch/push operations.
// Returns empty string if the submodule is not yet initialized.
func GetActiveURL(ctx context.Context, repoRoot, submodulePath string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot,
		"config", fmt.Sprintf("submodule.%s.url", submodulePath))

	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			// Exit code 1 means key not found - submodule not initialized
			return "", nil
		}
		return "", camperrors.Wrapf(err, "get active URL for %s", submodulePath)
	}

	return strings.TrimSpace(string(output)), nil
}

// URLComparison contains the result of comparing declared and active URLs.
type URLComparison struct {
	// Match indicates whether the URLs match after normalization.
	Match bool
	// DeclaredURL is the URL from .gitmodules.
	DeclaredURL string
	// ActiveURL is the URL from .git/config (empty if not initialized).
	ActiveURL string
}

// CompareURLs checks if the declared and active URLs match for a submodule.
// Handles normalization of trailing slashes for accurate comparison.
func CompareURLs(ctx context.Context, repoRoot, submodulePath string) (*URLComparison, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	declared, err := GetDeclaredURL(ctx, repoRoot, submodulePath)
	if err != nil {
		return nil, err
	}

	active, err := GetActiveURL(ctx, repoRoot, submodulePath)
	if err != nil {
		return nil, err
	}

	result := &URLComparison{
		DeclaredURL: declared,
		ActiveURL:   active,
	}

	// Normalize URLs before comparison
	normalizedDeclared := normalizeGitURL(declared)
	normalizedActive := normalizeGitURL(active)

	result.Match = normalizedDeclared == normalizedActive
	return result, nil
}

// normalizeGitURL normalizes a git URL for comparison.
// Removes trailing slashes but preserves .git suffix as it matters for some servers.
func normalizeGitURL(url string) string {
	url = strings.TrimSpace(url)
	url = strings.TrimSuffix(url, "/")
	return url
}

// IsLocalFilesystemURL returns true if the URL is a local filesystem path
// rather than a real remote URL (SSH or HTTPS). Matches absolute paths,
// relative paths (./  ../), and file:// protocol URLs.
func IsLocalFilesystemURL(url string) bool {
	url = strings.TrimSpace(url)
	if url == "" {
		return false
	}

	// file:// protocol
	if strings.HasPrefix(url, "file://") {
		return true
	}

	// Absolute path (Unix)
	if strings.HasPrefix(url, "/") {
		return true
	}

	// Relative paths
	if strings.HasPrefix(url, "./") || strings.HasPrefix(url, "../") {
		return true
	}

	return false
}

// RemoteOriginURL returns the origin remote URL configured inside a submodule.
// Returns empty string if no origin remote is configured.
func RemoteOriginURL(ctx context.Context, submodulePath string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	cmd := exec.CommandContext(ctx, "git", "-C", submodulePath, "remote", "get-url", "origin")
	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// No origin remote configured
			return "", nil
		}
		return "", camperrors.Wrapf(err, "get remote origin URL for %s", submodulePath)
	}

	return strings.TrimSpace(string(output)), nil
}

// SetDeclaredURL updates the URL in .gitmodules for a submodule.
func SetDeclaredURL(ctx context.Context, repoRoot, submodulePath, newURL string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot,
		"config", "-f", ".gitmodules",
		fmt.Sprintf("submodule.%s.url", submodulePath), newURL)

	if output, err := cmd.CombinedOutput(); err != nil {
		return camperrors.Wrapf(err, "set declared URL for %s: %s", submodulePath, strings.TrimSpace(string(output)))
	}

	return nil
}
