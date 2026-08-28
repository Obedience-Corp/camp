package jobs

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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

	// Belt and braces against a hand-edited queue file. Enqueue validation
	// already rejects a follow-up carrying its own; this refuses to run one
	// that reached disk anyway, whichever kind it is. Chaining is bounded at
	// one level because each link runs against a repository further from the
	// state its author saw.
	if job.FollowUp && job.Then != nil {
		return camperrors.Newf("job %s is a follow-up carrying its own follow-up", job.ID)
	}

	repoPath, err := resolveRepoPath(campaignRoot, job.Repo)
	if err != nil {
		return err
	}

	switch job.Kind {
	case KindCommitPaths:
		return executeCommitPaths(ctx, repoPath, job)
	case KindCommitTree:
		return executeCommitTree(ctx, campaignRoot, repoPath, job)
	case KindManifest:
		return executeManifest(ctx, campaignRoot, repoPath, job)
	case KindPush:
		return executePush(ctx, repoPath, job)
	default:
		return camperrors.Newf("unknown job kind %q in job %s", job.Kind, job.ID)
	}
}

// executeCommitPaths commits the job's named paths through the temp index.
//
// A job whose paths resolve to nothing is a success, not a failure. Between the
// enqueue and the run, the content may have been committed by a drain, swept up
// by an ordinary `camp commit`, or deleted by the user who changed their mind.
// None of those is camp failing to keep a promise, and parking the job would
// leave the failed-commit notice nagging about work nobody wants done.
func executeCommitPaths(ctx context.Context, repoPath string, job *Job) error {
	if len(job.Paths) == 0 {
		return camperrors.Newf("job %s has no paths", job.ID)
	}
	if err := checkGitlinkCarveOut(ctx, repoPath, job); err != nil {
		return err
	}

	// Captured content when the enqueuer recorded it, the live working tree
	// otherwise. The captured form is what makes the commit mean its message:
	// without it the job defers the read as well as the write, so an edit made
	// in the gap lands under the earlier operation's description.
	//
	// The fallback exists for hand-written queue files and for jobs enqueued
	// by an older camp, which have paths and no blobs.
	opts := &git.CommitOptions{Message: job.Message, Retry: WorkerRetry()}
	var err error
	if len(job.Blobs) > 0 {
		err = commitBlobs(ctx, repoPath, toGitBlobs(job.Blobs), opts)
	} else {
		err = commitScoped(ctx, repoPath, job.Paths, opts)
	}
	switch {
	case err == nil:
		return nil
	case errors.Is(err, git.ErrNoChanges):
		return nil // already committed, or nothing to say
	case isMissingPathspec(err):
		return nil // the paths are gone; there is nothing left to commit
	default:
		return err
	}
}

// commitBlobs and commitScoped are how a paths job reaches git. Variables so
// a test can assert the profile the worker commits under, which is otherwise a
// claim about code nobody can observe until a lock contention parks a job.
var (
	commitBlobs  = git.CommitBlobs
	commitScoped = git.CommitScoped
)

// WorkerRetry is the git lock-retry profile every deferred job runs under.
//
// The interactive profile gives up after six attempts and one five-second wait
// for a lock another process is holding, which is the right answer for a person
// watching a prompt and the wrong one here. A worker has already returned the
// user's terminal, and an execution failure does not spend the reclaim budget:
// completionFor parks the job on the first error, so one lost race against a
// concurrent agent session is a commit in failed/ rather than a retry. Every
// index.lock failure in this campaign's worker log is that race.
//
// A fresh copy per call so no caller can mutate the profile another job runs
// under.
func WorkerRetry() *git.RetryConfig {
	cfg := git.BackgroundRetryConfig()
	return &cfg
}

