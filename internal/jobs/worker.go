package jobs

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Obedience-Corp/camp/internal/git"
)

// MaxAttempts is how many times a job may be reclaimed before it is parked in
// failed/. A job that has died three times is not going to succeed on the
// fourth; retrying forever would hide a real problem behind a busy queue.
//
// Exported because every message that mentions an attempt count renders it
// ("attempt 2 of 3"), and a hardcoded 3 in the copy would silently disagree
// with the worker the day this changes.
const MaxAttempts = 3

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
			// The follow-up is made durable before the parent is completed.
			//
			// Completing unlinks the parent's queue file. Enqueuing after that
			// leaves a window where a crash loses the follow-up permanently:
			// the parent is gone so nothing retries it, and the follow-up was
			// never written, so a drain observes neither and reports an empty
			// queue. Camp would have committed the project and silently never
			// recorded the pointer.
			//
			// Reversed, a crash in the same window leaves the parent in
			// running/ to be reclaimed and re-run, which enqueues the follow-up
			// a second time. Both re-runs are no-ops (the commit already
			// landed, the pointer is already current), so a duplicate costs a
			// wasted job and silent loss costs a commit that never happens.
			followErr := error(nil)
			if execErr == nil && job.Then != nil {
				followErr = enqueueFollowUp(ctx, campaignRoot, job)
				if hookInFollowUpWindow != nil {
					hookInFollowUpWindow(job)
				}
			}
			// A follow-up that could not be written fails the parent even
			// though its commit succeeded, so the job is retried rather than
			// quietly half-done. Camp promised a commit and a pointer update.
			if err := complete(cmp.Or(execErr, followErr)); err != nil {
				logWorker(campaignRoot, "complete-error lane=%s id=%s err=%v", repo, job.ID, err)
			}
			// A worker-made commit in the campaign root moves the history the
			// manifest record ties artifact state to, so it re-queues the
			// record like a foreground commit would. Manifest jobs themselves
			// are excluded: an unchanged root skips its commit, so without
			// the exclusion the one that did commit would re-queue forever.
			// Enqueue failure is logged, not fatal: the record is eventually
			// consistent, and the next commit re-queues it.
			if execErr == nil && followErr == nil && job.Kind != KindManifest && normalizeRepo(job.Repo) == "." {
				if _, err := EnqueueManifestsForRoots(ctx, campaignRoot); err != nil {
					logWorker(campaignRoot, "manifest-enqueue-error lane=%s id=%s err=%v", repo, job.ID, err)
				}
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

// hookInFollowUpWindow lets tests act inside the window where a follow-up has
// been made durable but its parent has not yet been completed. That instant is
// the durability claim itself: a crash there must leave both files on disk, so
// nothing is lost however the worker dies.
var hookInFollowUpWindow func(job *Job)

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
//
// Class is inherited rather than declared on Follow. A follow-up exists only to
// record its parent's commit, so it is exactly as drain-relevant as the parent;
// giving it an independent class would let a manifest's gitlink follow-up block
// the drains the manifest itself is exempt from, defeating the exemption one
// level down.
func enqueueFollowUp(ctx context.Context, campaignRoot string, job *Job) error {
	blobs, err := captureFollowUpBlobs(ctx, campaignRoot, job.Then)
	if err != nil {
		logWorker(campaignRoot, "follow-up-capture-error parent=%s err=%v", job.ID, err)
		return err
	}
	follow := Job{
		Kind:     job.Then.Kind,
		Class:    job.Class,
		Repo:     job.Then.Repo,
		Paths:    job.Then.Paths,
		Blobs:    blobs,
		FollowUp: true,
		Message:  job.Then.Message,
	}
	if _, err := Enqueue(ctx, campaignRoot, follow); err != nil {
		logWorker(campaignRoot, "follow-up-error parent=%s err=%v", job.ID, err)
		return err
	}
	logWorker(campaignRoot, "follow-up lane=%s parent=%s", job.Then.Repo, job.ID)
	return nil
}

// captureFollowUpBlobs is replaceable only so queue lifecycle tests can stay
// filesystem-only. Production always snapshots the checked-out gitlink after
// the parent project commit lands and before the follow-up becomes durable.
var captureFollowUpBlobs = snapshotFollowUpBlobs

func snapshotFollowUpBlobs(ctx context.Context, campaignRoot string, follow *Follow) ([]BlobRef, error) {
	repoPath, err := resolveRepoPath(campaignRoot, follow.Repo)
	if err != nil {
		return nil, err
	}
	refs := make([]BlobRef, 0, len(follow.Paths))
	for _, path := range follow.Paths {
		ref, err := git.CaptureGitlink(ctx, repoPath, path)
		if err != nil {
			return nil, err
		}
		refs = append(refs, BlobRef{Path: ref.Path, Mode: ref.Mode, SHA: ref.SHA})
	}
	return refs, nil
}

// reclaimLane returns abandoned jobs to pending and parks the exhausted ones.
//
// Reclaim consults the job file's mtime, not the lock: by the time this runs
// we already hold the lane, so any running job here belongs to a worker that
// is gone. A job that has burned MaxAttempts moves to failed/ rather than
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
		if job.Attempts < MaxAttempts {
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

// LogEvent records a queue-adjacent event from outside the worker.
//
// It exists for enqueuers that degrade a guarantee rather than fail: the
// degradation has to be recorded somewhere the user can find it, and the
// worker log is where everything else about a job's history already lives.
func LogEvent(campaignRoot, format string, args ...any) {
	logWorker(campaignRoot, format, args...)
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
