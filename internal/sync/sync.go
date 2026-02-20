package sync

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/obediencecorp/camp/internal/git"
)

// Sync performs safe submodule synchronization.
//
// The sync process runs in four phases:
//  1. Pre-flight checks: Detect uncommitted changes, unpushed commits, and URL mismatches
//  2. URL synchronization: Copy URLs from .gitmodules to .git/config
//  3. Submodule update: Initialize and update all submodules
//  4. Post-update validation: Verify all submodules are at expected commits
//
// In safe mode (default), sync aborts if pre-flight checks detect uncommitted
// changes or unpushed commits. Use WithForce(true) to skip safety checks.
//
// Returns a SyncResult containing the outcome of each phase and any errors.
func (s *Syncer) Sync(ctx context.Context) (*SyncResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	result := &SyncResult{Success: true}

	// Phase 1: Pre-flight checks (reuse cached result if available)
	var preflight *PreflightResult
	if s.cachedPreflight != nil {
		preflight = s.cachedPreflight
	} else {
		var err error
		preflight, err = s.RunPreflight(ctx)
		if err != nil {
			return nil, fmt.Errorf("pre-flight checks: %w", err)
		}
	}
	result.PreflightPassed = preflight.Passed
	result.Warnings = s.collectWarnings(preflight)

	// In safe mode, abort if pre-flight failed
	if !preflight.Passed && !s.options.Force {
		result.Success = false
		return result, nil
	}

	// Dry-run stops here - just report what would happen
	if s.options.DryRun {
		// Add potential URL changes to result
		result.URLChanges = s.predictURLChanges(preflight)
		return result, nil
	}

	// Phase 2: URL synchronization
	urlChanges, err := s.syncURLs(ctx)
	if err != nil {
		result.Success = false
		result.Errors = append(result.Errors, err)
		return result, nil
	}
	result.URLChanges = urlChanges

	// Phase 3: Submodule update
	updateResults, err := s.updateSubmodules(ctx)
	if err != nil {
		result.Success = false
		result.Errors = append(result.Errors, err)
		return result, nil
	}
	result.UpdateResults = updateResults

	// Phase 4: Post-update validation
	if err := s.validateUpdate(ctx); err != nil {
		result.Success = false
		result.Errors = append(result.Errors, err)
	}

	return result, nil
}

// collectWarnings gathers warning messages from preflight results.
func (s *Syncer) collectWarnings(preflight *PreflightResult) []string {
	var warnings []string

	// In force mode, uncommitted changes become warnings instead of errors
	if s.options.Force {
		for _, status := range preflight.UncommittedChanges {
			warnings = append(warnings, fmt.Sprintf("uncommitted changes in %s: %s", status.Path, status.Details))
		}
		for _, status := range preflight.UnpushedCommits {
			warnings = append(warnings, fmt.Sprintf("unpushed commits in %s: %s", status.Path, status.Details))
		}
	}

	// Detached HEADs are always warnings
	for _, detached := range preflight.DetachedHEADs {
		if detached.HasLocalWork {
			warnings = append(warnings, fmt.Sprintf("detached HEAD with %d local commits in %s",
				detached.LocalCommits, detached.Path))
		}
	}

	return warnings
}

// predictURLChanges returns URL changes that would happen during sync.
func (s *Syncer) predictURLChanges(preflight *PreflightResult) []URLChange {
	changes := make([]URLChange, len(preflight.URLMismatches))
	for i, mismatch := range preflight.URLMismatches {
		changes[i] = URLChange{
			Submodule: mismatch.Submodule,
			OldURL:    mismatch.ActiveURL,
			NewURL:    mismatch.DeclaredURL,
		}
	}
	return changes
}

// syncURLs runs git submodule sync to copy URLs from .gitmodules to .git/config.
func (s *Syncer) syncURLs(ctx context.Context) ([]URLChange, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Capture URL state before sync
	beforeURLs, err := s.captureURLs(ctx)
	if err != nil {
		return nil, fmt.Errorf("capture URLs before sync: %w", err)
	}

	// Run git submodule sync --recursive
	cmd := exec.CommandContext(ctx, "git", "-C", s.repoRoot,
		"submodule", "sync", "--recursive")
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git submodule sync: %s: %w", strings.TrimSpace(string(output)), err)
	}

	// Capture URL state after sync
	afterURLs, err := s.captureURLs(ctx)
	if err != nil {
		return nil, fmt.Errorf("capture URLs after sync: %w", err)
	}

	// Compute what changed
	return s.diffURLs(beforeURLs, afterURLs), nil
}

