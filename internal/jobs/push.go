package jobs

import (
	"context"
	"os"
	"os/exec"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// EnqueuePush records a deferred push job at the tail of repo's lane.
//
// Called by `camp push` when a blocking drain would hold the terminal — i.e.,
// the queue has outstanding work and stdin is a TTY. Lane Seq ordering
// guarantees the push runs after the pending commits it was waiting for, so
// the ordering barrier holds without the user paying for it with their
// terminal.
//
// The remote and branch are captured here, not at execution: a branch switch
// between enqueue and execution would push the wrong branch otherwise.
func EnqueuePush(ctx context.Context, campaignRoot, repo, remote, branch string) (*Job, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if strings.TrimSpace(remote) == "" {
		return nil, camperrors.NewValidation("remote", "push requires a remote name", nil)
	}
	if strings.TrimSpace(branch) == "" {
		return nil, camperrors.NewValidation("branch", "push requires a branch name", nil)
	}
	job := Job{
		Kind:   KindPush,
		Class:  ClassCommit,
		Repo:   repo,
		Remote: remote,
		Branch: branch,
	}
	return Enqueue(ctx, campaignRoot, job)
}

// executePush pushes the job's branch to the job's remote.
//
// The remote and branch were recorded at enqueue so a deferred push publishes
// what the user asked for rather than whatever the upstream happens to be at
// execution. The ref is pushed by name, never HEAD, so a branch switch between
// enqueue and execution cannot publish the wrong branch.
//
// GIT_TERMINAL_PROMPT=0 makes an auth prompt fail fast rather than wedging the
// lane: a detached worker has no terminal to answer one, so without this the
// process would hang indefinitely and the lane would never progress.
//
// A non-fast-forward rejection parks immediately rather than burning retries,
// because retrying a push the remote rejected for the same reason is a loop
// with no exit. The user is told to pull or force-push; the job sits in failed/
// until they decide.
func executePush(ctx context.Context, repoPath string, job *Job) error {
	if strings.TrimSpace(job.Remote) == "" || strings.TrimSpace(job.Branch) == "" {
		return camperrors.Newf("push job %s has no remote or branch", job.ID)
	}

	cmd := exec.CommandContext(ctx, "git", "-C", repoPath,
		"push", job.Remote, job.Branch)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	// A detached worker has no terminal. Without this, an auth prompt hangs the
	// lane forever: the process blocks waiting for input nobody can give, and
	// the job never fails so it never parks and never gets noticed.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	if err := cmd.Run(); err != nil {
		return classifyPushError(err)
	}
	return nil
}

// classifyPushError turns a git push failure into one the worker can act on.
//
// A non-fast-forward rejection is not a transient failure: retrying will hit the
// same rejection and burn the job's attempts for nothing. Parking it on the
// first rejection tells the user the push needs a decision (pull, rebase, or
// force-push) rather than letting the queue retry pointlessly.
//
// Auth and network failures are returned as-is: they may be transient (the
// remote was momentarily unreachable), and the worker's retry budget handles
// them the same way it handles any other potentially transient failure.
func classifyPushError(err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	// Non-fast-forward: the remote has commits this branch does not. Retrying
	// without integrating them is a guaranteed repeat of the same rejection.
	if strings.Contains(msg, "non-fast-forward") ||
		strings.Contains(msg, "[rejected]") {
		return camperrors.Newf("push rejected (non-fast-forward); pull or force-push: %w", err)
	}
	return err
}
