package jobs

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
)

// execute runs one job against its repository.
//
// Every path here commits through camp's temp-index machinery, never the real
// index. That is the property that makes deferral safe at all: the user's
// index belongs to the user, and a background process that staged into it
// would sweep whatever they happened to be working on into camp's bookkeeping
// commit.
func execute(ctx context.Context, campaignRoot string, job *Job) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	repoPath, err := resolveRepoPath(campaignRoot, job.Repo)
	if err != nil {
		return err
	}

	switch job.Kind {
	case KindCommitPaths:
		return executeCommitPaths(ctx, repoPath, job)
	case KindCommitTree:
		// Sequence 03 wires this. Returning a clear error rather than a
		// silent success keeps a mis-enqueued job visible in failed/ instead
		// of vanishing.
		return camperrors.Wrapf(errNotWired,
			"commit-tree execution arrives in sequence 03 (job %s)", job.ID)
	default:
		return camperrors.Newf("unknown job kind %q in job %s", job.Kind, job.ID)
	}
}

// executeCommitPaths commits the job's named paths through the temp index.
//
// A job whose paths resolve to nothing is a success, not a failure: the
// content it was written for may already have been committed by a drain, and
// failing here would park a job that has nothing left to do.
func executeCommitPaths(ctx context.Context, repoPath string, job *Job) error {
	if len(job.Paths) == 0 {
		return camperrors.Newf("job %s has no paths", job.ID)
	}

	err := git.CommitScoped(ctx, repoPath, job.Paths, &git.CommitOptions{
		Message: job.Message,
	})
	switch {
	case err == nil:
		return nil
	case errors.Is(err, git.ErrNoChanges):
		return nil // already committed, or nothing to say
	default:
		return err
	}
}

// resolveRepoPath turns a campaign-relative repo into an absolute path,
// refusing anything that escapes the campaign.
//
// Validation already rejects escaping repos at enqueue. This is the second
// check, at the moment of use, because the job file is on disk and editable
// between those two moments.
func resolveRepoPath(campaignRoot, repo string) (string, error) {
	normalized := normalizeRepo(repo)
	if normalized == "." {
		return campaignRoot, nil
	}
	if !filepath.IsLocal(filepath.FromSlash(normalized)) {
		return "", camperrors.Newf("repo %q escapes the campaign", repo)
	}
	path := filepath.Join(campaignRoot, filepath.FromSlash(normalized))
	if _, err := os.Stat(path); err != nil {
		return "", camperrors.Wrapf(err, "repo %s is not present", repo)
	}
	return path, nil
}

// forbiddenGitArgs are staging and history operations a deferred job may never
// perform.
//
// A worker runs after the user's terminal has returned, against a working tree
// they are still using. Anything that stages by wildcard would capture whatever
// they happen to have edited since; anything that moves history would rewrite
// commits they may already have built on. The queue's contract is that a job's
// effect is fixed at enqueue time, and these are precisely the operations that
// would break it.
var forbiddenGitArgs = [][]string{
	{"add", "-A"},
	{"add", "--all"},
	{"add", "."},
	{"commit", "-a"},
	{"commit", "--all"},
	{"commit", "--amend"},
	{"reset", "--hard"},
	{"rebase"},
	{"push"},
	{"checkout"},
}

// GitArgsForJob returns the git argument list a job would run.
//
// It exists so the forbidden-operation rule is testable as data rather than by
// inspection: the test asserts over this function's output for every job
// shape, which is a claim about what the worker can do rather than about what
// it currently happens to do.
func GitArgsForJob(job *Job) []string {
	switch job.Kind {
	case KindCommitPaths:
		// CommitScoped's shape: build a temp index from HEAD, add exactly
		// these paths, commit with GIT_INDEX_FILE pointed at it.
		args := []string{"add", "--"}
		args = append(args, job.Paths...)
		return args
	case KindCommitTree:
		return []string{"commit-tree", job.Tree}
	default:
		return nil
	}
}

// ViolatesForbiddenGit reports whether an argument list contains a forbidden
// operation, and which one.
func ViolatesForbiddenGit(args []string) (string, bool) {
	for _, forbidden := range forbiddenGitArgs {
		if containsSequence(args, forbidden) {
			return strings.Join(forbidden, " "), true
		}
	}
	return "", false
}

// containsSequence reports whether args contains the given consecutive tokens.
func containsSequence(args, seq []string) bool {
	if len(seq) == 0 || len(args) < len(seq) {
		return false
	}
	for i := 0; i+len(seq) <= len(args); i++ {
		match := true
		for j := range seq {
			if args[i+j] != seq[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// SpawnIfNeeded starts a detached worker when a lane has work and no live
// worker.
//
// Called by every enqueuer. Losing a spawn race is harmless: the loser's
// acquireLane returns !ok and the process exits, which is why the check here
// can be cheap and approximate rather than synchronized. The expensive
// mistake would be the other direction, so when in doubt this spawns.
func SpawnIfNeeded(ctx context.Context, campaignRoot, repo string) {
	if ctx.Err() != nil {
		return
	}
	if empty, err := laneEmpty(campaignRoot, repo); err != nil || empty {
		return
	}

	queueDir := QueueDir(campaignRoot)
	slug := LaneSlug(repo)
	if laneLockFresh(queueDir, slug) {
		return // a live worker already has this lane
	}
	if countFreshLaneLocks(queueDir) >= laneCap {
		// At the cap. The running workers rediscover lanes when they finish,
		// so this lane is served without another process.
		return
	}

	exe, err := os.Executable()
	if err != nil {
		return // degrade quietly: the next command's spawn check retries
	}

	logPath := WorkerLogPath(campaignRoot)
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer func() { _ = logFile.Close() }()

	// Deliberately not exec.CommandContext: the child must outlive this
	// process, which is the entire point of deferring. Setsid detaches it from
	// the terminal so closing the shell does not take the worker with it.
	cmd := exec.Command(exe, "jobs", "run", "--campaign", campaignRoot)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdin = nil
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		logWorker(campaignRoot, "spawn-error lane=%s err=%v", repo, err)
		return
	}
	// Never Wait: the child is detached on purpose. Releasing the process
	// handle leaves it to init rather than making this process linger.
	_ = cmd.Process.Release()
	logWorker(campaignRoot, "spawned lane=%s pid=%d", repo, cmd.Process.Pid)
}
