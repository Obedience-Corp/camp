package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
		return camperrors.Wrapf(err, "%s %s", ErrSubmoduleInit.Error(), subPath)
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

	return camperrors.Wrapf(err, "%s %s: %s", ErrSubmoduleUpdate.Error(), subPath, strings.TrimSpace(outputStr))
}

// InitFromDefaultBranch clones a submodule at its remote's default branch
// instead of the recorded (possibly stale) commit. This is used as a fallback when
// the recorded commit no longer exists on the remote.
func InitFromDefaultBranch(ctx context.Context, repoDir, subPath string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if err := pathutil.ValidateSubmodulePath(repoDir, subPath); err != nil {
		return camperrors.Wrapf(err, "%s %s", ErrSubmoduleClone.Error(), subPath)
	}

	// Get submodule URL from .gitmodules
	url, err := getSubmoduleURL(ctx, repoDir, subPath)
	if err != nil {
		return camperrors.Wrapf(err, "%s %s", ErrSubmoduleURL.Error(), subPath)
	}

	subDir := filepath.Join(repoDir, subPath)

	// Remove empty submodule directory if exists
	if err := os.RemoveAll(subDir); err != nil {
		return camperrors.Wrapf(err, "%s %s", ErrSubmoduleRemove.Error(), subPath)
	}

	// Clone directly to submodule path (will use remote's default branch)
	cmd := exec.CommandContext(ctx, "git", "clone", url, subDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return camperrors.Wrapf(err, "%s %s: %s", ErrSubmoduleClone.Error(), subPath, strings.TrimSpace(string(output)))
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
		return "", camperrors.Wrapf(err, "%s %s: %s", ErrBranchCheckout.Error(), branch, strings.TrimSpace(string(output)))
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
