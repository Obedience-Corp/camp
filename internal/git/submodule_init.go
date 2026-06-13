package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/pathutil"
)

// IsStaleRefError checks if an error indicates a stale commit reference.
// Common messages: "not our ref", "did not contain", "reference is not a tree".
func IsStaleRefError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "not our ref") ||
		strings.Contains(errStr, "did not contain") ||
		strings.Contains(errStr, "reference is not a tree")
}

// isDirEmpty reports whether dir exists and contains no files or directories
// (ignoring the case where dir does not exist at all).
func isDirEmpty(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}

// resolveSubmoduleURL resolves a potentially relative submodule URL against
// the superproject's remote origin URL. Relative means starting with "../" or "./".
// If the URL is not relative, it is returned unchanged.
func resolveSubmoduleURL(ctx context.Context, repoDir, url string) string {
	if !strings.HasPrefix(url, "../") && !strings.HasPrefix(url, "./") {
		return url
	}
	remoteURL, err := RemoteOriginURL(ctx, repoDir)
	if err != nil || remoteURL == "" {
		return url
	}
	// filepath.Join is wrong here (would strip the scheme); use path.Join on
	// the path component only, then reassemble.
	base := strings.TrimSuffix(remoteURL, "/")
	// Strip trailing component to get the "parent" remote dir.
	slash := strings.LastIndex(base, "/")
	if slash < 0 {
		return url
	}
	parent := base[:slash]
	rel := strings.TrimPrefix(url, "../")
	return parent + "/" + rel
}

// DetectDefaultBranch determines the default branch for a submodule using
// local-first detection. This avoids the expensive `git remote show origin`
// network call.
//
// Detection order:
//  1. git symbolic-ref refs/remotes/origin/HEAD (local, set after clone/fetch)
//  2. Try "main" then "master" by checking refs/remotes/origin/<branch>
func DetectDefaultBranch(ctx context.Context, subDir string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	// Strategy 1: Check symbolic-ref (populated after clone/fetch, no network)
	cmd := exec.CommandContext(ctx, "git", "-C", subDir, "symbolic-ref", "refs/remotes/origin/HEAD")
	output, err := cmd.Output()
	if err == nil {
		ref := strings.TrimSpace(string(output))
		// ref looks like "refs/remotes/origin/main"
		branch := strings.TrimPrefix(ref, "refs/remotes/origin/")
		if branch != ref && branch != "" {
			return branch, nil
		}
	}

	// Strategy 2: Check if main or master exists as a remote tracking branch
	for _, candidate := range []string{"main", "master"} {
		cmd = exec.CommandContext(ctx, "git", "-C", subDir,
			"rev-parse", "--verify", "--quiet", fmt.Sprintf("refs/remotes/origin/%s", candidate))
		if err := cmd.Run(); err == nil {
			return candidate, nil
		}
	}

	return "", camperrors.Wrapf(ErrBranchDetection, "%s", subDir)
}

// DetectDefaultBranchWithParent determines the default branch for a submodule,
// also checking the parent repo's .gitmodules for a branch key.
func DetectDefaultBranchWithParent(ctx context.Context, repoDir, subPath, subDir string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	// Strategy 1: Check symbolic-ref (populated after clone/fetch, no network)
	cmd := exec.CommandContext(ctx, "git", "-C", subDir, "symbolic-ref", "refs/remotes/origin/HEAD")
	output, err := cmd.Output()
	if err == nil {
		ref := strings.TrimSpace(string(output))
		branch := strings.TrimPrefix(ref, "refs/remotes/origin/")
		if branch != ref && branch != "" {
			return branch, nil
		}
	}

	// Strategy 2: Check .gitmodules for explicit branch key
	cmd = exec.CommandContext(ctx, "git", "-C", repoDir,
		"config", "-f", ".gitmodules", fmt.Sprintf("submodule.%s.branch", subPath))
	output, err = cmd.Output()
	if err == nil {
		branch := strings.TrimSpace(string(output))
		if branch != "" && branch != "." {
			return branch, nil
		}
	}

	// Strategy 3: Check if main or master exists as a remote tracking branch
	for _, candidate := range []string{"main", "master"} {
		cmd = exec.CommandContext(ctx, "git", "-C", subDir,
			"rev-parse", "--verify", "--quiet", fmt.Sprintf("refs/remotes/origin/%s", candidate))
		if err := cmd.Run(); err == nil {
			return candidate, nil
		}
	}

	return "", camperrors.Wrapf(ErrBranchDetection, "%s", subPath)
}

