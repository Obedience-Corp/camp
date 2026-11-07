package sync

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
	gosync "sync"

	"github.com/Obedience-Corp/camp/internal/artifacts"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/pathutil"
	"github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/remote"
	"github.com/Obedience-Corp/camp/internal/rsyncprobe"
)

// Sync performs safe submodule synchronization.
//
// The sync process runs in four phases:
//  1. Pre-flight checks: Detect uncommitted changes, unpushed commits, and URL mismatches
//  2. URL synchronization: Copy URLs from .gitmodules to .git/config
//  3. Submodule update: Initialize and update all submodules
//  4. Post-update validation: Verify owner metadata, selected commits, and active URLs
//
// In safe mode (default), sync aborts if pre-flight checks detect uncommitted
// changes or unpushed commits. Use WithForce(true) to skip safety checks.
//
// Returns a SyncResult containing the outcome of each phase and any errors.
func (s *Syncer) Sync(ctx context.Context) (*SyncResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if s.peer != nil && !s.options.VerifyArtifacts {
		if err := remote.RequirePeerSupport(); err != nil {
			return nil, err
		}
	}

	result := &SyncResult{Success: true}
	if links, linkErr := project.ListBrokenLinks(s.repoRoot); linkErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("optional project link inventory: %v", linkErr))
	} else {
		result.OptionalLinks = links
	}

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
	result.Warnings = append(result.Warnings, s.collectWarnings(preflight)...)

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

	// Artifact-only variants keep the preflight above (every sync variant
	// runs it) but skip the git phases entirely.
	if s.options.VerifyArtifacts {
		s.verifyArtifacts(ctx, result)
		return result, nil
	}
	if s.options.ArtifactsOnly {
		s.pullArtifacts(ctx, result)
		return result, nil
	}

	// Phase 1.5: Report orphaned gitlinks without changing the index.
	orphans, orphanErr := s.reportOrphanedGitlinks(ctx)
	if orphanErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("orphan inventory warning: %v", orphanErr))
	}
	for _, orphan := range orphans {
		result.Warnings = append(result.Warnings, fmt.Sprintf("orphaned gitlink without .gitmodules declaration: %s", orphan.Path))
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
		result.OperationFailed = true
		result.Errors = append(result.Errors, err)
		return result, nil
	}
	result.URLChanges = append(result.URLChanges, urlChanges...)

	// Phase 3: Submodule update
	updateResults, err := s.updateSubmodules(ctx)
	if err != nil {
		result.Success = false
		result.OperationFailed = true
		result.Errors = append(result.Errors, err)
		return result, nil
	}
	result.UpdateResults = updateResults
	for _, sub := range updateResults {
		if !sub.Success {
			result.Success = false
			result.OperationFailed = true
			if sub.Error != nil {
				result.Errors = append(result.Errors, sub.Error)
			}
		}
		if sub.DriftWarning != "" {
			result.Success = false
			result.OperationFailed = true
			result.Warnings = append(result.Warnings, sub.DriftWarning)
		}
		if sub.PeerWarning != "" {
			result.Warnings = append(result.Warnings, sub.PeerWarning)
		}
	}
	if ctx.Err() != nil {
		result.Success = false
		result.OperationFailed = true
		if len(result.Errors) == 0 {
			result.Errors = append(result.Errors, ctx.Err())
		}
		return result, nil
	}

	// Phase 4: Post-update validation
	validationWarnings, validationErrors := s.validateInventory(ctx)
	result.ValidationRan = true
	for _, warning := range validationWarnings {
		found := false
		for _, existing := range result.Warnings {
			if warning == existing {
				found = true
				break
			}
		}
		if !found {
			result.Warnings = append(result.Warnings, warning)
		}
	}
	if len(validationErrors) > 0 {
		result.Success = false
		result.ValidationFailed = true
		result.Errors = append(result.Errors, validationErrors...)
	}
	if ctx.Err() != nil {
		result.OperationFailed = true
	}

	// Phase 5: Artifact pull rides the peer connection when one is
	// configured; policy=always roots only, and failures degrade to
	// warnings (the accelerated sync must not be worse than a plain one).
	if s.peer != nil && !s.options.GitOnly {
		beforeErrors := len(result.Errors)
		s.pullArtifacts(ctx, result)
		if len(result.Errors) > beforeErrors {
			result.OperationFailed = true
		}
	}

	return result, nil
}

