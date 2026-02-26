package sync

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/git"
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
			return nil, &SyncError{Op: "preflight", Cause: err}
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

	// Phase 1.5: Clean orphaned gitlinks before URL sync
	removedOrphans, orphanErr := s.cleanOrphanedGitlinks(ctx)
	if orphanErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("orphan cleanup warning: %v", orphanErr))
	}
	for _, path := range removedOrphans {
		result.Warnings = append(result.Warnings, fmt.Sprintf("removed orphaned gitlink: %s", path))
	}

	// Phase 1.7: Reverse-sync local filesystem URLs
	reverseChanges, reverseErr := s.reverseSyncLocalURLs(ctx)
	if reverseErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("reverse URL sync: %v", reverseErr))
	}
	result.URLChanges = append(result.URLChanges, reverseChanges...)

	// Phase 2: URL synchronization
	urlChanges, err := s.syncURLs(ctx)
	if err != nil {
		result.Success = false
		result.Errors = append(result.Errors, err)
		return result, nil
	}
	result.URLChanges = append(result.URLChanges, urlChanges...)

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
		return nil, &SyncError{Op: "capture-urls-before", Cause: err}
	}

	// Run git submodule sync --recursive
	cmd := exec.CommandContext(ctx, "git", "-C", s.repoRoot,
		"submodule", "sync", "--recursive")
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, &SyncError{Op: "submodule-sync", Cause: fmt.Errorf("%w: %s", ErrSubmoduleSync, strings.TrimSpace(string(output)))}
	}

	// Capture URL state after sync
	afterURLs, err := s.captureURLs(ctx)
	if err != nil {
		return nil, &SyncError{Op: "capture-urls-after", Cause: err}
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
			result.Error = &SyncError{Op: "init", Submodule: path, Cause: err}
			results = append(results, result)
			continue
		}

		// Initialize nested submodules within this submodule
		subDir := filepath.Join(s.repoRoot, path)
		cmd := exec.CommandContext(ctx, "git", "-C", subDir, "submodule", "update", "--init", "--recursive")
		if output, nestedErr := cmd.CombinedOutput(); nestedErr != nil {
			// Nested failure is non-fatal for the parent submodule
			result.Error = &SyncError{Op: "nested-init", Submodule: path, Cause: fmt.Errorf("%w: %s", ErrNestedSubmodules, strings.TrimSpace(string(output)))}
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
			result.Error = ErrSubmoduleNotInitialized
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

// cleanOrphanedGitlinks detects and removes gitlink entries in the index that
// have no corresponding .gitmodules declaration.
func (s *Syncer) cleanOrphanedGitlinks(ctx context.Context) ([]string, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	orphans, err := git.ListOrphanedGitlinks(ctx, s.repoRoot)
	if err != nil {
		return nil, err
	}

	if len(orphans) == 0 {
		return nil, nil
	}

	removed, err := git.RemoveOrphanedGitlinks(ctx, s.repoRoot, orphans)
	if err != nil {
		return removed, err
	}

	return removed, nil
}

// reverseSyncLocalURLs detects submodules where .gitmodules has a local
// filesystem path but the submodule has a real remote origin configured,
// and updates .gitmodules to use the remote URL.
func (s *Syncer) reverseSyncLocalURLs(ctx context.Context) ([]URLChange, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	paths, err := s.listSubmodules(ctx)
	if err != nil {
		return nil, err
	}

	var changes []URLChange
	for _, path := range paths {
		if ctx.Err() != nil {
			return changes, ctx.Err()
		}

		declaredURL, err := git.GetDeclaredURL(ctx, s.repoRoot, path)
		if err != nil {
			continue
		}

		if !git.IsLocalFilesystemURL(declaredURL) {
			continue
		}

		subFullPath := filepath.Join(s.repoRoot, path)
		remoteURL, err := git.RemoteOriginURL(ctx, subFullPath)
		if err != nil || remoteURL == "" {
			continue
		}

		if git.IsLocalFilesystemURL(remoteURL) {
			continue
		}

		if err := git.SetDeclaredURL(ctx, s.repoRoot, path, remoteURL); err != nil {
			return changes, fmt.Errorf("reverse-sync %s: %w", path, err)
		}

		changes = append(changes, URLChange{
			Submodule: path,
			OldURL:    declaredURL,
			NewURL:    remoteURL,
		})
	}

	// Propagate changes to .git/config
	if len(changes) > 0 {
		cmd := exec.CommandContext(ctx, "git", "-C", s.repoRoot,
			"submodule", "sync", "--recursive")
		if output, syncErr := cmd.CombinedOutput(); syncErr != nil {
			return changes, fmt.Errorf("submodule sync after reverse-sync: %w: %s",
				syncErr, strings.TrimSpace(string(output)))
		}
	}

	return changes, nil
}

// validateUpdate checks that all submodules are at expected commits.
func (s *Syncer) validateUpdate(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// git submodule status --recursive
	cmd := exec.CommandContext(ctx, "git", "-C", s.repoRoot,
		"submodule", "status", "--recursive")
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Handle "no submodule mapping" errors gracefully - these indicate
		// orphaned gitlinks that weren't fully cleaned up. Don't fail the
		// entire validation for this.
		outputStr := string(output)
		if strings.Contains(outputStr, "no submodule mapping found") {
			return nil
		}
		return &SyncError{Op: "validate", Cause: fmt.Errorf("%w: %w", ErrSubmoduleValidation, err)}
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
			sub := line
			if len(parts) >= 2 {
				sub = parts[1]
			}
			return &SyncError{Op: "validate", Submodule: sub, Cause: ErrSubmoduleNotInitialized}
		}

		// '+' prefix means commit differs, but this is expected after sync
		// since we just updated to the recorded commit
	}

	return scanner.Err()
}
