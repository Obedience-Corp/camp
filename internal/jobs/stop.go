package jobs

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// Giving up on a job that is still running.
//
// Every other way out of the queue is the queue's own decision: a job fails, a
// worker dies, a drain waits. This is the one place a person overrules it, and
// it exists because the queue cannot tell a writer that is thinking from one
// that will never answer. The worker's bound eventually calls it, but "eventually"
// is up to the whole budget away, and a user staring at a wedged terminal is
// entitled to end it now.

// Worker stop timing.
//
// Variables rather than constants so tests drive the escalation instead of
// sleeping through it.
var (
	// workerStopGrace is how long a worker gets to stop on its own after
	// SIGTERM. It has real work to do in that window: its cancel kills the
	// message writer's process group and returns any other job it was running
	// to pending, so cutting the grace short trades a tidy shutdown for
	// orphaned processes and jobs left in running/ for the liveness window.
	workerStopGrace = 5 * time.Second
	// workerStopPoll is how often the wait re-checks. A stopping worker
	// usually goes in milliseconds; polling this often keeps the common case
	// feeling immediate without spinning.
	workerStopPoll = 25 * time.Millisecond
)

// WorkerStop reports what stopping a lane's worker took.
//
// Carried back to the caller rather than printed here, because camp acting on
// its own behalf has to say what it did: a process the user did not start was
// killed on their instruction, and that is not something to do silently.
type WorkerStop struct {
	// Lane is the campaign-relative repo whose worker this was.
	Lane string
	// PID is the worker that was signalled. Zero when the lane had no live
	// worker and nothing needed stopping.
	PID int
	// Killed reports that the worker ignored SIGTERM and had to be killed.
	// A killed worker never ran its cancel, so anything it had started may
	// have outlived it.
	Killed bool
}

// DropRunning stops the worker on each matching running job's lane and
// discards the job, keeping its content.
//
// The order is the whole correctness argument, so it is worth stating plainly:
// the job file leaves running/ before the worker is signalled, never after.
// A worker told to stop returns the jobs in its lane to pending, which is right
// when camp is shutting down and exactly wrong here, because the user just
// asked for this job to stop. Removing the file first means the worker's
// shutdown finds nothing of ours to put back, and the job stays dropped rather
// than reappearing in pending seconds later and being served again.
//
// Like Drop, only the job file goes. Whatever it was going to commit stays in
// the working tree for the next ordinary commit.
func DropRunning(ctx context.Context, campaignRoot, selector string) ([]Job, []WorkerStop, error) {
	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}

	matches, err := runningMatches(campaignRoot, selector)
	if err != nil {
		return nil, nil, err
	}

	var (
		dropped []Job
		stops   []WorkerStop
		stopped = map[string]bool{}
	)
	for _, e := range matches {
		path := runningJobPath(campaignRoot, e.Lane, e.Seq)
		if err := os.Remove(path); err != nil {
			if os.IsNotExist(err) {
				// The worker finished it in the moment between the listing and
				// this call. There is nothing to drop and no reason to stop a
				// worker over a job that is already committed.
				continue
			}
			return dropped, stops, camperrors.Wrapf(err, "drop running job %s", e.ID)
		}
		dropped = append(dropped, e.Job)

		// One stop per lane. A worker serves one job per lane at a time, so
		// two matching jobs in the same lane mean one is already abandoned,
		// and signalling twice would only mean waiting out the grace twice.
		if stopped[e.Lane] {
			continue
		}
		stopped[e.Lane] = true
		stop, err := stopLaneWorker(campaignRoot, e.Lane)
		if err != nil {
			return dropped, stops, err
		}
		stops = append(stops, stop)
	}
	return dropped, stops, nil
}

// RunningMatch returns the running job a selector names, if there is one.
//
// It exists so a refusal can be specific. "No failed job with that id" is true
// and useless when the job is sitting in running/; what the user needs is to be
// told which state it is actually in and the flag that acts on it.
func RunningMatch(campaignRoot, selector string) (Entry, bool) {
	matches, err := runningMatches(campaignRoot, selector)
	if err != nil || len(matches) == 0 {
		return Entry{}, false
	}
	return matches[0], true
}

