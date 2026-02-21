package clone

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	gitpkg "github.com/obediencecorp/camp/internal/git"
)

// gitClone performs the initial repository clone.
// Note: We do NOT use --recurse-submodules because if any nested submodule
// has a stale commit reference (commit no longer exists on remote), git will
// abort the ENTIRE clone. Instead, we clone the main repo and then initialize
// submodules one-by-one with graceful error handling.
func (c *Cloner) gitClone(ctx context.Context) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	args := []string{"clone"}

	// NOTE: --recurse-submodules removed. Submodules are initialized manually
	// via initSubmoduleGraceful() to handle stale commit references gracefully.

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
		return "", &SubmoduleError{Op: "clone", Cause: fmt.Errorf("%s: %w", strings.TrimSpace(string(output)), err)}
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
		return &SubmoduleError{Op: "sync", Cause: fmt.Errorf("%s: %w", strings.TrimSpace(string(output)), err)}
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
		return fmt.Errorf("%w: %s: %w", ErrSubmoduleUpdate, strings.TrimSpace(string(output)), err)
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
		return "", fmt.Errorf("%w: %w", ErrBranchDetection, err)
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
		return nil, fmt.Errorf("%w: %w", ErrSubmoduleUpdate, err)
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

	return "", fmt.Errorf("%w: %s", ErrSubmoduleURL, submodulePath)
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
		result.Error = ErrSubmoduleNotInitialized
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

// cleanOrphanedGitlinks detects and removes gitlink entries in the index that
// have no corresponding .gitmodules declaration. These orphans cause
// `git submodule sync` and `git submodule status` to fail with
// "no submodule mapping found in .gitmodules", which cascades and prevents
// other submodules from initializing properly.
func (c *Cloner) cleanOrphanedGitlinks(ctx context.Context, dir string) ([]string, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	orphans, err := gitpkg.ListOrphanedGitlinks(ctx, dir)
	if err != nil {
		return nil, err
	}

	if len(orphans) == 0 {
		return nil, nil
	}

	removed, err := gitpkg.RemoveOrphanedGitlinks(ctx, dir, orphans)
	if err != nil {
		return removed, err
	}

	return removed, nil
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

// verifySubmoduleWorkingTree checks that a submodule has files checked out.
// Returns an error if the working tree is empty (only .git file exists).
func (c *Cloner) verifySubmoduleWorkingTree(ctx context.Context, repoDir, subPath string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	subDir := filepath.Join(repoDir, subPath)
	entries, err := os.ReadDir(subDir)
	if err != nil {
		return &SubmoduleError{Op: "read", Submodule: subPath, Cause: fmt.Errorf("%w: %w", ErrSubmoduleRead, err)}
	}

	// Count real files (not .git)
	realEntries := 0
	for _, e := range entries {
		if e.Name() != ".git" {
			realEntries++
		}
	}

	if realEntries == 0 {
		return &SubmoduleError{Op: "verify", Submodule: subPath, Cause: ErrEmptyWorkingTree}
	}
	return nil
}

// forceCheckoutSubmodule forces a checkout of HEAD in the submodule.
func (c *Cloner) forceCheckoutSubmodule(ctx context.Context, repoDir, subPath string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	subDir := filepath.Join(repoDir, subPath)
	cmd := exec.CommandContext(ctx, "git", "-C", subDir, "checkout", "HEAD", "--", ".")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return &SubmoduleError{
			Op: "checkout", Submodule: subPath,
			Cause: fmt.Errorf("%w: %s: %w", ErrCheckoutFailed, strings.TrimSpace(string(output)), err),
		}
	}
	return nil
}

// checkoutSubmoduleBranch checks out the remote's default branch instead of detached HEAD.
// Uses the parent-aware detection (checks .gitmodules branch key) then falls back to
// the shared CheckoutDefaultBranch utility.
func (c *Cloner) checkoutSubmoduleBranch(ctx context.Context, repoDir, subPath string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	subDir := filepath.Join(repoDir, subPath)

	// Try parent-aware detection first (has access to .gitmodules branch key)
	branch, err := gitpkg.DetectDefaultBranchWithParent(ctx, repoDir, subPath, subDir)
	if err != nil {
		// Fall back to standard detection
		_, err = gitpkg.CheckoutDefaultBranch(ctx, subDir)
		return err
	}

	// Checkout the detected branch
	cmd := exec.CommandContext(ctx, "git", "-C", subDir, "checkout", branch)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return &SubmoduleError{
			Op: "checkout-branch", Submodule: subPath,
			Cause: fmt.Errorf("%w: %s in %s: %s", ErrBranchCheckout, branch, subPath, strings.TrimSpace(string(output))),
		}
	}

	return nil
}

