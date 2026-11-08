package sync

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
)

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
