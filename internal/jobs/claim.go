package jobs

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// What happens to a job file once a worker takes it.
//
// Every transition here is one atomic filesystem operation, and the choice of
// which one is the crash-safety story: claim is a rename, completion is an
// unlink, failure is a rename, and returning a job to pending is a write
// followed by an unlink. A process dying between any two of them leaves a
// state the next worker reads correctly, and where a crash can duplicate work
// or lose it, these are all arranged to duplicate. A job that runs twice is
// recoverable; a job that never runs is the promise camp broke.

// Claim moves the lowest-numbered pending job in a lane to running and returns
// it with a completion func.
//
// The rename is the whole election mechanism: it is atomic on POSIX, so two
// racing claimants cannot both take the same job. The loser sees ENOENT and
// moves to the next file rather than failing, because a claim race is normal
// operation, not an error.
//
// The returned func must be called exactly once. Passing nil completes the job
// (unlink); passing an error fails it (rename to failed/), preserving the file
// for inspection.
func Claim(ctx context.Context, campaignRoot, repo string) (*Job, func(error) error, error) {
	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}

	pendingDir := laneDir(campaignRoot, statePending, repo)
	names, err := sortedJobFiles(pendingDir)
	if err != nil {
		return nil, nil, err
	}

	runningDir := laneDir(campaignRoot, stateRunning, repo)
	if len(names) > 0 {
		if err := os.MkdirAll(runningDir, 0o755); err != nil {
			return nil, nil, camperrors.Wrapf(err, "create running lane %s", runningDir)
		}
	}

	for _, name := range names {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		src := filepath.Join(pendingDir, name)
		dst := filepath.Join(runningDir, name)

		if err := os.Rename(src, dst); err != nil {
			if os.IsNotExist(err) {
				continue // another claimant took it; try the next
			}
			return nil, nil, camperrors.Wrapf(err, "claim job %s", src)
		}
		markAttemptStart(dst)

		job, err := readJob(dst)
		if err == nil {
			// Re-validated at the moment of use, not only at enqueue. A queue
			// file is plain JSON on disk and nothing stops it being edited, so
			// the invariants are only real if they are checked again here. The
			// one that matters most is the explicit-paths rule: an edited
			// `paths: ["."]` would make the worker stage everything present
			// when it runs, which is exactly the sweep the rule prevents.
			//
			// Checked here rather than in readJob because List uses that, and
			// a job that fails validation must still appear in `camp jobs` and
			// still hold a drain. Hiding it from the listing would leave it
			// sitting in pending forever with nothing able to report it.
			err = job.Validate()
		}
		if err != nil {
			// Unreadable or invalid, so it can never execute. Move it to
			// failed rather than leaving it claimed forever, and say so.
			_ = failJobFile(campaignRoot, repo, dst, name)
			return nil, nil, camperrors.Wrapf(err, "job %s cannot run", name)
		}
		return job, completionFor(campaignRoot, repo, dst, name), nil
	}
	return nil, nil, nil
}

// completionFor builds the completion closure for a claimed job.
func completionFor(campaignRoot, repo, runningPath, name string) func(error) error {
	return func(jobErr error) error {
		if jobErr != nil {
			return parkFailedJob(campaignRoot, repo, runningPath, name, jobErr)
		}
		if err := os.Remove(runningPath); err != nil && !os.IsNotExist(err) {
			return camperrors.Wrapf(err, "complete job %s", runningPath)
		}
		return nil
	}
}

// parkFailedJob records the run and moves the job to failed/.
//
// An attempt is a job that started and did not finish, and until now only one
// of the two ways that happens was counted. Reclaim counted the worker that
// died; a job that ran and returned an error counted nothing, so it was parked
// still reading Attempts: 0 while the listing beside it said the job had
// already been tried and given up on. The two disagreed in the same output,
// and an agent reading --json could not tell a failure that had been retried
// from one that never was.
//
// The incremented copy is written to the destination and the source removed
// after, never edited in place. Reclaim skips a job it cannot parse, so an
// interrupted in-place write would leave unreadable JSON in running/ that
// nothing ever picks up again: the one failure this queue treats as
// unforgivable. Written this way a crash in the window leaves the job in both
// lanes instead, and reclaim resolves that by running it again. Duplicating a
// job is recoverable; stranding one is not.
//
// A job that cannot be read or marshalled is still parked, without a count.
// It has to leave running/ either way, and a wrong number in a listing costs
// less than a job nobody is coming for.
func parkFailedJob(campaignRoot, repo, runningPath, name string, jobErr error) error {
	job, err := readJob(runningPath)
	if err != nil {
		return failJobFile(campaignRoot, repo, runningPath, name)
	}
	job.Attempts++
	job.LastError = failureReason(jobErr)
	data, err := json.MarshalIndent(job, "", "  ")
	if err != nil {
		return failJobFile(campaignRoot, repo, runningPath, name)
	}

	failedDir := laneDir(campaignRoot, stateFailed, repo)
	if err := os.MkdirAll(failedDir, 0o755); err != nil {
		return camperrors.Wrapf(err, "create failed lane %s", failedDir)
	}
	if err := os.WriteFile(filepath.Join(failedDir, name), data, 0o644); err != nil {
		return camperrors.Wrapf(err, "park job %s", runningPath)
	}
	if err := os.Remove(runningPath); err != nil && !os.IsNotExist(err) {
		return camperrors.Wrapf(err, "park job %s", runningPath)
	}
	return nil
}

