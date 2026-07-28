package jobs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// maxAttempts is how many times a job may be reclaimed before it is parked in
// failed/. A job that has died three times is not going to succeed on the
// fourth; retrying forever would hide a real problem behind a busy queue.
const maxAttempts = 3

// Run serves every lane with pending work and returns when none is left.
//
// One spawned worker drains the whole queue rather than only the lane that
// triggered it. That keeps the spawn decision trivial for enqueuers (they
// never have to reason about which lanes exist) and means a single process
// handles a burst of enqueues across repos.
func Run(ctx context.Context, campaignRoot string) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		lanes, err := lanesWithWork(campaignRoot)
		if err != nil {
			return err
		}
		if len(lanes) == 0 {
			return nil
		}

		var served atomic.Bool
		var wg sync.WaitGroup
		sem := make(chan struct{}, laneCap)
		for _, lane := range lanes {
			wg.Add(1)
			go func(lane string) {
				defer wg.Done()
				select {
				case sem <- struct{}{}:
				case <-ctx.Done():
					return
				}
				defer func() { <-sem }()
				if runLane(ctx, campaignRoot, lane) {
					served.Store(true)
				}
			}(lane)
		}
		wg.Wait()

		// If a whole pass took no lane, every lane with work is held by
		// another live worker and this process has no role. Returning is not
		// an optimization: without it the loop rediscovers the same held lanes
		// forever and burns a core doing nothing, and a queue whose second
		// worker spins is worse than one that never started it.
		//
		// The work is not dropped. The worker holding those lanes runs its own
		// check-after-release before exiting, so anything queued behind it is
		// still served.
		if !served.Load() {
			return nil
		}

		// Otherwise loop: lanes enqueued while we worked deserve service from
		// this process rather than waiting for the next spawn.
	}
}

// runLane drains one lane, holding its lock for the duration. It reports
// whether this process actually took the lane, so Run can tell "nothing left
// to do" apart from "someone else is doing it".
func runLane(ctx context.Context, campaignRoot, repo string) (served bool) {
	queueDir := QueueDir(campaignRoot)
	slug := LaneSlug(repo)

	for {
		if ctx.Err() != nil {
			return served
		}

		lock, ok, err := acquireLane(queueDir, slug)
		if err != nil || !ok {
			return served // a live worker owns this lane; not ours to serve
		}
		served = true

		// Startup reclaim happens after winning the lock, never before: a
		// running job in a lane we now hold was abandoned by definition,
		// because a live worker would have kept its lock fresh and we would
		// not be here.
		reclaimLane(ctx, campaignRoot, repo)

		for {
			if ctx.Err() != nil {
				lock.release()
				return served
			}
			job, complete, err := Claim(ctx, campaignRoot, repo)
			if err != nil {
				logWorker(campaignRoot, "claim-error lane=%s err=%v", repo, err)
				break
			}
			if job == nil {
				break // lane drained
			}

			logWorker(campaignRoot, "claimed lane=%s seq=%d id=%s kind=%s", repo, job.Seq, job.ID, job.Kind)
			execErr := executeJob(ctx, campaignRoot, job)
			if execErr != nil {
				logWorker(campaignRoot, "failed lane=%s seq=%d id=%s err=%v", repo, job.Seq, job.ID, execErr)
			} else {
				logWorker(campaignRoot, "done lane=%s seq=%d id=%s", repo, job.Seq, job.ID)
			}
			if err := complete(execErr); err != nil {
				logWorker(campaignRoot, "complete-error lane=%s id=%s err=%v", repo, job.ID, err)
			}
			if execErr == nil && job.Then != nil {
				enqueueFollowUp(ctx, campaignRoot, job)
			}
		}

		lock.release()

		// The window itself: the lane is drained and the lock is gone, so an
		// enqueuer arriving now would see no live worker. Tests inject here to
		// stand in for the enqueuer that arrived a moment earlier, saw the
		// lock still fresh, and therefore did not spawn.
		if hookInReleaseWindow != nil {
			hookInReleaseWindow(repo)
		}

		// Check-after-release. This ordering is the whole fix for the strand
		// race: an enqueuer that saw our lock still fresh will have skipped
		// spawning, so a job written in the window between our last claim and
		// our release has nobody coming for it. Re-scanning here is what makes
		// "a queued job is always eventually served" true rather than usually
		// true.
		empty, err := laneEmpty(campaignRoot, repo)
		if err != nil || empty {
			return served
		}
	}
}

// hookInReleaseWindow lets tests act inside the window between a lane's lock
// being released and the re-scan that follows it. That window is otherwise
// unobservable from outside the process, and it is precisely where the strand
// race lives.
var hookInReleaseWindow func(repo string)

