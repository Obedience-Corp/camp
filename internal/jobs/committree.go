package jobs

import (
	"context"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
)

// The commit-tree half of job execution: build the captured snapshot into a
// commit and move HEAD to it.
//
// It is separated from the path-scoped half because the hard part is entirely
// here. A commit-paths job stages content camp captured and commits it; a
// commit-tree job carries an immutable snapshot of the user's own index and has
// to land it on a branch other processes are still committing to, which is
// where every ordering rule in this package comes from.

// executeCommitTree builds a commit from the job's captured tree and moves HEAD
// to it.
//
// Nothing here touches the real index or the working tree. While HEAD remains
// at the captured parent, the immutable tree produces exactly the commit the
// user staged. A later commit that already versioned every captured path makes
// the job a no-op; an unrelated HEAD move fails rather than being overwritten.
func executeCommitTree(ctx context.Context, campaignRoot, repoPath string, job *Job) error {
	if strings.TrimSpace(job.Tree) == "" {
		return camperrors.Newf("job %s has no captured tree", job.ID)
	}

	// A reclaimed job may have died after its commit landed. Recognize that
	// before generating the message, not after: the writer is an external
	// process a retry should not pay for again, and a writer failing on the
	// retry must not fail a job whose commit is sitting in the log. A first
	// run has Attempts == 0 and skips the check.
	if job.Attempts > 0 && alreadyApplied(ctx, repoPath, job) {
		return nil
	}

	// An empty snapshot is a success, not a failure. The enqueue path refuses
	// these, but a hand-edited or older queue file can still carry one, and
	// parking it would leave a failed-job notice for work nobody asked for.
	// Same contract as executeCommitPaths when git.ErrNoChanges is returned.
	//
	// A variable so tests can drive the gating order without a repository.
	if empty, err := isEmptyCommitTree(ctx, repoPath, job); err != nil {
		return err
	} else if empty {
		return nil
	}

	// The real index keeps the captured tree staged until this job lands. A
	// direct commit (or another tool that stages everything) can therefore
	// sweep the queued content into a later commit while the worker is alive.
	// If that happened, the promise is fulfilled: requiring the exact queued
	// tree and parent pair would park a false failure whenever the later commit
	// also carried unrelated paths.
	//
	// Otherwise the captured change has to be re-applied on top of whatever
	// landed. That is probed here, before the writer, so a job that genuinely
	// cannot land still fails without first paying for an external process.
	tree, parent := job.Tree, job.Parent
	if head := git.HeadSHA(ctx, repoPath); head != "" && head != job.Parent {
		landed, err := headMoveFulfilled(ctx, repoPath, job, head)
		if err != nil {
			return err
		}
		if landed {
			return nil
		}
		merged, err := git.ReapplyTreeOnto(ctx, repoPath, job.Parent, job.Tree, head)
		if err != nil {
			return headMovedError(err, job.Parent, "")
		}
		tree, parent = merged, head
	}

	// The message describes the captured change, not the parent it lands on.
	// runWriter pins the writer's HEAD to job.Parent in a scratch git
	// directory, so what it sees is job.Parent to job.Tree whatever HEAD has
	// become, which is exactly the change a re-application carries forward.
	message, err := messageForTree(ctx, campaignRoot, repoPath, job)
	if err != nil {
		return err
	}

	return applyCapturedTree(ctx, campaignRoot, repoPath, job, tree, parent, message)
}

// headMoveReapplies bounds how many times one job re-applies its captured
// change after losing the compare-and-swap on HEAD.
//
// Each round costs two git plumbing calls and lands only if it wins the next
// swap, so a lane under constant contention could otherwise spin here for as
// long as the contention lasts. Four attempts is past any realistic burst of
// concurrent commits into one repository; beyond that the honest answer is the
// parked job the user can see, not a worker quietly retrying forever.
const headMoveReapplies = 3

// applyCapturedTree commits tree onto parent and moves HEAD to it, re-applying
// the captured change onto whatever landed when the swap loses a race.
//
// The swap is never widened. Every commit this makes still moves HEAD only
// from the exact commit its tree was built against, so nothing another process
// committed can be discarded; a lost race produces a new tree built on top of
// that commit rather than a swap that ignores it.
func applyCapturedTree(ctx context.Context, campaignRoot, repoPath string,
	job *Job, tree, parent, message string,
) error {
	for range headMoveReapplies + 1 {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		newSHA, err := git.CommitTree(ctx, repoPath, tree, parent, message)
		if err != nil {
			return err
		}
		moveErr := git.UpdateHeadFrom(ctx, repoPath, newSHA, parent,
			"camp: deferred commit "+job.ID)
		if moveErr == nil {
			return nil
		}
		nextTree, nextParent, err := recoverFromHeadMove(
			ctx, repoPath, job, tree, parent, newSHA, moveErr)
		if err != nil {
			return err
		}
		if nextTree == "" {
			return nil // the captured change is already in history
		}
		logWorker(campaignRoot, "head-moved-reapplied lane=%s id=%s onto=%s",
			job.Repo, job.ID, shortSHA(nextParent))
		tree, parent = nextTree, nextParent
	}
	return headMovedError(camperrors.Newf(
		"HEAD moved again during each of the %d re-applications camp attempted",
		headMoveReapplies), job.Parent, "")
}

