package clone

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// gitClone performs the initial repository clone.
func (c *Cloner) gitClone(ctx context.Context) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	args := []string{"clone"}

	// Add --recurse-submodules unless disabled
	if !c.options.NoSubmodules {
		args = append(args, "--recurse-submodules")
	}

	if c.options.Branch != "" {
		args = append(args, "--branch", c.options.Branch)
	}

	if c.options.Depth > 0 {
		args = append(args, "--depth", fmt.Sprintf("%d", c.options.Depth))
	}

	args = append(args, c.options.URL)

	// Determine target directory
	targetDir := c.options.Directory
	if targetDir == "" {
		targetDir = extractRepoName(c.options.URL)
	}
	args = append(args, targetDir)

	cmd := exec.CommandContext(ctx, "git", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git clone: %s: %w", strings.TrimSpace(string(output)), err)
	}

	// Return absolute path to cloned directory
	absDir, err := filepath.Abs(targetDir)
	if err != nil {
		return targetDir, nil // Fall back to relative path
	}
	return absDir, nil
}

// gitSubmoduleSync synchronizes submodule URLs from .gitmodules to .git/config.
func (c *Cloner) gitSubmoduleSync(ctx context.Context, dir string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	cmd := exec.CommandContext(ctx, "git", "-C", dir, "submodule", "sync", "--recursive")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git submodule sync: %s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

// gitSubmoduleUpdate initializes and updates submodules.
func (c *Cloner) gitSubmoduleUpdate(ctx context.Context, dir string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	cmd := exec.CommandContext(ctx, "git", "-C", dir, "submodule", "update", "--init", "--recursive")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git submodule update: %s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

// gitGetBranch returns the current branch name.
func (c *Cloner) gitGetBranch(ctx context.Context, dir string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("get branch: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// gitSubmoduleStatus returns the status of all submodules.
func (c *Cloner) gitSubmoduleStatus(ctx context.Context, dir string) ([]SubmoduleResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Get submodule status
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "submodule", "status", "--recursive")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git submodule status: %w", err)
	}

	var results []SubmoduleResult
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		result := parseSubmoduleStatus(line)
		results = append(results, result)
	}

	// Get URLs for each submodule
	for i := range results {
		url, _ := c.gitSubmoduleURL(ctx, dir, results[i].Path)
		results[i].URL = url
	}

	return results, scanner.Err()
}

// gitSubmoduleURL returns the URL for a specific submodule.
func (c *Cloner) gitSubmoduleURL(ctx context.Context, dir, submodulePath string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	// Try to get URL from .gitmodules
	cmd := exec.CommandContext(ctx, "git", "-C", dir,
		"config", "-f", ".gitmodules", fmt.Sprintf("submodule.%s.url", submodulePath))
	output, err := cmd.Output()
	if err == nil {
		return strings.TrimSpace(string(output)), nil
	}

	// Fall back to .git/config
	cmd = exec.CommandContext(ctx, "git", "-C", dir,
		"config", fmt.Sprintf("submodule.%s.url", submodulePath))
	output, err = cmd.Output()
	if err == nil {
		return strings.TrimSpace(string(output)), nil
	}

	return "", fmt.Errorf("could not get URL for submodule %s", submodulePath)
}

// parseSubmoduleStatus parses a line from git submodule status output.
// Format: [+- ]<sha1> <path> (<describe>)
// Prefixes: '-' = not initialized, '+' = wrong commit, ' ' = OK
func parseSubmoduleStatus(line string) SubmoduleResult {
	result := SubmoduleResult{}

	if len(line) == 0 {
		return result
	}

	// Check prefix for status
	prefix := line[0]
	switch prefix {
	case '-':
		result.Success = false
		result.Error = fmt.Errorf("submodule not initialized")
		line = line[1:]
	case '+':
		// Commit differs - this might be OK after checkout
		result.Success = true
		line = line[1:]
	case ' ':
		result.Success = true
		line = line[1:]
	default:
		// No prefix
		result.Success = true
	}

	// Parse remaining: <sha1> <path> (<describe>)
	parts := strings.Fields(line)
	if len(parts) >= 2 {
		result.Commit = parts[0]
		result.Path = parts[1]
		result.Name = parts[1] // Use path as name
	}

	return result
}

// extractRepoName extracts repository name from a git URL.
func extractRepoName(url string) string {
	// Handle various URL formats:
	// git@github.com:org/repo.git
	// https://github.com/org/repo.git
	// https://github.com/org/repo
	// ssh://git@github.com/org/repo.git

	// Get the last path component
	url = strings.TrimSuffix(url, "/")

	// Handle SSH URLs with colon
	if idx := strings.LastIndex(url, ":"); idx > 0 {
		if !strings.Contains(url[idx:], "/") {
			url = url[idx+1:]
		}
	}

	// Get base name
	base := filepath.Base(url)

	// Remove .git suffix
	return strings.TrimSuffix(base, ".git")
}