// executeJob is how the worker runs a job. It is a variable so tests can drive
// the worker's lifecycle without a real repository behind every job; execute
// is always what runs in production.
var executeJob = execute

// enqueueFollowUp writes a job's follow-up once the job itself has landed.
//
// Enqueued after completion so a follow-up can never run against a commit that
// did not happen. The follow-up carries no Then of its own, which validation
// enforces, so chaining is bounded at one level.
func enqueueFollowUp(ctx context.Context, campaignRoot string, job *Job) {
	follow := Job{
		Kind:  job.Then.Kind,
		Repo:  job.Then.Repo,
		Paths: job.Then.Paths,
		Message: fmt.Sprintf("[camp] record %s after %s",
			job.Then.Paths[0], job.ID),
	}
	if _, err := Enqueue(ctx, campaignRoot, follow); err != nil {
		logWorker(campaignRoot, "follow-up-error parent=%s err=%v", job.ID, err)
		return
	}
	logWorker(campaignRoot, "follow-up lane=%s parent=%s", job.Then.Repo, job.ID)
}

// reclaimLane returns abandoned jobs to pending and parks the exhausted ones.
//
// Reclaim consults the job file's mtime, not the lock: by the time this runs
// we already hold the lane, so any running job here belongs to a worker that
// is gone. A job that has burned maxAttempts moves to failed/ rather than
// cycling forever, because a queue that retries a poisoned job indefinitely
// hides the failure behind apparent activity.
func reclaimLane(ctx context.Context, campaignRoot, repo string) {
	if err := parkExhausted(campaignRoot, repo); err != nil {
		logWorker(campaignRoot, "park-error lane=%s err=%v", repo, err)
	}
	n, err := Reclaim(ctx, campaignRoot, repo, laneLiveness)
	if err != nil {
		logWorker(campaignRoot, "reclaim-error lane=%s err=%v", repo, err)
		return
	}
	if n > 0 {
		logWorker(campaignRoot, "reclaimed lane=%s count=%d", repo, n)
	}
	if err := parkExhausted(campaignRoot, repo); err != nil {
		logWorker(campaignRoot, "park-error lane=%s err=%v", repo, err)
	}
}

// parkExhausted moves pending jobs that have exhausted their attempts to
// failed/, so the worker does not pick them up again.
func parkExhausted(campaignRoot, repo string) error {
	pendingDir := laneDir(campaignRoot, statePending, repo)
	names, err := sortedJobFiles(pendingDir)
	if err != nil {
		return err
	}
	for _, name := range names {
		path := filepath.Join(pendingDir, name)
		job, readErr := readJob(path)
		if readErr != nil {
			continue
		}
		if job.Attempts < maxAttempts {
			continue
		}
		if err := failJobFile(campaignRoot, repo, path, name); err != nil {
			return err
		}
		logWorker(campaignRoot, "parked lane=%s id=%s attempts=%d", repo, job.ID, job.Attempts)
	}
	return nil
}

// lanesWithWork returns the repos whose pending lane holds at least one job.
func lanesWithWork(campaignRoot string) ([]string, error) {
	slugs, err := Lanes(campaignRoot, statePending)
	if err != nil {
		return nil, err
	}
	var repos []string
	for _, slug := range slugs {
		repo := RepoFromLaneSlug(slug)
		empty, err := laneEmpty(campaignRoot, repo)
		if err != nil || empty {
			continue
		}
		repos = append(repos, repo)
	}
	sort.Strings(repos)
	return repos, nil
}

// laneEmpty reports whether a lane has no pending jobs.
func laneEmpty(campaignRoot, repo string) (bool, error) {
	names, err := sortedJobFiles(laneDir(campaignRoot, statePending, repo))
	if err != nil {
		return false, err
	}
	return len(names) == 0, nil
}

// WorkerLogPath is the shared worker log for a campaign.
func WorkerLogPath(campaignRoot string) string {
	return filepath.Join(QueueDir(campaignRoot), "worker.log")
}

// logWorker appends one greppable line per state transition.
//
// The log is the only window into work that by definition happened after the
// user's terminal returned. It is append-only and one line per transition so
// `grep <job-id> worker.log` reconstructs a job's whole history.
func logWorker(campaignRoot, format string, args ...any) {
	path := WorkerLogPath(campaignRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = fmt.Fprintf(f, "%s %s\n",
		time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
		fmt.Sprintf(format, args...))
}

// errNotWired reports a job kind whose execution lands in a later sequence.
var errNotWired = camperrors.New("job kind not wired yet")