// pullArtifacts transfers declared artifact roots from the configured peer.
// Default syncs move policy=always roots; an explicit --artifacts-only run
// moves every declared root and treats per-root failures as sync failures
// (the artifacts were the whole ask).
func (s *Syncer) pullArtifacts(ctx context.Context, result *SyncResult) {
	if s.peer == nil {
		result.Success = false
		result.Errors = append(result.Errors, &SyncError{Op: "artifacts",
			Cause: camperrors.New("artifact sync needs a peer (--from <machine>)")})
		return
	}
	prober := rsyncprobe.NewProber(s.options.NoProbeCache)
	cfg, err := artifacts.Load(s.repoRoot)
	if err != nil {
		// A default `--from` sync degrades artifact failures to warnings (the
		// accelerated path must never be worse than a plain sync), but under
		// --artifacts-only the artifacts were the entire ask, so an unreadable
		// config is a hard failure rather than a silent exit 0.
		result.Warnings = append(result.Warnings, fmt.Sprintf("artifacts config: %v", err))
		if s.options.ArtifactsOnly {
			result.Success = false
			result.Errors = append(result.Errors, &SyncError{Op: "artifacts", Cause: err})
		}
		return
	}
	// A committed declaration is not trusted just because it parsed: an
	// unsupported version, unknown policy, or duplicate root must fail closed
	// here rather than be silently skipped on a normal sync yet pulled under
	// --artifacts-only. Same degrade contract as an unreadable config.
	if verr := cfg.Validate(); verr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("artifacts config: %v", verr))
		if s.options.ArtifactsOnly {
			result.Success = false
			result.Errors = append(result.Errors, &SyncError{Op: "artifacts", Cause: verr})
		}
		return
	}
	for _, root := range cfg.Roots {
		if ctx.Err() != nil {
			result.Errors = append(result.Errors, ctx.Err())
			result.Success = false
			return
		}
		if !s.options.ArtifactsOnly && root.EffectivePolicy() != artifacts.PolicyAlways {
			continue
		}
		pr := artifacts.PullWithProber(ctx, s.repoRoot, s.peer, root, prober)
		result.Artifacts = append(result.Artifacts, pr)
		// The top-of-loop guard cannot catch a cancellation that lands during
		// this root's transfer (the last root has no next iteration), so
		// re-check here: a cancelled sync must fail regardless of mode rather
		// than reporting the interrupted transfer as a warning and exiting 0.
		if ctx.Err() != nil {
			result.Success = false
			result.Errors = append(result.Errors, ctx.Err())
			return
		}
		if pr.Warning != "" {
			result.Warnings = append(result.Warnings, fmt.Sprintf("artifacts %s: %s", pr.Root, pr.Warning))
			if s.options.ArtifactsOnly {
				result.Success = false
			}
		}
	}
}

