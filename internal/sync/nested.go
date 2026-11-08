package sync

import (
	"context"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/pathutil"
)

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
