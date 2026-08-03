package jobs

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
)

// The read side of the queue: what `camp jobs`, the failed-job notice, and the
// doctor check all ask.
//
// They share one traversal rather than each growing its own, because they must
// agree. A notice that says two jobs failed while `camp jobs` shows one is
// worse than either being wrong alone: the user cannot tell which to believe,
// and the queue's whole job is to be trustworthy about work camp promised.

// States are the queue's three directories, in the order a listing shows them:
// what has not run, what is running, what gave up.
var States = []string{statePending, stateRunning, stateFailed}

// Entry is one job plus the facts that are not in its document.
type Entry struct {
	Job
	// State is the directory the job is in, which is its state.
	State string
	// Lane is the campaign-relative repo, decoded from the lane directory.
	Lane string
	// Stuck marks a running job whose lane has no live worker. It means the
	// job will be reclaimed when a worker next takes the lane, not that it has
	// failed; naming it is how a user tells "slow" from "nobody is home".
	Stuck bool
}

// Age is how long ago the job was enqueued. Zero when the timestamp is
// unreadable, which is better than a nonsense duration.
func (e Entry) Age(now time.Time) time.Duration {
	t, err := time.Parse("2006-01-02T15:04:05.000Z", e.CreatedAt)
	if err != nil {
		return 0
	}
	if d := now.Sub(t); d > 0 {
		return d
	}
	return 0
}

// Snapshot returns every job in every lane, ordered by state, then lane, then
// sequence, which is the order they would run.
func Snapshot(campaignRoot string) ([]Entry, error) {
	var out []Entry
	queueDir := QueueDir(campaignRoot)

	for _, state := range States {
		slugs, err := Lanes(campaignRoot, state)
		if err != nil {
			return nil, err
		}
		for _, slug := range slugs {
			lane := RepoFromLaneSlug(slug)
			jobList, err := List(campaignRoot, state, lane)
			if err != nil {
				return nil, err
			}
			// One lock stat per lane rather than per job: a lane has one
			// worker, so the answer cannot differ between its jobs.
			laneAlive := laneLockFresh(queueDir, slug)
			for _, job := range jobList {
				out = append(out, Entry{
					Job:   job,
					State: state,
					Lane:  lane,
					Stuck: state == stateRunning && !laneAlive,
				})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].State != out[j].State {
			return stateRank(out[i].State) < stateRank(out[j].State)
		}
		if out[i].Lane != out[j].Lane {
			return out[i].Lane < out[j].Lane
		}
		return out[i].Seq < out[j].Seq
	})
	return out, nil
}

func stateRank(state string) int {
	switch state {
	case stateFailed:
		return 0 // failures first: they are the rows that need a decision
	case stateRunning:
		return 1
	default:
		return 2
	}
}

// Superseded reports whether a failed job can never succeed on a retry because
// history moved past the commit it was queued against.
//
// executeCommitTree refuses to replay a captured tree onto a parent the user
// did not choose, so a commit-tree job whose parent is no longer HEAD is not
// waiting for a better moment: retrying it is guaranteed to fail again, every
// time, forever. Telling a user to retry that job sends them around a loop
// with no exit, which is worse than telling them nothing.
//
// A job whose commit already landed is deliberately not superseded. Retry is
// the right action there: it recognizes its own work and clears the queue.
//
// Best effort by design. Anything unreadable answers false, because the cost
// of a wrong "cannot retry" (a user drops recoverable work) is higher than the
// cost of a wrong "try it" (they retry once and see it fail).
func Superseded(ctx context.Context, campaignRoot string, e Entry) bool {
	if e.State != stateFailed || e.Kind != KindCommitTree {
		return false
	}
	if strings.TrimSpace(e.Parent) == "" || strings.TrimSpace(e.Tree) == "" {
		return false
	}
	repoPath, err := resolveRepoPath(campaignRoot, e.Repo)
	if err != nil {
		return false
	}
	head := git.HeadSHA(ctx, repoPath)
	if head == "" || head == e.Parent {
		return false
	}
	return !git.FirstParentChainContains(ctx, repoPath, e.Tree, e.Parent)
}

