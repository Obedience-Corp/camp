package jobs

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/autowrite"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
)

// The commit-message step of a deferred commit-tree job.
//
// It is the one part of executing a job that runs somebody else's program.
// Everything else here is camp calling git with arguments it chose, bounded by
// how long git takes; this hands control to a configured tool that may be an
// LLM on the other side of a network, and gets it back only when that tool
// decides to return. That is why the bound, the process group, and the failure
// vocabulary all live together in this file.

// messageForTree returns the commit message for a commit-tree job, running the
// configured writer when the job asked for one.
//
// The writer runs against a temporary index materializing the captured tree, so
// what it sees is the snapshot being committed rather than whatever the working
// tree looks like now. That is the difference between a deferred message that
// describes the commit and one that describes an unrelated later state.
//
// A writer that fails, prints nothing, or runs past its bound fails the job.
// Camp does not invent a subject: a filler commit in history is worse than a
// parked job the user can retry once the writer is healthy, or drop and
// re-commit by hand.
func messageForTree(ctx context.Context, campaignRoot, repoPath string, job *Job) (string, error) {
	if !job.AutoWrite {
		if strings.TrimSpace(job.Message) == "" {
			return "", camperrors.Newf("job %s has no message", job.ID)
		}
		return job.Message, nil
	}

	message, err := writeMessage(ctx, campaignRoot, repoPath, job)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(message) == "" {
		return "", camperrors.Newf("the commit message writer produced no message for job %s", job.ID)
	}

	// The tag goes on the subject line, which is why it is prepended here
	// rather than composed into the writer's prompt: the writer owns the
	// message, camp owns the tag, and a deferred commit has to end up with the
	// same subject a synchronous one would.
	if job.MessagePrefix != "" {
		return job.MessagePrefix + " " + message, nil
	}
	return message, nil
}

// writeMessage runs the configured writer against the job's captured tree.
//
// The temp index is the point. The writer's job is to describe *this* commit,
// and by the time it runs the working tree and the real index may both have
// moved on. Materializing the captured tree into a scratch index and pointing
// GIT_INDEX_FILE at it means `git diff --cached` inside the writer shows the
// snapshot being committed rather than whatever the user is doing now.
//
// A variable so tests can drive the worker's lifecycle without a configured
// writer; runWriter is always what runs in production.
var writeMessage = runWriter

func runWriter(ctx context.Context, campaignRoot, repoPath string, job *Job) (string, error) {
	// Loaded here rather than inside autowrite's convenience wrapper because
	// the worker needs both halves of the hook: the command to run and the
	// bound to run it under. Reading it once also means the reason a timeout
	// reports and the command that timed out cannot disagree.
	hook, err := autowrite.LoadCommitMessageHook(ctx, campaignRoot)
	if err != nil {
		return "", err
	}

	commonDir, err := git.Output(ctx, repoPath, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", camperrors.Wrap(err, "resolve the repository for the message writer")
	}
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(repoPath, commonDir)
	}
	commonDir, err = filepath.Abs(commonDir)
	if err != nil {
		return "", camperrors.Wrap(err, "resolve the repository path for the message writer")
	}

	// GIT_INDEX_FILE isolates only the staged tree. `git diff --cached` still
	// compares that index to the repository's live HEAD, so a commit landing
	// while a slow writer runs changes the input underneath it. Give the writer
	// a private per-worktree git directory whose detached HEAD is the captured
	// parent, while GIT_COMMON_DIR keeps objects and config in the real repo.
	// Together these two files are the complete staged snapshot the writer was
	// asked to describe: parent plus index, both immutable for the whole run.
	gitDir, err := os.MkdirTemp("", "camp-job-git-*")
	if err != nil {
		return "", camperrors.Wrap(err, "create a scratch git directory for the message writer")
	}
	defer func() { _ = os.RemoveAll(gitDir) }()
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte(job.Parent+"\n"), 0o600); err != nil {
		return "", camperrors.Wrap(err, "pin the captured parent for the message writer")
	}
	indexPath := filepath.Join(gitDir, "index")

	env := append(os.Environ(),
		"GIT_DIR="+gitDir,
		"GIT_COMMON_DIR="+commonDir,
		"GIT_WORK_TREE="+repoPath,
		"GIT_INDEX_FILE="+indexPath,
	)
	if err := git.RunWithEnv(ctx, repoPath, env, "read-tree", job.Tree); err != nil {
		return "", camperrors.Wrapf(err, "materialize tree %s for the message writer", shortSHA(job.Tree))
	}

	// The job's own variables first and Camp's git snapshot last, so a malformed
	// or stale job cannot point the writer at a different repository, parent, or
	// index than the ones just materialized.
	writerEnv := append(append([]string(nil), job.Env...),
		"GIT_DIR="+gitDir,
		"GIT_COMMON_DIR="+commonDir,
		"GIT_WORK_TREE="+repoPath,
		"GIT_INDEX_FILE="+indexPath,
	)

	// Bounded, and in a process group of its own, because this is the deferred
	// path: there is no terminal in front of the writer and nobody to press
	// Ctrl+C. An unbounded run here holds the lane, and every drain queued
	// behind it, for as long as the writer stays alive; the incident this
	// bound was added for was fifty minutes of a writer that was never going
	// to return, with `camp jobs` reporting it as ordinary progress.
	//
	// Stderr goes to the worker's own stderr, which SpawnIfNeeded wires to
	// worker.log: the writer's live diagnostics are the only account of what
	// the writer was doing when it stopped making progress.
	return autowrite.RunCommitMessageCommandWithOptions(ctx, repoPath, hook.Command,
		autowrite.RunOptions{
			Env:             writerEnv,
			Timeout:         hook.Timeout,
			OwnProcessGroup: true,
			DiagnosticOut:   os.Stderr,
		})
}
