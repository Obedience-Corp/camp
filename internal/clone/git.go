package clone

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
	url, declaredErr := gitpkg.GetDeclaredURL(ctx, dir, submodulePath)
	if declaredErr == nil {
		return url, nil
	}
	url, activeErr := gitpkg.GetActiveURL(ctx, dir, submodulePath)
	if activeErr == nil {
		return url, nil
	}
	return "", camperrors.WrapJoinf(gitpkg.ErrSubmoduleURL, errors.Join(declaredErr, activeErr), "%s", submodulePath)
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

// reportOrphanedGitlinks inventories undeclared gitlinks without changing
// the freshly cloned repository's index or working directories.
func (c *Cloner) reportOrphanedGitlinks(ctx context.Context, dir string) ([]gitpkg.OrphanedGitlink, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	orphans, err := gitpkg.ListOrphanedGitlinks(ctx, dir)
	if err != nil {
		return nil, err
	}

	return orphans, nil
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
	// --prune/--prune-tags drop the peer's branches (as origin/*) and tags that
	// the real origin does not have, so a peer-seeded clone is a true origin
	// replica rather than carrying the peer's private refs.
	steps := [][]string{
		{"remote", "set-url", "origin", c.options.URL},
		{"fetch", "--prune", "--prune-tags", "--tags", "origin"},
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
	if output, err := initCmd.CombinedOutput(); err != nil {
		return camperrors.Wrapf(err, "register peer-seeded submodule %s: %s",
			sub.Path, strings.TrimSpace(string(output)))
	}

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

	// The submodule's origin/* still reflect the PEER (the seed fetched from
	// the peer URL). Left alone, checkoutSubmoduleBranch would DWIM a local
	// branch off the peer's stale origin/<branch> and land on the peer's tip,
	// silently importing un-pushed peer commits instead of origin's state.
	// Refresh from the now-real origin: a cheap delta (the peer already
	// supplied the objects) that makes a peer-seeded submodule identical to a
	// normally-cloned one. A failure here means origin is unusable, so report
	// it as a seed failure and let graceful init fall back through origin.
	fetchCmd := exec.CommandContext(ctx, "git", "-C", subDir,
		"fetch", "--prune", "--prune-tags", "--tags", "origin")
	if out, err := fetchCmd.CombinedOutput(); err != nil {
		return camperrors.Wrapf(err, "refresh %s from origin after peer seed: %s",
			sub.Path, strings.TrimSpace(string(out)))
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

// checkoutSubmoduleBranch applies the shared parent-aware branch policy.
func (c *Cloner) checkoutSubmoduleBranch(ctx context.Context, repoDir, subPath string) error {
	_, err := gitpkg.CheckoutSubmoduleBranch(ctx, repoDir, subPath, false)
	if err != nil {
		return &SubmoduleError{
			Op: "checkout-branch", Submodule: subPath,
			Cause: err,
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
func (c *Cloner) initSubmoduleGraceful(ctx context.Context, repoDir, subPath string) (gitpkg.InitSubmoduleResult, error) {
	outcome, err := gitpkg.InitSubmoduleGracefulResult(ctx, repoDir, subPath)
	if err != nil {
		return outcome, &SubmoduleError{Op: "init", Submodule: subPath, Cause: err}
	}
	return outcome, nil
}

// initNestedSubmodulesGraceful returns one result per child of the selected
// checkout. A failed child does not prevent independent siblings from running.
func (c *Cloner) initNestedSubmodulesGraceful(ctx context.Context, repoDir, subPath string) []gitpkg.NestedResult {
	return c.initNestedSelected(ctx, filepath.Join(repoDir, subPath), subPath, map[string]bool{})
}

func (c *Cloner) initNestedSelected(ctx context.Context, ownerDir, ownerPath string, visited map[string]bool) []gitpkg.NestedResult {
	if ctx.Err() != nil {
		return []gitpkg.NestedResult{{Owner: ownerPath, Path: ownerPath, Skipped: true,
			Operation: "list", Error: &gitpkg.NestedError{Owner: ownerPath, Path: ownerPath, Op: "list", Cause: ctx.Err()}}}
	}
	actualOwner, err := filepath.EvalSymlinks(ownerDir)
	if err != nil {
		return []gitpkg.NestedResult{{Owner: ownerPath, Path: ownerPath, Attempted: true,
			Operation: "identify", Error: &gitpkg.NestedError{Owner: ownerPath, Path: ownerPath, Op: "identify", Cause: err}}}
	}
	if visited[actualOwner] {
		return []gitpkg.NestedResult{{Owner: ownerPath, Path: ownerPath, Attempted: true,
			Operation: "cycle", Error: &gitpkg.NestedError{Owner: ownerPath, Path: ownerPath, Op: "cycle", Cause: camperrors.Newf("repository already visited: %s", actualOwner)}}}
	}
	visited[actualOwner] = true
	defer delete(visited, actualOwner)
	nestedSubs, err := parseGitmodules(ctx, ownerDir)
	if err != nil {
		return []gitpkg.NestedResult{{Owner: ownerPath, Path: ownerPath, Attempted: true,
			Operation: "list", Error: &gitpkg.NestedError{Owner: ownerPath, Path: ownerPath, Op: "list", Cause: err}}}
	}
	sort.Slice(nestedSubs, func(i, j int) bool { return nestedSubs[i].Path < nestedSubs[j].Path })
	results := make([]gitpkg.NestedResult, 0, len(nestedSubs))
	for _, nested := range nestedSubs {
		fullPath := filepath.Join(ownerPath, nested.Path)
		r := gitpkg.NestedResult{Owner: ownerPath, Path: fullPath, Name: nested.Name, URL: nested.URL}
		fail := func(op string, cause error) {
			r.Operation = op
			r.Error = &gitpkg.NestedError{Owner: ownerPath, Path: fullPath, Op: op, Cause: cause}
		}
		if ctx.Err() != nil {
			r.Skipped = true
			fail("skip", ctx.Err())
			results = append(results, r)
			continue
		}
		r.Attempted = true
		if err := pathutil.ValidateSubmodulePath(ownerDir, nested.Path); err != nil {
			fail("validate-path", err)
			results = append(results, r)
			continue
		}
		childDir := filepath.Join(ownerDir, nested.Path)
		initialized, identityErr := gitpkg.IsWorktreeRoot(ctx, childDir)
		if identityErr != nil {
			fail("identify", identityErr)
			results = append(results, r)
			continue
		}
		if initialized {
			cmd := exec.CommandContext(ctx, "git", "-C", ownerDir, "submodule", "sync", "--", nested.Path)
			if output, err := cmd.CombinedOutput(); err != nil {
				fail("sync-url", camperrors.Wrapf(err, "%s", strings.TrimSpace(string(output))))
				results = append(results, r)
				continue
			}
		}
		outcome, err := c.initSubmoduleGraceful(ctx, ownerDir, nested.Path)
		if err != nil {
			fail("init", err)
			results = append(results, r)
			continue
		}
		r.Initialized = true
		r.StaleRefFallback = outcome.StaleRefFallback
		if err := c.checkoutSubmoduleBranch(ctx, ownerDir, nested.Path); err != nil {
			fail("checkout", err)
			results = append(results, r)
			continue
		}
		r.Commit, _ = gitpkg.Output(ctx, childDir, "rev-parse", "HEAD")
		if pin, err := gitpkg.Output(ctx, ownerDir, "ls-files", "-s", "--", nested.Path); err == nil {
			fields := strings.Fields(pin)
			if len(fields) >= 2 && fields[0] == "160000" {
				r.RecordedCommit = fields[1]
			}
		}
		r.Children = c.initNestedSelected(ctx, childDir, fullPath, visited)
		if err := gitpkg.NestedFailures(r.Children); err != nil {
			fail("children", err)
		} else {
			r.Success = true
		}
		results = append(results, r)
	}
	return results
}