// runningMatches returns the running jobs a selector names, in execution order.
func runningMatches(campaignRoot, selector string) ([]Entry, error) {
	slugs, err := Lanes(campaignRoot, stateRunning)
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, slug := range slugs {
		lane := RepoFromLaneSlug(slug)
		list, err := List(campaignRoot, stateRunning, lane)
		if err != nil {
			return nil, err
		}
		for _, job := range list {
			if selector != SelectorAll && job.ID != selector {
				continue
			}
			out = append(out, Entry{Job: job, State: stateRunning, Lane: lane})
		}
	}
	return out, nil
}

// stopLaneWorker ends the worker holding a lane, politely first.
//
// SIGTERM, then a grace period, then SIGKILL. The grace is not politeness for
// its own sake: camp's own signal handling turns SIGTERM into a cancelled
// context, and the worker spends that window killing the message writer's
// process group and returning its other jobs to pending. A worker killed
// outright skips all of it, which is why SIGKILL is the escalation and never
// the opening move.
//
// A variable so tests can drive the sequence without real processes;
// terminateLaneWorker is always what runs in production.
var stopLaneWorker = terminateLaneWorker

func terminateLaneWorker(campaignRoot, lane string) (WorkerStop, error) {
	stop := WorkerStop{Lane: lane}

	queueDir := QueueDir(campaignRoot)
	slug := LaneSlug(lane)
	pid, ok := laneWorkerPID(queueDir, slug)
	if !ok || !processAlive(pid) {
		// Nobody home. The job is already dropped, and the stale lock is left
		// where it is: acquireLane steals one this old on sight, so removing
		// it here would only race a worker starting up.
		return stop, nil
	}
	stop.PID = pid

	if err := signalWorker(pid, false); err != nil {
		return stop, camperrors.Wrapf(err, "stop the worker for lane %s (pid %d)", lane, pid)
	}
	if !waitForExit(pid, workerStopGrace) {
		if err := signalWorker(pid, true); err != nil {
			return stop, camperrors.Wrapf(err, "kill the worker for lane %s (pid %d)", lane, pid)
		}
		stop.Killed = true
		_ = waitForExit(pid, workerStopGrace)
	}

	// A worker that shut down cleanly has already released its locks. One that
	// was killed has not, and every lane it held stays unservable until the
	// lock goes stale: SpawnIfNeeded reads a lock that recent as a live worker
	// and declines to start one, so the jobs queued behind the dropped one
	// wait out the liveness window with nothing coming for them.
	//
	// Every lane, not only this one, because a worker serves as many lanes as
	// it can and the other lanes' jobs did nothing wrong. Only after the
	// process is confirmed gone: a lock belonging to a process that is still
	// alive is a lock, not litter.
	if !processAlive(pid) {
		releaseLocksHeldBy(queueDir, pid)
	}
	return stop, nil
}

// releaseLocksHeldBy removes the lane locks a stopped worker left behind.
//
// Matched by the pid the lock records rather than by name, so a lane another
// worker has taken in the meantime keeps its own lock: the file says who owns
// it, and that is the only safe way to tell a dead worker's leftovers from a
// live worker's claim.
func releaseLocksHeldBy(queueDir string, pid int) {
	entries, err := os.ReadDir(queueDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !isLaneLockName(e.Name()) {
			continue
		}
		path := filepath.Join(queueDir, e.Name())
		if holder, ok := pidFromLockFile(path); ok && holder == pid {
			_ = os.Remove(path)
		}
	}
}

// waitForExit reports whether a process was gone before the deadline.
func waitForExit(pid int, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for {
		if !processAlive(pid) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(workerStopPoll)
	}
}

// laneWorkerPID reads the pid a lane lock records.
//
// The lock has carried its holder's pid since it was written, for exactly this
// moment: a lane's lock is the only place the queue records which process is
// serving it. Anything unparseable answers "unknown", and an unknown worker is
// left alone rather than guessed at, because the guess would be a signal sent
// to whatever else happens to hold that number.
func laneWorkerPID(queueDir, slug string) (int, bool) {
	return pidFromLockFile(filepath.Join(queueDir, laneLockName(slug)))
}

func pidFromLockFile(path string) (int, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}
