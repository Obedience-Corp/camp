package clone

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	gitpkg "github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/pathutil"
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
		return "", &SubmoduleError{Op: "clone", Cause: camperrors.Wrap(err, strings.TrimSpace(string(output)))}
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
		return &SubmoduleError{Op: "sync", Cause: camperrors.Wrap(err, strings.TrimSpace(string(output)))}
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
		return camperrors.WrapJoinf(gitpkg.ErrSubmoduleUpdate, err, "%s", strings.TrimSpace(string(output)))
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
		return "", camperrors.WrapJoin(gitpkg.ErrBranchDetection, err, "")
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
		return nil, camperrors.WrapJoin(gitpkg.ErrSubmoduleUpdate, err, "")
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

	return "", camperrors.Wrapf(gitpkg.ErrSubmoduleURL, "%s", submodulePath)
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
		result.Error = gitpkg.ErrSubmoduleNotInitialized
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

// gitCloneFromPeer clones the root repository from the peer copy, then
// re-points origin at the real URL and fetches the delta, so the resulting
// checkout is an origin replica that merely arrived over the fast path. Any
// failure after clone starts removes the partial clone and returns an error
// for the caller to fall back to the plain origin clone.
func (c *Cloner) gitCloneFromPeer(ctx context.Context) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	targetDir := c.options.Directory
	if targetDir == "" {
		targetDir = extractRepoName(c.options.URL)
	}
	if _, err := os.Stat(targetDir); err == nil {
		return "", camperrors.Newf("target directory %s already exists", targetDir)
	}

	// Clean up a partial destination on any error after we attempt the clone
	// (including cancel mid-clone). Without this the origin fallback fails
	// with "destination already exists".
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(targetDir)
		}
	}()

	args := []string{"clone"}
	if c.options.Branch != "" {
		args = append(args, "--branch", c.options.Branch)
	}
	if c.options.Depth > 0 {
		args = append(args, "--depth", fmt.Sprintf("%d", c.options.Depth))
	}
	args = append(args, c.peer.URL(""), targetDir)

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = c.peer.GitEnv()
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", camperrors.Wrapf(err, "clone from peer %s: %s", c.peer.ID(), strings.TrimSpace(string(output)))
	}

	if err := c.repointOrigin(ctx, targetDir); err != nil {
		return "", err
	}

	success = true
	absDir, err := filepath.Abs(targetDir)
	if err != nil {
		return targetDir, nil
	}
	return absDir, nil
}

// repointOrigin moves a peer-cloned repository onto its real origin: set-url,
// fetch the delta the peer did not have, and align the checkout with origin's
// tip of the requested (or default) branch. The reset is safe because the
// clone is fresh: there is no local work to lose, and clone semantics promise
// an origin replica.
func (c *Cloner) repointOrigin(ctx context.Context, dir string) error {
	steps := [][]string{
		{"remote", "set-url", "origin", c.options.URL},
		{"fetch", "--tags", "origin"},
		{"remote", "set-head", "origin", "--auto"},
	}
	for _, step := range steps {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, step...)...)
		if output, err := cmd.CombinedOutput(); err != nil {
			return camperrors.Wrapf(err, "re-point origin (git %s): %s",
				strings.Join(step, " "), strings.TrimSpace(string(output)))
		}
	}

	branch := c.options.Branch
	if branch == "" {
		cmd := exec.CommandContext(ctx, "git", "-C", dir, "symbolic-ref", "refs/remotes/origin/HEAD")
		output, err := cmd.Output()
		if err != nil {
			return camperrors.Wrap(err, "determine origin default branch")
		}
		branch = strings.TrimPrefix(strings.TrimSpace(string(output)), "refs/remotes/origin/")
	}

	cmd := exec.CommandContext(ctx, "git", "-C", dir, "checkout", "-B", branch, "--track", "origin/"+branch)
	if output, err := cmd.CombinedOutput(); err != nil {
		return camperrors.Wrapf(err, "checkout origin/%s: %s", branch, strings.TrimSpace(string(output)))
	}
	return nil
}