// verifyArtifacts compares each declared root against last-transfer
// snapshots: no transfer, no git phases. Discrepancies fail the run so
// scripting can rely on the exit code.
func (s *Syncer) verifyArtifacts(ctx context.Context, result *SyncResult) {
	// VerifyPeer comes straight from --from and is joined into the snapshot
	// path (.campaign/cache/peersync/<peer>/...); validate it before it can
	// traverse on read (e.g. --verify-artifacts --from ../../..).
	if s.options.VerifyPeer != "" {
		if err := artifacts.ValidatePeerID(s.options.VerifyPeer); err != nil {
			result.Success = false
			result.Errors = append(result.Errors, &SyncError{Op: "artifacts-verify", Cause: err})
			return
		}
	}

	cfg, err := artifacts.Load(s.repoRoot)
	if err != nil {
		result.Success = false
		result.Errors = append(result.Errors, &SyncError{Op: "artifacts-verify", Cause: err})
		return
	}
	if err := cfg.Validate(); err != nil {
		result.Success = false
		result.Errors = append(result.Errors, &SyncError{Op: "artifacts-verify", Cause: err})
		return
	}

	peers := []string{s.options.VerifyPeer}
	if s.options.VerifyPeer == "" {
		peers, err = artifacts.ListSnapshotPeers(s.repoRoot)
		if err != nil {
			result.Success = false
			result.Errors = append(result.Errors, &SyncError{Op: "artifacts-verify", Cause: err})
			return
		}
		if len(peers) == 0 {
			result.Warnings = append(result.Warnings,
				"no peer snapshots recorded yet; nothing to verify against (run 'camp sync --from <machine>' first)")
			return
		}
	}

	matched := 0
	for _, root := range cfg.Roots {
		if ctx.Err() != nil {
			result.Errors = append(result.Errors, ctx.Err())
			result.Success = false
			return
		}
		rootRel, err := artifacts.EnsureRootWithin(s.repoRoot, root.Path)
		if err != nil {
			result.Success = false
			result.Errors = append(result.Errors, &SyncError{Op: "artifacts-verify", Submodule: root.Path, Cause: err})
			continue
		}
		local, err := artifacts.BuildManifest(ctx, s.repoRoot, rootRel)
		if err != nil {
			result.Success = false
			result.Errors = append(result.Errors, &SyncError{Op: "artifacts-verify", Submodule: rootRel, Cause: err})
			continue
		}
		for _, peerID := range peers {
			snapshot, err := artifacts.LoadSnapshot(s.repoRoot, peerID, rootRel)
			if err != nil {
				result.Success = false
				result.Errors = append(result.Errors, &SyncError{Op: "artifacts-verify", Submodule: rootRel, Cause: err})
				continue
			}
			if snapshot == nil {
				continue
			}
			matched++
			verify := artifacts.Verify(local, snapshot)
			result.ArtifactVerifies = append(result.ArtifactVerifies, ArtifactVerify{Peer: peerID, Result: verify})
			if !verify.Clean() {
				result.Success = false
			}
		}
	}

	// A --from-scoped verify that matched no snapshot for any root is almost
	// always a typo'd or never-synced peer id: report it rather than exiting
	// 0 "complete" having checked nothing.
	if s.options.VerifyPeer != "" && matched == 0 && len(cfg.Roots) > 0 {
		result.Success = false
		result.Errors = append(result.Errors, &SyncError{Op: "artifacts-verify",
			Cause: camperrors.Newf("no snapshots recorded for peer %q; check the machine id or run 'camp sync --from %s' first",
				s.options.VerifyPeer, s.options.VerifyPeer)})
	}
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
					Skipped: true,
					Error:   &SyncError{Op: "skip", Submodule: path, Cause: ctx.Err()},
				}
				return
			}

			release, lockErr := git.LockSubmoduleOwner(ctx, s.repoRoot)
			if lockErr != nil {
				skipped := errors.Is(lockErr, context.Canceled) || errors.Is(lockErr, context.DeadlineExceeded)
				op := "init"
				if skipped {
					op = "skip"
				}
				results[idx] = SubmoduleResult{
					Path: path, Name: path, Success: false, Skipped: skipped,
					Error: &SyncError{Op: op, Submodule: path, Cause: lockErr},
				}
				return
			}
			defer release()
			results[idx] = s.updateSubmodule(ctx, path)
		}(i, path)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for i, path := range paths {
		if results[i].Path == "" {
			results[i] = SubmoduleResult{Path: path, Name: path, Success: false, Skipped: true,
				Error: &SyncError{Op: "skip", Submodule: path, Cause: ctx.Err()}}
		}
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

	subDir := filepath.Join(s.repoRoot, path)
	initialized, identityErr := git.IsWorktreeRoot(ctx, subDir)
	if identityErr != nil {
		result.Success = false
		result.Error = &SyncError{Op: "identify", Submodule: path, Cause: identityErr}
		return result
	}
	wasInitialized := initialized
	// An initialized local branch belongs to the user. Updating it through
	// the gitlink would detach HEAD before the checkout policy can inspect it.
	if !initialized {
		outcome, err := git.InitSubmoduleGracefulResult(ctx, s.repoRoot, path)
		result.StaleRefFallback = outcome.StaleRefFallback
		if err != nil {
			result.Success = false
			result.Error = &SyncError{Op: "init", Submodule: path, Cause: err}
			return result
		}
	}
	initialized, identityErr = git.IsWorktreeRoot(ctx, subDir)
	if identityErr != nil {
		result.Success = false
		result.Error = &SyncError{Op: "identify", Submodule: path, Cause: identityErr}
		return result
	}
	if !initialized {
		result.Success = false
		result.Error = &SyncError{Op: "identify", Submodule: path, Cause: git.ErrSubmoduleNotInitialized}
		return result
	}
	result.Initialized = true
	if wasInitialized {
		if err := s.ensureRecordedCommitAvailable(ctx, s.repoRoot, subDir, path); err != nil {
			result.Success = false
			result.Error = &SyncError{Op: "fetch", Submodule: path, Cause: err}
			return result
		}
	}
	// Checkout default branch instead of leaving on detached HEAD.
	branch, checkoutErr := git.CheckoutSubmoduleBranch(ctx, s.repoRoot, path, true)
	if checkoutErr != nil {
		result.Success = false
		result.Error = &SyncError{Op: "checkout", Submodule: path, Cause: checkoutErr}
		return result
	}
	result.CheckedOutBranch = branch
	result.RecordedCommit, _ = s.recordedGitlinkSHA(ctx, path)
	if warning, driftErr := s.reconcileCheckoutDrift(ctx, path, subDir, branch); driftErr != nil {
		result.Success = false
		result.Error = &SyncError{Op: "drift", Submodule: path, Cause: driftErr}
		return result
	} else if warning != "" {
		result.DriftWarning = warning
	}
	result.Commit, _ = git.Output(ctx, subDir, "rev-parse", "HEAD")

	// Each child must select its branch before its own children are listed.
	result.Nested = s.initializeNestedSelected(ctx, subDir, path, map[string]bool{})
	if nestedWarnings := git.NestedWarnings(result.Nested); len(nestedWarnings) > 0 {
		if result.DriftWarning != "" {
			result.DriftWarning += "\n"
		}
		result.DriftWarning += strings.Join(nestedWarnings, "\n")
	}
	if nestedErr := git.NestedFailures(result.Nested); nestedErr != nil {
		result.Success = false
		result.Error = nestedErr
	}
	return result
}