// FailedCount reports how many jobs are parked in failed/.
//
// Every foreground camp command calls this before its own work, so the empty
// case has to cost nothing. It does: no campaign has a failed/ directory until
// something fails, so the common path is a single failed ReadDir. Errors are
// swallowed rather than surfaced, because a notice that cannot be computed must
// not break the command it was going to annotate.
func FailedCount(campaignRoot string) int {
	failedDir := filepath.Join(QueueDir(campaignRoot), stateFailed)
	lanes, err := os.ReadDir(failedDir)
	if err != nil {
		return 0
	}
	count := 0
	for _, lane := range lanes {
		if !lane.IsDir() {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(failedDir, lane.Name()))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
				count++
			}
		}
	}
	return count
}

// SelectorAll is the token that means every matching job.
const SelectorAll = "all"

// Retry moves failed jobs back to pending with their attempts reset.
//
// Attempts reset rather than continue because a retry is a human deciding the
// cause is gone. Keeping the count would let a job that failed three times for
// a reason now fixed be parked again on its first new attempt, which reads as
// camp ignoring the instruction.
func Retry(ctx context.Context, campaignRoot, selector string) ([]Job, error) {
	return moveFailed(ctx, campaignRoot, selector, func(job *Job, path, name string) error {
		job.Attempts = 0
		pendingDir := laneDir(campaignRoot, statePending, job.Repo)
		if err := os.MkdirAll(pendingDir, 0o755); err != nil {
			return camperrors.Wrapf(err, "create pending lane %s", pendingDir)
		}
		data, err := marshalJob(job)
		if err != nil {
			return err
		}
		// Written before the unlink, so a crash between them duplicates a job
		// rather than losing it. Same ordering as Reclaim, same reason: a job
		// that runs twice is recoverable, one that never runs is not.
		if err := os.WriteFile(filepath.Join(pendingDir, name), data, 0o644); err != nil {
			return camperrors.Wrapf(err, "requeue job %s", job.ID)
		}
		return os.Remove(path)
	})
}

// Drop discards failed jobs, leaving their content untouched on disk.
//
// Only the job file is removed. The intent, manifest, or workitem marker the
// job was going to commit stays in the working tree, uncommitted, for the next
// ordinary commit to pick up. Deleting the content would make "give up on this
// commit" mean "throw away my work", which is not what a user asking to drop a
// stuck job is asking for, and is unrecoverable if it is wrong.
func Drop(ctx context.Context, campaignRoot, selector string) ([]Job, error) {
	return moveFailed(ctx, campaignRoot, selector, func(_ *Job, path, _ string) error {
		return os.Remove(path)
	})
}

// moveFailed applies an action to the failed jobs a selector names.
func moveFailed(ctx context.Context, campaignRoot, selector string,
	action func(job *Job, path, name string) error,
) ([]Job, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	slugs, err := Lanes(campaignRoot, stateFailed)
	if err != nil {
		return nil, err
	}

	var acted []Job
	for _, slug := range slugs {
		lane := RepoFromLaneSlug(slug)
		dir := laneDir(campaignRoot, stateFailed, lane)
		names, err := sortedJobFiles(dir)
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			path := filepath.Join(dir, name)
			job, readErr := readJob(path)
			if readErr != nil {
				// An unreadable job file cannot be matched by id, so only
				// "all" can act on it. Leaving it forever would make the
				// failed-job notice permanent with nothing able to clear it.
				if selector != SelectorAll {
					continue
				}
				if err := os.Remove(path); err != nil {
					return nil, camperrors.Wrapf(err, "drop unreadable job %s", path)
				}
				continue
			}
			if selector != SelectorAll && job.ID != selector {
				continue
			}
			if err := action(job, path, name); err != nil {
				return nil, err
			}
			acted = append(acted, *job)
		}
	}
	if len(acted) == 0 && selector != SelectorAll {
		return nil, camperrors.Newf("no failed job with id %q", selector)
	}
	return acted, nil
}

// StuckRunning returns running jobs whose lane has no live worker.
//
// These are not failures: a worker will reclaim them when it next takes the
// lane. They are worth reporting because the wait is invisible otherwise, and a
// user watching a job sit in running/ has no way to tell whether something is
// working on it.
func StuckRunning(campaignRoot string) ([]Entry, error) {
	all, err := Snapshot(campaignRoot)
	if err != nil {
		return nil, err
	}
	var stuck []Entry
	for _, e := range all {
		if e.Stuck {
			stuck = append(stuck, e)
		}
	}
	return stuck, nil
}
