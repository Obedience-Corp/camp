package sync

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	gosync "sync"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
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
	for _, sub := range updateResults {
		if !sub.Success {
			result.Success = false
			if sub.Error != nil {
				result.Errors = append(result.Errors, sub.Error)
			}
		}
		if sub.DriftWarning != "" {
			result.Success = false
			result.Warnings = append(result.Warnings, sub.DriftWarning)
		}
		if sub.PeerWarning != "" {
			result.Warnings = append(result.Warnings, sub.PeerWarning)
		}
	}

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
		return nil, &SyncError{Op: "submodule-sync", Cause: camperrors.Wrapf(git.ErrSubmoduleSync, "%s", strings.TrimSpace(string(output)))}
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

// updateSubmodules initializes and updates submodules individually with graceful
// stale reference handling. This replaces the bulk `git submodule update --init --recursive`
// which fails entirely if any single submodule has a stale ref. Submodules are
// processed concurrently under a semaphore bounded by SyncOptions.Parallel,
// mirroring the worker pattern clone uses for submodule initialization; results
// preserve .gitmodules declaration order.
//
// Concurrency tradeoff (shared with clone's parallel init): each worker runs
// superproject git commands, which git guards with repo lockfiles that fail
// fast on contention rather than queueing. Under high parallelism on slow
// disks that can surface transient "Unable to create ...lock" failures the
// serial path never hit; the mitigation is lowering --parallel for that
// campaign.
func (s *Syncer) updateSubmodules(ctx context.Context) ([]SubmoduleResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	paths, err := s.listSubmodules(ctx)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, nil
	}

	parallel := s.options.Parallel
	if parallel <= 0 {
		parallel = 4
	}
	if parallel > len(paths) {
		parallel = len(paths)
	}

	results := make([]SubmoduleResult, len(paths))
	sem := make(chan struct{}, parallel)
	var wg gosync.WaitGroup

	for i, path := range paths {
		if ctx.Err() != nil {
			break
		}

		wg.Add(1)
		go func(idx int, path string) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[idx] = SubmoduleResult{
					Path:    path,
					Name:    path,
					Success: false,
					Error:   &SyncError{Op: "update", Submodule: path, Cause: ctx.Err()},
				}
				return
			}

			results[idx] = s.updateSubmodule(ctx, path)
		}(i, path)
	}
	wg.Wait()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return results, nil
}

// updateSubmodule runs the full update pipeline for a single submodule:
// graceful init, nested submodule init, default-branch checkout, and
// gitlink drift reconciliation.
func (s *Syncer) updateSubmodule(ctx context.Context, path string) SubmoduleResult {
	result := SubmoduleResult{
		Path:    path,
		Name:    path,
		Success: true,
	}

	// --no-fetch promises local refs only, so it suppresses the peer network
	// fetch too (a peer fetch is still a network transfer).
	if s.peer != nil && !s.options.NoFetch {
		fetched, peerErr := s.peerFetch(ctx, path)
		result.PeerFetched = fetched
		if peerErr != nil {
			result.PeerWarning = fmt.Sprintf("%s: peer fetch from %s failed, continuing via origin: %v",
				path, s.peer.ID(), peerErr)
		}
	}

	// Use shared graceful init (handles stale refs with fallback)
	if err := git.InitSubmoduleGraceful(ctx, s.repoRoot, path); err != nil {
		result.Success = false
		result.Error = &SyncError{Op: "init", Submodule: path, Cause: err}
		return result
	}

	// Initialize nested submodules within this submodule
	subDir := filepath.Join(s.repoRoot, path)
	cmd := exec.CommandContext(ctx, "git", "-C", subDir, "submodule", "update", "--init", "--recursive")
	if output, nestedErr := cmd.CombinedOutput(); nestedErr != nil {
		// Nested failure is non-fatal for the parent submodule
		result.Error = &SyncError{Op: "nested-init", Submodule: path, Cause: camperrors.Wrapf(ErrNestedSubmodules, "%s", strings.TrimSpace(string(output)))}
	}

	// Checkout default branch instead of leaving on detached HEAD.
	branch, checkoutErr := git.CheckoutDefaultBranch(ctx, subDir)
	if checkoutErr != nil {
		result.Success = false
		result.Error = &SyncError{Op: "checkout", Submodule: path, Cause: checkoutErr}
		return result
	}
	result.CheckedOutBranch = branch
	if warning, driftErr := s.reconcileCheckoutDrift(ctx, path, subDir, branch); driftErr != nil {
		result.Success = false
		result.Error = &SyncError{Op: "drift", Submodule: path, Cause: driftErr}
	} else if warning != "" {
		result.DriftWarning = warning
	}

	return result
}

