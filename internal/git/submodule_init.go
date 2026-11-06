package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
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

func submoduleModuleGitDir(ctx context.Context, repoDir, subPath string) (string, error) {
	modulePath := filepath.ToSlash(filepath.Join("modules", filepath.FromSlash(subPath)))
	cmd := exec.CommandContext(ctx, "git", "-C", repoDir,
		"rev-parse", "--path-format=absolute", "--git-path", modulePath)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func isGitDir(path string) bool {
	if path == "" {
		return false
	}
	if info, err := os.Stat(filepath.Join(path, "config")); err == nil && !info.IsDir() {
		return true
	}
	return false
}

func quarantineSuffix(quarantine string) string {
	if quarantine == "" {
		return ""
	}
	return fmt.Sprintf(" (quarantine available at %s)", quarantine)
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
	return resolveRelativeSubmoduleURL(remoteURL, url)
}

// Git treats the first ../ as the move from a repository to its sibling.
// Remaining parent segments are normalized inside the remote's path, leaving
// scheme and scp-style host prefixes intact.
func resolveRelativeSubmoduleURL(remoteURL, relativeURL string) string {
	base := strings.TrimSuffix(remoteURL, "/")
	prefix, remotePath := "", base
	if scheme := strings.Index(base, "://"); scheme >= 0 {
		pathStart := strings.IndexByte(base[scheme+3:], '/')
		if pathStart < 0 {
			return relativeURL
		}
		pathStart += scheme + 3
		prefix, remotePath = base[:pathStart], base[pathStart:]
	} else if colon := strings.IndexByte(base, ':'); colon >= 0 && !strings.HasPrefix(base, "/") {
		prefix, remotePath = base[:colon+1], base[colon+1:]
	}
	slash := strings.LastIndexByte(remotePath, '/')
	if slash < 0 {
		return relativeURL
	}
	relativeURL = strings.TrimPrefix(relativeURL, "../")
	return prefix + path.Clean(remotePath[:slash]+"/"+relativeURL)
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

// DetectDefaultBranchWithParent selects an explicit .gitmodules branch when
// present, then falls back to the remote's default branch.
func DetectDefaultBranchWithParent(ctx context.Context, repoDir, subPath, subDir string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	// An explicit branch is a checkout policy, even when origin/HEAD names a
	// different branch. Git's special "." value inherits the parent's branch.
	name, nameErr := SubmoduleNameForPath(ctx, repoDir, subPath)
	if nameErr != nil {
		return "", nameErr
	}
	cmd := exec.CommandContext(ctx, "git", "-C", repoDir,
		"config", "-f", ".gitmodules", fmt.Sprintf("submodule.%s.branch", name))
	output, err := cmd.Output()
	if err == nil {
		branch := strings.TrimSpace(string(output))
		if branch == "." {
			parentBranch, parentErr := Output(ctx, repoDir, "symbolic-ref", "--quiet", "--short", "HEAD")
			if parentErr == nil && strings.TrimSpace(parentBranch) != "" {
				return strings.TrimSpace(parentBranch), nil
			}
		}
		if branch != "" && branch != "." {
			return branch, nil
		}
	}

	// Check symbolic-ref (populated after clone/fetch, no network).
	cmd = exec.CommandContext(ctx, "git", "-C", subDir, "symbolic-ref", "refs/remotes/origin/HEAD")
	output, err = cmd.Output()
	if err == nil {
		ref := strings.TrimSpace(string(output))
		branch := strings.TrimPrefix(ref, "refs/remotes/origin/")
		if branch != ref && branch != "" {
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

// CheckoutSubmoduleBranch selects the owner's declared branch, or the remote
// default. Sync can preserve an already selected local branch; clone selects
// the declared branch after its fresh initialization.
func CheckoutSubmoduleBranch(ctx context.Context, repoDir, subPath string, preserveCurrent bool) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	subDir := filepath.Join(repoDir, subPath)
	if preserveCurrent {
		current, err := Output(ctx, subDir, "symbolic-ref", "--quiet", "--short", "HEAD")
		if err == nil && strings.TrimSpace(current) != "" {
			return strings.TrimSpace(current), nil
		}
	}
	branch, err := DetectDefaultBranchWithParent(ctx, repoDir, subPath, subDir)
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, "git", "-C", subDir, "checkout", branch)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", camperrors.WrapJoinf(ErrBranchCheckout, err, "%s in %s: %s", branch, subPath, strings.TrimSpace(string(output)))
	}
	return branch, nil
}

// InitSubmoduleGraceful initializes a single submodule, handling stale commit references.
// If the recorded commit doesn't exist on the remote, it falls back to cloning at the
// remote's default branch instead.
func InitSubmoduleGraceful(ctx context.Context, repoDir, subPath string) error {
	_, err := InitSubmoduleGracefulResult(ctx, repoDir, subPath)
	return err
}

// InitSubmoduleResult records whether the recorded commit was unavailable.
type InitSubmoduleResult struct {
	StaleRefFallback bool
}

// InitSubmoduleGracefulResult has the same safe initialization behavior and
// reports whether recovery from an unavailable gitlink was necessary.
func InitSubmoduleGracefulResult(ctx context.Context, repoDir, subPath string) (InitSubmoduleResult, error) {
	if ctx.Err() != nil {
		return InitSubmoduleResult{}, ctx.Err()
	}

	if err := pathutil.ValidateSubmodulePath(repoDir, subPath); err != nil {
		return InitSubmoduleResult{}, camperrors.WrapJoinf(ErrSubmoduleInit, err, "%s", subPath)
	}

	// Register and update in one Git operation. A failed registration must not
	// turn the following update into a successful no-op on an empty child.
	cmd := exec.CommandContext(ctx, "git", "-C", repoDir, "submodule", "update", "--init", subPath)
	output, err := cmd.CombinedOutput()

	if err == nil {
		initialized, identityErr := IsWorktreeRoot(ctx, filepath.Join(repoDir, subPath))
		if identityErr != nil {
			return InitSubmoduleResult{}, camperrors.WrapJoinf(ErrSubmoduleInit, identityErr, "%s: verify repository identity", subPath)
		}
		if !initialized {
			return InitSubmoduleResult{}, camperrors.WrapJoinf(ErrSubmoduleInit, ErrSubmoduleNotInitialized, "%s: update returned without initializing a repository", subPath)
		}
		return InitSubmoduleResult{}, nil
	}

	outputStr := string(output)

	// Step 3: Check if error is stale reference
	if strings.Contains(outputStr, "not our ref") ||
		strings.Contains(outputStr, "did not contain") ||
		strings.Contains(outputStr, "reference is not a tree") {
		if fallbackErr := InitFromDefaultBranch(ctx, repoDir, subPath); fallbackErr != nil {
			return InitSubmoduleResult{}, fallbackErr
		}
		return InitSubmoduleResult{StaleRefFallback: true}, nil
	}

	return InitSubmoduleResult{}, camperrors.WrapJoinf(ErrSubmoduleUpdate, err, "%s: %s", subPath, strings.TrimSpace(outputStr))
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
	quarantine := ""

	empty, err := isDirEmpty(subDir)
	if err != nil {
		return camperrors.WrapJoinf(ErrSubmoduleRemove, err, "checking %s", subPath)
	}

	if !empty {
		// Quarantine-rename instead of RemoveAll so uncommitted content survives.
		ts := time.Now().UTC().Format("20060102-150405")
		quarantine = subDir + ".sync-quarantine-" + ts
		if renErr := os.Rename(subDir, quarantine); renErr != nil {
			return camperrors.WrapJoinf(ErrSubmoduleRemove, renErr,
				"%s: non-empty dir cannot be quarantined (inspect %s)", subPath, quarantine)
		}
	} else {
		if err := os.RemoveAll(subDir); err != nil {
			return camperrors.WrapJoinf(ErrSubmoduleRemove, err, "%s", subPath)
		}
	}

	moduleGitDir, moduleErr := submoduleModuleGitDir(ctx, repoDir, subPath)
	if moduleErr != nil {
		return camperrors.WrapJoinf(ErrSubmoduleUpdate, moduleErr,
			"%s: resolving submodule gitdir%s", subPath, quarantineSuffix(quarantine))
	}

	var fetchFailure string
	if isGitDir(moduleGitDir) {
		// Fetch into .git/modules/<subPath>, not the superproject, so the
		// fallback preserves submodule wiring and cannot pollute root remotes.
		fetchCmd := exec.CommandContext(ctx, "git",
			"--git-dir", moduleGitDir,
			"fetch", "--recurse-submodules=no", url,
			"+refs/heads/*:refs/remotes/origin/*")
		if output, fetchErr := fetchCmd.CombinedOutput(); fetchErr != nil {
			fetchFailure = fmt.Sprintf("fetch into %s failed: %s", moduleGitDir, strings.TrimSpace(string(output)))
		}
	}

	updateCmd := exec.CommandContext(ctx, "git", "-C", repoDir,
		"submodule", "update", "--init", subPath)
	if output, updateErr := updateCmd.CombinedOutput(); updateErr != nil {
		updateOutput := strings.TrimSpace(string(output))
		if !IsStaleRefError(camperrors.Wrapf(updateErr, "%s", updateOutput)) {
			return camperrors.WrapJoinf(ErrSubmoduleUpdate, updateErr,
				"%s: %s%s", subPath, updateOutput, quarantineSuffix(quarantine))
		}

		remoteCmd := exec.CommandContext(ctx, "git", "-C", repoDir,
			"submodule", "update", "--init", "--remote", "--checkout", subPath)
		if remoteOutput, remoteErr := remoteCmd.CombinedOutput(); remoteErr != nil {
			detail := updateOutput
			if fetchFailure != "" {
				detail = fetchFailure + "; " + detail
			}
			return camperrors.WrapJoinf(ErrSubmoduleUpdate, remoteErr,
				"%s: recorded commit unavailable (%s); remote fallback failed: %s%s",
				subPath, detail, strings.TrimSpace(string(remoteOutput)), quarantineSuffix(quarantine))
		}
		return nil
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
	name, nameErr := SubmoduleNameForPath(ctx, repoDir, subPath)
	if nameErr != nil {
		return "", nameErr
	}
	// Try .gitmodules first
	cmd := exec.CommandContext(ctx, "git", "-C", repoDir,
		"config", "-f", ".gitmodules", fmt.Sprintf("submodule.%s.url", name))
	output, err := cmd.Output()
	if err == nil {
		return strings.TrimSpace(string(output)), nil
	}

	// Fall back to .git/config
	cmd = exec.CommandContext(ctx, "git", "-C", repoDir,
		"config", fmt.Sprintf("submodule.%s.url", name))
	output, err = cmd.Output()
	if err == nil {
		return strings.TrimSpace(string(output)), nil
	}

	return "", camperrors.Wrapf(ErrSubmoduleURL, "%s", subPath)
}