// recoverFromHeadMove decides what a job does after losing the swap on HEAD.
//
// It returns the tree and parent to try next, or an empty tree when the work
// is already in history and the job is done.
//
// The re-application is a three-way merge, not a re-parent. A captured tree is
// a whole-repository snapshot, so hanging it off the new HEAD would revert
// every path the intervening commit touched; merging from the captured parent
// carries only what this job changed. A content conflict means the two commits
// disagree about the same lines, and there the queued snapshot stops being
// something camp can land on its own.
func recoverFromHeadMove(ctx context.Context, repoPath string, job *Job,
	tree, parent, newSHA string, moveErr error,
) (string, string, error) {
	// If HEAD is already this job's commit, a previous attempt succeeded and
	// died before the queue file was unlinked. Recognized by content, not by
	// hash: `git commit-tree` puts the committer timestamp in the object, so a
	// retry over identical inputs could never match what the first attempt
	// wrote.
	if alreadyApplied(ctx, repoPath, job) {
		return "", "", nil
	}
	head := git.HeadSHA(ctx, repoPath)
	if head == "" || head == parent {
		// Not a lost race: HEAD is unreadable, or it is exactly where the swap
		// reported it was not. Neither gives anything to re-apply onto.
		return "", "", headMovedError(moveErr, job.Parent, newSHA)
	}
	fulfilled, err := headMoveFulfilled(ctx, repoPath, job, head)
	if err == nil && fulfilled {
		return "", "", nil
	}
	merged, err := git.ReapplyTreeOnto(ctx, repoPath, parent, tree, head)
	if err != nil {
		return "", "", headMovedError(err, job.Parent, newSHA)
	}
	// A merge that reproduces HEAD's own tree adds nothing. Committing it
	// would put an empty commit in history under this job's message.
	if headTree, err := git.TreeSHA(ctx, repoPath, head); err == nil && headTree == merged {
		return "", "", nil
	}
	return merged, head, nil
}

// headMoveFulfilled reports whether a concurrent commit already carried the
// captured change.
//
// A later version of every path the snapshot touched, with no queued staging
// left, fulfills the job; unrelated additions in that commit do not make it a
// failure.
func headMoveFulfilled(ctx context.Context, repoPath string, job *Job, head string) (bool, error) {
	if alreadyApplied(ctx, repoPath, job) {
		return true, nil
	}
	return git.FirstParentChainContainsOrSupersedesTreeChanges(
		ctx, repoPath, job.Parent, job.Tree, head)
}

// alreadyApplied reports whether a commit this job set out to make has landed.
//
// It walks HEAD's first-parent chain rather than checking HEAD alone: the user
// may have kept committing between the crash and the retry, and a landed
// commit buried under their new work is still landed. A variable so tests can
// exercise the retry ordering without a repository behind the job.
var alreadyApplied = func(ctx context.Context, repoPath string, job *Job) bool {
	return git.FirstParentChainContains(ctx, repoPath, job.Tree, job.Parent)
}

// isEmptyCommitTree reports whether the job's tree is identical to its
// parent's tree — an empty commit that must never land.
//
// Variable so tests can exercise executeCommitTree's gate order without a
// repository behind the job; treeUnchangedFromParent is always what runs in
// production.
var isEmptyCommitTree = treeUnchangedFromParent

func treeUnchangedFromParent(ctx context.Context, repoPath string, job *Job) (bool, error) {
	parentTree, err := git.TreeSHA(ctx, repoPath, job.Parent)
	if err != nil {
		return false, camperrors.Wrapf(err, "resolve parent tree for job %s", job.ID)
	}
	return parentTree == job.Tree, nil
}

// headMovedError is the user-visible failure when HEAD is no longer the
// captured parent. Noticing that before the writer and noticing it at
// update-ref must produce the same text: empty parent is unborn HEAD, not
// a missing "expected parent".
func headMovedError(cause error, parent, newSHA string) error {
	msg := headMovedMessage(parent, newSHA)
	if cause != nil {
		return camperrors.Wrap(cause, msg)
	}
	return camperrors.New(msg)
}

func headMovedMessage(parent, newSHA string) string {
	if strings.TrimSpace(parent) == "" {
		if newSHA != "" {
			return "HEAD is no longer unborn; " + shortSHA(newSHA) +
				" was not applied (queued as the first commit)"
		}
		return "HEAD is no longer unborn; captured changes were not applied (queued as the first commit)"
	}
	if newSHA != "" {
		return "HEAD moved since this commit was queued; " + shortSHA(newSHA) +
			" was not applied (expected parent " + shortSHA(parent) + ")"
	}
	return "HEAD moved since this commit was queued; captured changes were not applied (expected parent " +
		shortSHA(parent) + ")"
}