// peerFetch pulls objects for one submodule from the configured peer ahead of
// the origin-based update. It returns (false, nil) for a submodule that is not
// initialized locally: there is no repository to fetch into yet, and the
// normal init path handles it from origin. A fetch failure is returned for
// the caller to surface as a warning; sync then proceeds via origin.
func (s *Syncer) peerFetch(ctx context.Context, path string) (bool, error) {
	subDir := filepath.Join(s.repoRoot, path)
	if _, err := os.Stat(filepath.Join(subDir, ".git")); err != nil {
		return false, nil
	}
	if err := s.peer.Fetch(ctx, subDir, path); err != nil {
		return false, err
	}
	return true, nil
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
			result.Error = git.ErrSubmoduleNotInitialized
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
			return changes, camperrors.Wrapf(err, "reverse-sync %s", path)
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
			return changes, camperrors.Wrapf(syncErr, "submodule sync after reverse-sync: %s",
				strings.TrimSpace(string(output)))
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
		return &SyncError{Op: "validate", Cause: camperrors.Wrap(err, ErrSubmoduleValidation.Error())}
	}

	return validateSubmoduleStatusOutput(string(output))
}

func validateSubmoduleStatusOutput(output string) error {
	// Parse output for issues.
	// Format: [+- ]<sha1> <path> (<describe>)
	// '-' prefix = not initialized
	// '+' prefix = checked out commit differs from the recorded gitlink
	scanner := bufio.NewScanner(strings.NewReader(output))
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
			return &SyncError{Op: "validate", Submodule: sub, Cause: git.ErrSubmoduleNotInitialized}
		}

		if strings.HasPrefix(line, "+") {
			parts := strings.Fields(line)
			sub := line
			recordedSHA := strings.TrimPrefix(line, "+")
			if len(parts) >= 2 {
				sub = parts[1]
				recordedSHA = strings.TrimPrefix(parts[0], "+")
			}
			return &SyncError{
				Op:        "validate-drift",
				Submodule: sub,
				Cause: camperrors.Wrapf(
					ErrSubmoduleValidation,
					"checked-out commit differs from recorded gitlink %s; run 'camp sync' or inspect the submodule",
					recordedSHA,
				),
			}
		}
	}

	return scanner.Err()
}

func (s *Syncer) reconcileCheckoutDrift(ctx context.Context, path, subDir, branch string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if strings.TrimSpace(branch) == "" || branch == "HEAD" {
		return "", nil
	}

	recordedSHA, err := s.recordedGitlinkSHA(ctx, path)
	if err != nil {
		return "", err
	}

	branchSHA, err := git.Output(ctx, subDir, "rev-parse", "--verify", branch+"^{commit}")
	if err != nil {
		return "", err
	}

	recordedSHA = strings.TrimSpace(recordedSHA)
	branchSHA = strings.TrimSpace(branchSHA)
	if branchSHA == recordedSHA {
		return "", nil
	}

	canFastForward, err := git.IsAncestor(ctx, subDir, branchSHA, recordedSHA)
	if err != nil {
		return fmt.Sprintf("%s: local %s tip %s != recorded %s (could not verify fast-forward: %v)",
			path, branch, shortSHA(branchSHA), shortSHA(recordedSHA), err), nil
	}
	if !canFastForward {
		return fmt.Sprintf("%s: local %s tip %s != recorded %s (fast-forward not possible)",
			path, branch, shortSHA(branchSHA), shortSHA(recordedSHA)), nil
	}

	cmd := exec.CommandContext(ctx, "git", "-C", subDir, "merge", "--ff-only", recordedSHA)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Sprintf("%s: local %s tip %s != recorded %s (fast-forward failed: %s)",
			path, branch, shortSHA(branchSHA), shortSHA(recordedSHA), strings.TrimSpace(string(output))), nil
	}

	return fmt.Sprintf("%s: local %s tip %s != recorded %s (fast-forwarded)",
		path, branch, shortSHA(branchSHA), shortSHA(recordedSHA)), nil
}

func (s *Syncer) recordedGitlinkSHA(ctx context.Context, path string) (string, error) {
	output, err := git.Output(ctx, s.repoRoot, "ls-files", "-s", "--", path)
	if err != nil {
		return "", err
	}
	fields := strings.Fields(output)
	if len(fields) < 2 || fields[0] != "160000" {
		return "", camperrors.Wrapf(ErrSubmoduleValidation, "no gitlink entry for %s", path)
	}
	return fields[1], nil
}

func shortSHA(sha string) string {
	sha = strings.TrimSpace(sha)
	if len(sha) <= 8 {
		return sha
	}
	return sha[:8]
}