// InitSubmoduleGraceful initializes a single submodule, handling stale commit references.
// If the recorded commit doesn't exist on the remote, it falls back to cloning at the
// remote's default branch instead.
func InitSubmoduleGraceful(ctx context.Context, repoDir, subPath string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if err := pathutil.ValidateSubmodulePath(repoDir, subPath); err != nil {
		return camperrors.WrapJoinf(ErrSubmoduleInit, err, "%s", subPath)
	}

	// Step 1: Register the submodule (init without update)
	cmd := exec.CommandContext(ctx, "git", "-C", repoDir, "submodule", "init", subPath)
	_ = cmd.Run() // Ignore error, may already be initialized

	// Step 2: Try normal update
	cmd = exec.CommandContext(ctx, "git", "-C", repoDir, "submodule", "update", subPath)
	output, err := cmd.CombinedOutput()

	if err == nil {
		return nil
	}

	outputStr := string(output)

	// Step 3: Check if error is stale reference
	if strings.Contains(outputStr, "not our ref") ||
		strings.Contains(outputStr, "did not contain") ||
		strings.Contains(outputStr, "reference is not a tree") {
		return InitFromDefaultBranch(ctx, repoDir, subPath)
	}

	return camperrors.WrapJoinf(ErrSubmoduleUpdate, err, "%s: %s", subPath, strings.TrimSpace(outputStr))
}

// InitFromDefaultBranch clones a submodule at its remote's default branch
// instead of the recorded (possibly stale) commit. This is used as a fallback when
// the recorded commit no longer exists on the remote.
func InitFromDefaultBranch(ctx context.Context, repoDir, subPath string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if err := pathutil.ValidateSubmodulePath(repoDir, subPath); err != nil {
		return camperrors.WrapJoinf(ErrSubmoduleClone, err, "%s", subPath)
	}

	url, err := getSubmoduleURL(ctx, repoDir, subPath)
	if err != nil {
		return camperrors.WrapJoinf(ErrSubmoduleURL, err, "%s", subPath)
	}
	url = resolveSubmoduleURL(ctx, repoDir, url)

	subDir := filepath.Join(repoDir, subPath)

	empty, err := isDirEmpty(subDir)
	if err != nil {
		return camperrors.WrapJoinf(ErrSubmoduleRemove, err, "checking %s", subPath)
	}

	if !empty {
		// Quarantine-rename instead of RemoveAll so uncommitted content survives.
		ts := time.Now().UTC().Format("20060102-150405")
		quarantine := subDir + ".sync-quarantine-" + ts
		if renErr := os.Rename(subDir, quarantine); renErr != nil {
			return camperrors.WrapJoinf(ErrSubmoduleRemove, renErr,
				"%s: non-empty dir cannot be quarantined (inspect %s)", subPath, quarantine)
		}
	} else {
		if err := os.RemoveAll(subDir); err != nil {
			return camperrors.WrapJoinf(ErrSubmoduleRemove, err, "%s", subPath)
		}
	}

	// Fetch into the existing .git/modules wiring, then update to initialize.
	// This preserves the submodule structure rather than creating a standalone clone.
	fetchCmd := exec.CommandContext(ctx, "git", "-C", repoDir,
		"fetch", "--recurse-submodules=no", url,
		"refs/heads/*:refs/remotes/origin/*")
	if output, fetchErr := fetchCmd.CombinedOutput(); fetchErr != nil {
		// Fetch failed; fall back to re-init and update.
		initCmd := exec.CommandContext(ctx, "git", "-C", repoDir, "submodule", "init", subPath)
		_ = initCmd.Run()
		updateCmd := exec.CommandContext(ctx, "git", "-C", repoDir,
			"submodule", "update", "--init", subPath)
		if out2, updateErr := updateCmd.CombinedOutput(); updateErr != nil {
			return camperrors.WrapJoinf(ErrSubmoduleClone, updateErr,
				"%s: fetch failed (%s) and update failed: %s",
				subPath, strings.TrimSpace(string(output)), strings.TrimSpace(string(out2)))
		}
		return nil
	}

	updateCmd := exec.CommandContext(ctx, "git", "-C", repoDir,
		"submodule", "update", "--init", subPath)
	if output, updateErr := updateCmd.CombinedOutput(); updateErr != nil {
		return camperrors.WrapJoinf(ErrSubmoduleUpdate, updateErr,
			"%s: %s", subPath, strings.TrimSpace(string(output)))
	}

	return nil
}

// CheckoutDefaultBranch detects the default branch for a submodule and checks it out,
// moving it from detached HEAD to a proper branch. Returns the branch name on success.
func CheckoutDefaultBranch(ctx context.Context, subDir string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	branch, err := DetectDefaultBranch(ctx, subDir)
	if err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, "git", "-C", subDir, "checkout", branch)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", camperrors.WrapJoinf(ErrBranchCheckout, err, "%s: %s", branch, strings.TrimSpace(string(output)))
	}

	return branch, nil
}

// getSubmoduleURL returns the URL for a submodule, trying .gitmodules then .git/config.
func getSubmoduleURL(ctx context.Context, repoDir, subPath string) (string, error) {
	// Try .gitmodules first
	cmd := exec.CommandContext(ctx, "git", "-C", repoDir,
		"config", "-f", ".gitmodules", fmt.Sprintf("submodule.%s.url", subPath))
	output, err := cmd.Output()
	if err == nil {
		return strings.TrimSpace(string(output)), nil
	}

	// Fall back to .git/config
	cmd = exec.CommandContext(ctx, "git", "-C", repoDir,
		"config", fmt.Sprintf("submodule.%s.url", subPath))
	output, err = cmd.Output()
	if err == nil {
		return strings.TrimSpace(string(output)), nil
	}

	return "", camperrors.Wrapf(ErrSubmoduleURL, "%s", subPath)
}