// seedSubmoduleFromPeer clones one submodule's objects from the peer copy
// instead of origin. The peer URL is supplied for that single `git submodule
// update` invocation via -c, so the parent's persisted config keeps the
// declared origin URL; afterwards the freshly created module's origin remote
// (which git set from the -c value) is re-pointed at the declared URL. No
// origin fetch runs here: the recorded gitlink SHA is either present from the
// peer (checkout proceeds with zero origin traffic) or the caller's graceful
// init fetches the delta from origin.
func (c *Cloner) seedSubmoduleFromPeer(ctx context.Context, repoDir string, sub SubmoduleInfo) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if strings.HasPrefix(sub.URL, "./") || strings.HasPrefix(sub.URL, "../") {
		return camperrors.New("relative submodule URL; origin init handles it")
	}

	initCmd := exec.CommandContext(ctx, "git", "-C", repoDir, "submodule", "init", sub.Path)
	_ = initCmd.Run() // may already be initialized, matching InitSubmoduleGraceful

	updateArgs := []string{"-C", repoDir,
		"-c", "submodule." + sub.Name + ".url=" + c.peer.URL(sub.Path)}
	if c.peer.IsFilesystem() {
		updateArgs = append(updateArgs, "-c", "protocol.file.allow=always")
	}
	updateArgs = append(updateArgs, "submodule", "update", sub.Path)
	updateCmd := exec.CommandContext(ctx, "git", updateArgs...)
	updateCmd.Env = c.peer.GitEnv()
	output, updateErr := updateCmd.CombinedOutput()

	// Always re-point when a module repo exists, even if update failed
	// partway (e.g. peer lacked the recorded SHA after a successful module
	// clone). Prefer a loud seed failure over leaving peer as origin.
	subDir := filepath.Join(repoDir, sub.Path)
	var repointErr error
	if _, statErr := os.Stat(filepath.Join(subDir, ".git")); statErr == nil {
		setURLCmd := exec.CommandContext(ctx, "git", "-C", subDir, "remote", "set-url", "origin", sub.URL)
		if setOut, setErr := setURLCmd.CombinedOutput(); setErr != nil {
			repointErr = camperrors.Wrapf(setErr, "re-point %s origin: %s",
				sub.Path, strings.TrimSpace(string(setOut)))
		}
	}

	if updateErr != nil {
		msg := fmt.Sprintf("seed %s from peer %s: %s",
			sub.Path, c.peer.ID(), strings.TrimSpace(string(output)))
		if repointErr != nil {
			return camperrors.Wrapf(updateErr, "%s (also re-point failed: %v)", msg, repointErr)
		}
		return camperrors.Wrapf(updateErr, "%s", msg)
	}
	if repointErr != nil {
		return repointErr
	}
	return nil
}

// RepoNameFromURL returns the repository name a clone of url would produce,
// exposed for callers that need the default directory/campaign name (e.g.
// resolving the same campaign on a peer machine).
func RepoNameFromURL(url string) string {
	return extractRepoName(url)
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
		return &SubmoduleError{Op: "read", Submodule: subPath, Cause: camperrors.WrapJoin(ErrSubmoduleRead, err, "")}
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
			Cause: camperrors.WrapJoinf(ErrCheckoutFailed, err, "%s", strings.TrimSpace(string(output))),
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
			Cause: camperrors.WrapJoinf(gitpkg.ErrBranchCheckout, err, "%s in %s: %s", branch, subPath, strings.TrimSpace(string(output))),
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

	if err := pathutil.ValidateSubmodulePath(repoDir, subPath); err != nil {
		return &SubmoduleError{Op: "validate-path", Submodule: subPath, Cause: err}
	}

	// Atomic init + update to avoid lock contention in parallel execution
	cmd := exec.CommandContext(ctx, "git", "-C", repoDir, "submodule", "update", "--init", subPath)
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
		Cause: camperrors.WrapJoinf(gitpkg.ErrSubmoduleUpdate, err, "%s", strings.TrimSpace(outputStr)),
	}
}

// initSubmoduleFromDefaultBranch clones a submodule at its remote's default branch
// instead of the recorded (possibly stale) commit. This is used as a fallback when
// the recorded commit no longer exists on the remote.
func (c *Cloner) initSubmoduleFromDefaultBranch(ctx context.Context, repoDir, subPath string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if err := pathutil.ValidateSubmodulePath(repoDir, subPath); err != nil {
		return &SubmoduleError{Op: "validate-path", Submodule: subPath, Cause: err}
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
			Cause: camperrors.WrapJoin(gitpkg.ErrStaleRef, err, ""),
		}
	}

	// Clone directly to submodule path (will use remote's default branch)
	cmd := exec.CommandContext(ctx, "git", "clone", url, subDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return &SubmoduleError{
			Op: "clone-default-branch", Submodule: subPath,
			Cause: camperrors.WrapJoinf(ErrCloneFailed, err, "%s", strings.TrimSpace(string(output))),
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

		if err := pathutil.ValidateSubmodulePath(subDir, nested.Path); err != nil {
			warnings = append(warnings,
				fmt.Sprintf("nested submodule %s/%s: invalid path: %v", subPath, nested.Path, err))
			continue
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
