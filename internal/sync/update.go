package sync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	gosync "sync"

	"github.com/Obedience-Corp/camp/internal/git"
)

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