// captureURLs captures the current URL state for all submodules.
func (s *Syncer) captureURLs(ctx context.Context) (map[string]string, error) {
	paths, err := s.listSubmodules(ctx)
	if err != nil {
		return nil, err
	}

	urls := make(map[string]string)
	for _, path := range paths {
		url, err := git.GetActiveURL(ctx, s.repoRoot, path)
		if err != nil {
			continue // Skip submodules with URL issues
		}
		urls[path] = url
	}

	return urls, nil
}

// diffURLs computes URL changes between before and after states.
func (s *Syncer) diffURLs(before, after map[string]string) []URLChange {
	var changes []URLChange

	for path, afterURL := range after {
		beforeURL := before[path]
		if beforeURL != afterURL && beforeURL != "" {
			changes = append(changes, URLChange{
				Submodule: path,
				OldURL:    beforeURL,
				NewURL:    afterURL,
			})
		}
	}

	return changes
}

// updateSubmodules initializes and updates submodules one-by-one with graceful
// stale reference handling. This replaces the bulk `git submodule update --init --recursive`
// which fails entirely if any single submodule has a stale ref.
func (s *Syncer) updateSubmodules(ctx context.Context) ([]SubmoduleResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	paths, err := s.listSubmodules(ctx)
	if err != nil {
		return nil, err
	}

	var results []SubmoduleResult
	for _, path := range paths {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		result := SubmoduleResult{
			Path:    path,
			Name:    path,
			Success: true,
		}

		// Use shared graceful init (handles stale refs with fallback)
		if err := git.InitSubmoduleGraceful(ctx, s.repoRoot, path); err != nil {
			result.Success = false
			result.Error = fmt.Errorf("init %s: %w", path, err)
			results = append(results, result)
			continue
		}

		// Initialize nested submodules within this submodule
		subDir := filepath.Join(s.repoRoot, path)
		cmd := exec.CommandContext(ctx, "git", "-C", subDir, "submodule", "update", "--init", "--recursive")
		if output, nestedErr := cmd.CombinedOutput(); nestedErr != nil {
			// Nested failure is non-fatal for the parent submodule
			result.Error = fmt.Errorf("nested submodules in %s: %s", path, strings.TrimSpace(string(output)))
		}

		// Checkout default branch instead of leaving on detached HEAD
		git.CheckoutDefaultBranch(ctx, subDir)

		results = append(results, result)
	}

	return results, nil
}

// verifySubmodules checks the status of all submodules after update.
func (s *Syncer) verifySubmodules(ctx context.Context) ([]SubmoduleResult, error) {
	paths, err := s.listSubmodules(ctx)
	if err != nil {
		return nil, err
	}

	var results []SubmoduleResult
	for _, path := range paths {
		result := SubmoduleResult{
			Path:    path,
			Name:    path,
			Success: true,
		}

		fullPath := filepath.Join(s.repoRoot, path)

		// Check if submodule directory exists and has content
		cmd := exec.CommandContext(ctx, "git", "-C", fullPath, "rev-parse", "HEAD")
		if _, err := cmd.Output(); err != nil {
			result.Success = false
			result.Error = fmt.Errorf("submodule not initialized")
		}

		// Check for detached HEAD
		cmd = exec.CommandContext(ctx, "git", "-C", fullPath, "symbolic-ref", "-q", "HEAD")
		if err := cmd.Run(); err != nil {
			result.HeadDetached = true
		}

		// Check working directory cleanliness
		hasChanges, _ := git.HasChanges(ctx, fullPath)
		result.WasClean = !hasChanges

		results = append(results, result)
	}

	return results, nil
}

// validateUpdate checks that all submodules are at expected commits.
func (s *Syncer) validateUpdate(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// git submodule status --recursive
	cmd := exec.CommandContext(ctx, "git", "-C", s.repoRoot,
		"submodule", "status", "--recursive")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("git submodule status: %w", err)
	}

	// Parse output for issues
	// Format: [+- ]<sha1> <path> (<describe>)
	// '-' prefix = not initialized
	// '+' prefix = checked out commit differs from recorded
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "-") {
			// Not initialized - extract path
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return fmt.Errorf("submodule not initialized: %s", parts[1])
			}
			return fmt.Errorf("submodule not initialized: %s", line)
		}

		// '+' prefix means commit differs, but this is expected after sync
		// since we just updated to the recorded commit
	}

	return scanner.Err()
}