// checkGitlinkCarveOut enforces criterion 37l: only a worker-created follow-up
// may commit a submodule pointer.
//
// A gitlink records whatever the submodule's HEAD is at execution time, not a
// snapshot the enqueuer chose, so an ordinary deferred job committing one would
// publish a pointer to work nobody decided to publish. The follow-up is the one
// case where that is correct, because it exists only because its parent just
// moved that HEAD.
//
// Checked here rather than at enqueue because it needs the repository: whether
// a path is a gitlink is a fact about HEAD, not about the path's spelling.
func checkGitlinkCarveOut(ctx context.Context, repoPath string, job *Job) error {
	if job.FollowUp {
		return nil
	}
	for _, path := range job.Paths {
		if git.IsGitlink(ctx, repoPath, path) {
			return camperrors.Newf(
				"job %s would commit the submodule pointer %s; only a worker-created "+
					"follow-up may record a gitlink", job.ID, path)
		}
	}
	return nil
}

// toGitBlobs converts the job document's captured content into the git
// package's form.
func toGitBlobs(refs []BlobRef) []git.BlobRef {
	out := make([]git.BlobRef, 0, len(refs))
	for _, r := range refs {
		out = append(out, git.BlobRef{Path: r.Path, Mode: r.Mode, SHA: r.SHA})
	}
	return out
}

// isMissingPathspec reports whether a git failure is only "those paths are not
// here any more".
//
// Matched on message text because git offers no distinct exit code for it: `git
// add` exits 128 for a missing pathspec and for a corrupt repository alike, so
// keying on the code would swallow real failures. Narrow on purpose; anything
// else still fails the job and stays visible in failed/.
func isMissingPathspec(err error) bool {
	return err != nil && strings.Contains(err.Error(), "did not match any files")
}

// shortSHA abbreviates a hash for a message a human reads.
func shortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
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
	case KindManifest:
		return nil
	case KindPush:
		// A push job IS a push, so the forbidden-args rule — which exists to
		// prevent commit jobs from pushing as a side effect — does not apply.
		// The push runs through executePush directly, not through the staging
		// path GitArgsForJob exists to constrain.
		return nil
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

// shouldSpawnWorker reports whether a lane warrants starting a detached worker.
//
// Split out from the spawn itself so the decision can be asserted without
// starting a process. The guard tests could previously only state the negative
// cases, by observing that no worker was spawned; the positive ones had no way
// to be written at all, because saying "it would spawn" meant actually
// spawning. That gap is why this function's first question, the one that
// decides whether a lane holding only an abandoned running job is worth a
// worker, shipped without a test of its own.
//
// Cheap and approximate on purpose, like the spawn it guards: losing a race
// only wastes a process, because the loser's acquireLane returns !ok and it
// exits. Failing to spawn strands work, which is the expensive direction, so
// when in doubt this says yes.
func shouldSpawnWorker(campaignRoot, repo string) bool {
	work, err := laneNeedsWorker(campaignRoot, repo)
	if err != nil || !work {
		return false
	}

	queueDir := QueueDir(campaignRoot)
	if laneLockFresh(queueDir, LaneSlug(repo)) {
		return false // a live worker already has this lane
	}
	// At the cap. The running workers rediscover lanes when they finish, so
	// this lane is served without another process.
	return countFreshLaneLocks(queueDir) < laneCap
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
	if !shouldSpawnWorker(campaignRoot, repo) {
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
	detachProcess(cmd)
	cmd.Stdin = nil
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		logWorker(campaignRoot, "spawn-error lane=%s err=%v", repo, err)
		return
	}
	// Read before releasing. Release zeroes the handle's pid, so logging it
	// afterwards recorded "pid=-1" on every spawn the queue has ever made: the
	// one line that says which process was sent to serve a lane named no
	// process at all.
	pid := cmd.Process.Pid
	// Never Wait: the child is detached on purpose. Releasing the process
	// handle leaves it to init rather than making this process linger.
	_ = cmd.Process.Release()
	logWorker(campaignRoot, "spawned lane=%s pid=%d", repo, pid)
}