// isStaleRefError checks if an error indicates a stale commit reference.
func (c *Cloner) isStaleRefError(err error) bool {
	if err == nil {
		return false
	}
	// Check via shared utility first
	if gitpkg.IsStaleRefError(err) {
		return true
	}
	// Also check for our own fallback marker
	return strings.Contains(err.Error(), "using default branch")
}

// getSubmoduleCommit returns the current HEAD commit of a submodule.
func (c *Cloner) getSubmoduleCommit(ctx context.Context, repoDir, subPath string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	subDir := filepath.Join(repoDir, subPath)
	cmd := exec.CommandContext(ctx, "git", "-C", subDir, "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// initSubmoduleGraceful initializes a single submodule, handling stale commit references.
// If the recorded commit doesn't exist on the remote, it falls back to cloning at the
// remote's default branch instead.
func (c *Cloner) initSubmoduleGraceful(ctx context.Context, repoDir, subPath string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Step 1: Register the submodule (init without update)
	cmd := exec.CommandContext(ctx, "git", "-C", repoDir, "submodule", "init", subPath)
	_ = cmd.Run() // Ignore error, may already be initialized

	// Step 2: Try normal update
	cmd = exec.CommandContext(ctx, "git", "-C", repoDir, "submodule", "update", subPath)
	output, err := cmd.CombinedOutput()

	if err == nil {
		return nil // Success
	}

	outputStr := string(output)

	// Step 3: Check if error is stale reference
	// Common error messages for stale commits:
	// - "not our ref" - remote doesn't have the commit
	// - "did not contain" - fetched but commit missing
	// - "reference is not a tree" - commit doesn't exist
	if strings.Contains(outputStr, "not our ref") ||
		strings.Contains(outputStr, "did not contain") ||
		strings.Contains(outputStr, "reference is not a tree") {
		// Stale reference - fall back to default branch
		return c.initSubmoduleFromDefaultBranch(ctx, repoDir, subPath)
	}

	return &SubmoduleError{
		Op: "update", Submodule: subPath,
		Cause: fmt.Errorf("%w: %s", ErrSubmoduleUpdate, strings.TrimSpace(outputStr)),
	}
}

// initSubmoduleFromDefaultBranch clones a submodule at its remote's default branch
// instead of the recorded (possibly stale) commit. This is used as a fallback when
// the recorded commit no longer exists on the remote.
func (c *Cloner) initSubmoduleFromDefaultBranch(ctx context.Context, repoDir, subPath string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Get submodule URL
	url, err := c.gitSubmoduleURL(ctx, repoDir, subPath)
	if err != nil {
		return &SubmoduleError{Op: "url-resolve", Submodule: subPath, Cause: err}
	}

	subDir := filepath.Join(repoDir, subPath)

	// Remove empty submodule directory if exists
	if err := os.RemoveAll(subDir); err != nil {
		return &SubmoduleError{
			Op: "remove-stale", Submodule: subPath,
			Cause: fmt.Errorf("%w: %w", ErrStaleRef, err),
		}
	}

	// Clone directly to submodule path (will use remote's default branch)
	cmd := exec.CommandContext(ctx, "git", "clone", url, subDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return &SubmoduleError{
			Op: "clone-default-branch", Submodule: subPath,
			Cause: fmt.Errorf("%w: %s", ErrCloneFailed, strings.TrimSpace(string(output))),
		}
	}

	return nil
}

// initNestedSubmodulesGraceful initializes nested submodules within a submodule,
// handling stale commit references gracefully by falling back to default branch.
func (c *Cloner) initNestedSubmodulesGraceful(ctx context.Context, repoDir, subPath string) (int, []string) {
	if ctx.Err() != nil {
		return 0, nil
	}

	subDir := filepath.Join(repoDir, subPath)

	// Check if this submodule has its own .gitmodules
	nestedSubs, err := parseGitmodules(ctx, subDir)
	if err != nil || len(nestedSubs) == 0 {
		return 0, nil // No nested submodules
	}

	var warnings []string
	count := 0

	for _, nested := range nestedSubs {
		if ctx.Err() != nil {
			break
		}

		// Use graceful init for each nested submodule
		if err := c.initSubmoduleGraceful(ctx, subDir, nested.Path); err != nil {
			warnings = append(warnings,
				fmt.Sprintf("nested submodule %s/%s: %v (using default branch fallback)", subPath, nested.Path, err))

			// Try fallback directly
			if fbErr := c.initSubmoduleFromDefaultBranch(ctx, subDir, nested.Path); fbErr != nil {
				warnings = append(warnings,
					fmt.Sprintf("nested submodule %s/%s fallback failed: %v", subPath, nested.Path, fbErr))
				continue
			}
		}
		count++

		// Recursively initialize any sub-nested submodules
		nestedCount, nestedWarnings := c.initNestedSubmodulesGraceful(ctx, subDir, nested.Path)
		count += nestedCount
		warnings = append(warnings, nestedWarnings...)
	}

	return count, warnings
}