// failureReason renders why an attempt failed, for the parked job to carry.
//
// Bounded and flattened to one line because it is rendered in a table row and
// stored in a document a human reads. Newlines would break the row; an
// unbounded reason would bury every other column under a writer's usage text.
func failureReason(jobErr error) string {
	if jobErr == nil {
		return ""
	}
	reason := strings.Join(strings.Fields(jobErr.Error()), " ")
	if len(reason) > maxJobErrorBytes {
		reason = reason[:maxJobErrorBytes] + "..."
	}
	return reason
}

// failJobFile moves a running job to the failed lane, keeping it for
// inspection. A failed job is evidence, not garbage: it names work camp
// promised to do and did not.
func failJobFile(campaignRoot, repo, runningPath, name string) error {
	failedDir := laneDir(campaignRoot, stateFailed, repo)
	if err := os.MkdirAll(failedDir, 0o755); err != nil {
		return camperrors.Wrapf(err, "create failed lane %s", failedDir)
	}
	if err := os.Rename(runningPath, filepath.Join(failedDir, name)); err != nil {
		return camperrors.Wrapf(err, "fail job %s", runningPath)
	}
	return nil
}

// Reclaim returns running jobs older than olderThan to pending, incrementing
// attempts.
//
// This is crash recovery. A worker that dies mid-job leaves its file in
// running/ with nothing watching it, and without reclaim that job is stranded
// forever: the queue's one unforgivable failure.
//
// Age is the signal, and the age it reads is the age of the attempt: the file's
// time is set when the job is claimed. It is a second opinion rather than the
// decision. Callers already hold the lane, so every running job they find was
// abandoned by definition, and a lane lock outlives its worker by laneLiveness
// before it can be stolen at all: by the time anyone reclaims, the attempt is
// necessarily older than the same window.
func Reclaim(ctx context.Context, campaignRoot, repo string, olderThan time.Duration) (int, error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}

	runningDir := laneDir(campaignRoot, stateRunning, repo)
	names, err := sortedJobFiles(runningDir)
	if err != nil {
		return 0, err
	}
	if len(names) == 0 {
		return 0, nil
	}

	pendingDir := laneDir(campaignRoot, statePending, repo)
	if err := os.MkdirAll(pendingDir, 0o755); err != nil {
		return 0, camperrors.Wrapf(err, "create pending lane %s", pendingDir)
	}

	reclaimed := 0
	cutoff := time.Now().Add(-olderThan)
	for _, name := range names {
		path := filepath.Join(runningDir, name)
		info, statErr := os.Stat(path)
		if statErr != nil || info.ModTime().After(cutoff) {
			continue // gone, or still heartbeating
		}
		if requeueJobFile(pendingDir, path, name) {
			reclaimed++
		}
	}
	return reclaimed, nil
}

// RequeueRunning returns every running job in a lane to pending with the
// attempt counted, and reports how many moved.
//
// Called by a worker told to stop while it holds the lane. It consults no
// mtime, unlike Reclaim: the caller holds the lane lock, so every running job
// in the lane is either this worker's own or was abandoned by a worker that is
// already gone, and nobody still alive is going to finish either.
//
// Returning them to pending rather than leaving them for the next Reclaim is
// what keeps a shutdown off the user's critical path. SpawnIfNeeded starts a
// worker only for a lane with pending work, so a job left in running/ would
// block the next commit's drain for its whole timeout with nothing coming to
// serve it.
//
// It takes no context on purpose. This is the cleanup that runs precisely when
// the worker's context is already cancelled, so honoring one would decline to
// do the only thing standing between a cancelled job and the failed lane.
func RequeueRunning(campaignRoot, repo string) (int, error) {
	runningDir := laneDir(campaignRoot, stateRunning, repo)
	names, err := sortedJobFiles(runningDir)
	if err != nil {
		return 0, err
	}
	if len(names) == 0 {
		return 0, nil
	}

	pendingDir := laneDir(campaignRoot, statePending, repo)
	if err := os.MkdirAll(pendingDir, 0o755); err != nil {
		return 0, camperrors.Wrapf(err, "create pending lane %s", pendingDir)
	}

	requeued := 0
	for _, name := range names {
		if requeueJobFile(pendingDir, filepath.Join(runningDir, name), name) {
			requeued++
		}
	}
	return requeued, nil
}

// requeueJobFile moves one running job back to pending with its attempt
// counted, and reports whether it moved.
//
// The incremented copy is written to the destination and the source removed
// after, never edited in place. Reclaim skips a job it cannot parse, so an
// interrupted in-place write would leave unreadable JSON in running/ that
// nothing ever picks up again: the one failure this queue treats as
// unforgivable. Written this way a crash in the window leaves the job in both
// lanes instead, and reclaim resolves that by running it again. Duplicating a
// job is recoverable; stranding one is not.
//
// A job that cannot be read, marshalled, or written stays in running/ rather
// than being reported. Both callers are recovery paths with nowhere better to
// put a job they cannot parse, and leaving it is what lets the next reclaim try
// again.
func requeueJobFile(pendingDir, runningPath, name string) bool {
	job, err := readJob(runningPath)
	if err != nil {
		return false
	}
	job.Attempts++
	data, err := json.MarshalIndent(job, "", "  ")
	if err != nil {
		return false
	}
	if err := os.WriteFile(filepath.Join(pendingDir, name), data, 0o644); err != nil {
		return false
	}
	if err := os.Remove(runningPath); err != nil && !os.IsNotExist(err) {
		return false
	}
	return true
}