// initializeNestedSelected walks declarations in each selected checkout. It
// never removes a child that disappeared from a newer parent revision.
func (s *Syncer) initializeNestedSelected(ctx context.Context, ownerDir, ownerPath string, visited map[string]bool) []git.NestedResult {
	if ctx.Err() != nil {
		return []git.NestedResult{{Owner: ownerPath, Path: ownerPath, Skipped: true,
			Operation: "list", Error: &git.NestedError{Owner: ownerPath, Path: ownerPath, Op: "list", Cause: ctx.Err()}}}
	}
	actualOwner, err := filepath.EvalSymlinks(ownerDir)
	if err != nil {
		return []git.NestedResult{{Owner: ownerPath, Path: ownerPath, Attempted: true,
			Operation: "identify", Error: &git.NestedError{Owner: ownerPath, Path: ownerPath, Op: "identify", Cause: err}}}
	}
	if visited[actualOwner] {
		return []git.NestedResult{{Owner: ownerPath, Path: ownerPath, Attempted: true,
			Operation: "cycle", Error: &git.NestedError{Owner: ownerPath, Path: ownerPath, Op: "cycle", Cause: camperrors.Newf("repository already visited: %s", actualOwner)}}}
	}
	visited[actualOwner] = true
	defer delete(visited, actualOwner)
	paths, err := git.ListSubmodulePaths(ctx, ownerDir)
	if err != nil {
		return []git.NestedResult{{Owner: ownerPath, Path: ownerPath, Attempted: true,
			Operation: "list", Error: &git.NestedError{Owner: ownerPath, Path: ownerPath, Op: "list", Cause: err}}}
	}
	sort.Strings(paths)
	results := make([]git.NestedResult, 0, len(paths))
	for _, path := range paths {
		fullPath := filepath.Join(ownerPath, path)
		r := git.NestedResult{Owner: ownerPath, Path: fullPath, Name: path}
		fail := func(op string, cause error) {
			r.Operation = op
			r.Error = &git.NestedError{Owner: ownerPath, Path: fullPath, Op: op, Cause: cause}
		}
		if ctx.Err() != nil {
			r.Skipped = true
			fail("skip", ctx.Err())
			results = append(results, r)
			continue
		}
		r.Attempted = true
		if err := pathutil.ValidateSubmodulePath(ownerDir, path); err != nil {
			fail("validate-path", err)
			results = append(results, r)
			continue
		}
		childDir := filepath.Join(ownerDir, path)
		initialized, err := git.IsWorktreeRoot(ctx, childDir)
		if err != nil {
			fail("identify", err)
			results = append(results, r)
			continue
		}
		wasInitialized := initialized
		if initialized {
			cmd := exec.CommandContext(ctx, "git", "-C", ownerDir, "submodule", "sync", "--", path)
			if output, err := cmd.CombinedOutput(); err != nil {
				fail("sync-url", camperrors.WrapJoinf(git.ErrSubmoduleSync, err, "%s", strings.TrimSpace(string(output))))
				results = append(results, r)
				continue
			}
		}
		if !initialized {
			outcome, err := git.InitSubmoduleGracefulResult(ctx, ownerDir, path)
			r.StaleRefFallback = outcome.StaleRefFallback
			if err != nil {
				fail("init", err)
				results = append(results, r)
				continue
			}
		}
		initialized, err = git.IsWorktreeRoot(ctx, childDir)
		if err != nil || !initialized {
			if err == nil {
				err = git.ErrSubmoduleNotInitialized
			}
			fail("identify", err)
			results = append(results, r)
			continue
		}
		r.Initialized = true
		if wasInitialized {
			if err := s.ensureRecordedCommitAvailable(ctx, ownerDir, childDir, path); err != nil {
				fail("fetch", err)
				results = append(results, r)
				continue
			}
		}
		branch, err := git.CheckoutSubmoduleBranch(ctx, ownerDir, path, true)
		if err != nil {
			fail("checkout", err)
			results = append(results, r)
			continue
		}
		r.Branch = branch
		r.RecordedCommit, _ = recordedGitlinkSHAInOwner(ctx, ownerDir, path)
		warning, err := s.reconcileCheckoutDriftInOwner(ctx, ownerDir, path, childDir, branch)
		if err != nil {
			fail("drift", err)
			results = append(results, r)
			continue
		}
		r.Warning = warning
		r.Commit, _ = git.Output(ctx, childDir, "rev-parse", "HEAD")
		r.Children = s.initializeNestedSelected(ctx, childDir, fullPath, visited)
		if err := git.NestedFailures(r.Children); err != nil {
			fail("children", err)
		} else {
			r.Success = true
		}
		results = append(results, r)
	}
	return results
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
		initialized, identityErr := git.IsWorktreeRoot(ctx, fullPath)
		if identityErr != nil {
			result.Success = false
			result.Error = &SyncError{Op: "identify", Submodule: path, Cause: identityErr}
			results = append(results, result)
			continue
		}
		if !initialized {
			result.Success = false
			result.Error = &SyncError{Op: "identify", Submodule: path, Cause: git.ErrSubmoduleNotInitialized}
			results = append(results, result)
			continue
		}

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

// reportOrphanedGitlinks inventories undeclared gitlinks without mutating
// either the index or their working directories.
func (s *Syncer) reportOrphanedGitlinks(ctx context.Context) ([]git.OrphanedGitlink, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	orphans, err := git.ListOrphanedGitlinks(ctx, s.repoRoot)
	if err != nil {
		return nil, err
	}

	return orphans, nil
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

// validateUpdate retains the package-level validation contract for callers
// that need one error. Sync reports each inventory error independently.
func (s *Syncer) validateUpdate(ctx context.Context) error {
	_, validationErrors := s.validateInventory(ctx)
	return errors.Join(validationErrors...)
}

// validateInventory checks each child against the metadata and gitlink in its
// immediate owning repository. An orphan mapping never hides later failures.
func (s *Syncer) validateInventory(ctx context.Context) ([]string, []error) {
	if ctx.Err() != nil {
		return nil, []error{ctx.Err()}
	}
	inventory := git.InventorySubmodules(ctx, s.repoRoot)
	var warnings []string
	var validationErrors []error
	for _, inventoryErr := range inventory.Errors {
		validationErrors = append(validationErrors, &SyncError{Op: "validate-metadata", Cause: inventoryErr})
	}
	for _, entry := range inventory.Entries {
		switch entry.State {
		case git.InventoryDeclarationOnly:
			validationErrors = append(validationErrors, &SyncError{Op: "validate-metadata", Submodule: entry.Path,
				Cause: camperrors.Newf("declared in owner %s .gitmodules without an index gitlink; reconcile metadata", entry.Owner)})
		case git.InventoryGitlinkOnly:
			warnings = append(warnings, fmt.Sprintf("orphaned gitlink without .gitmodules declaration: %s", entry.Path))
		case git.InventoryUninitialized:
			validationErrors = append(validationErrors, &SyncError{Op: "validate-init", Submodule: entry.Path,
				Cause: camperrors.Wrapf(git.ErrSubmoduleNotInitialized, "owner %s; run 'camp sync'", entry.Owner)})
		case git.InventoryMatched:
			matches, err := git.SelectedCommitMatches(ctx, s.repoRoot, entry)
			if err != nil {
				validationErrors = append(validationErrors, &SyncError{Op: "validate-ancestry", Submodule: entry.Path,
					Cause: camperrors.Wrapf(err, "owner %s", entry.Owner)})
			} else if !matches {
				validationErrors = append(validationErrors, &SyncError{Op: "validate-drift", Submodule: entry.Path,
					Cause: camperrors.Newf("checkout diverges from owner %s gitlink; inspect branch history before updating", entry.Owner)})
			}
		}
		if entry.State != git.InventoryMatched && entry.State != git.InventoryUninitialized {
			continue
		}
		matchesURL, active, err := git.InventoryURLMatches(ctx, s.repoRoot, entry)
		if err != nil {
			validationErrors = append(validationErrors, &SyncError{Op: "validate-url", Submodule: entry.Path, Cause: err})
		} else if active == "" {
			validationErrors = append(validationErrors, &SyncError{Op: "validate-url", Submodule: entry.Path,
				Cause: camperrors.Newf("active URL unavailable in owner %s", entry.Owner)})
		} else if !matchesURL {
			validationErrors = append(validationErrors, &SyncError{Op: "validate-url", Submodule: entry.Path,
				Cause: camperrors.Newf("active URL %q differs from declaration %q in owner %s; run 'camp sync'", active, entry.DeclaredURL, entry.Owner)})
		}
	}
	return warnings, validationErrors
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

// ensureRecordedCommitAvailable fetches a newly pinned object into an existing
// checkout without changing its branch or worktree. Git's submodule update
// fetches this for a new checkout, but sync deliberately skips that update for
// an initialized branch so it must fetch the object separately.
func (s *Syncer) ensureRecordedCommitAvailable(ctx context.Context, ownerDir, subDir, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	recorded, err := recordedGitlinkSHAInOwner(ctx, ownerDir, path)
	if err != nil {
		return err
	}
	hasCommit := func() bool {
		return exec.CommandContext(ctx, "git", "-C", subDir, "cat-file", "-e", recorded+"^{commit}").Run() == nil
	}
	if hasCommit() {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.options.NoFetch {
		return camperrors.Newf("recorded commit %s is unavailable locally and --no-fetch was set", recorded)
	}
	fetch := func(refspec string) error {
		cmd := exec.CommandContext(ctx, "git", "-C", subDir, "fetch", "--no-tags", "origin", refspec)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return camperrors.Wrapf(err, "fetch %s from origin: %s", refspec, strings.TrimSpace(string(output)))
		}
		return nil
	}
	branchErr := fetch("+refs/heads/*:refs/remotes/origin/*")
	if err := ctx.Err(); err != nil {
		return err
	}
	if hasCommit() {
		return nil
	}
	if pinErr := fetch(recorded); pinErr != nil {
		return errors.Join(branchErr, pinErr)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !hasCommit() {
		return camperrors.Newf("origin did not provide recorded commit %s for %s", recorded, path)
	}
	return nil
}

func (s *Syncer) reconcileCheckoutDrift(ctx context.Context, path, subDir, branch string) (string, error) {
	return s.reconcileCheckoutDriftInOwner(ctx, s.repoRoot, path, subDir, branch)
}

func (s *Syncer) reconcileCheckoutDriftInOwner(ctx context.Context, ownerDir, path, subDir, branch string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if strings.TrimSpace(branch) == "" || branch == "HEAD" {
		return "", nil
	}

	recordedSHA, err := recordedGitlinkSHAInOwner(ctx, ownerDir, path)
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
	// Clone and sync intentionally select the branch tip. A branch that
	// descends from the recorded gitlink is a legitimate checkout policy,
	// even though git submodule status marks it with '+'.
	advanced, err := git.IsAncestor(ctx, subDir, recordedSHA, branchSHA)
	if err != nil {
		return fmt.Sprintf("%s: cannot verify whether %s extends recorded %s: %v",
			path, shortSHA(branchSHA), shortSHA(recordedSHA), err), nil
	}
	if advanced {
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

	return "", nil
}

func (s *Syncer) recordedGitlinkSHA(ctx context.Context, path string) (string, error) {
	return recordedGitlinkSHAInOwner(ctx, s.repoRoot, path)
}

func recordedGitlinkSHAInOwner(ctx context.Context, ownerDir, path string) (string, error) {
	output, err := git.Output(ctx, ownerDir, "ls-files", "-s", "--", path)
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
