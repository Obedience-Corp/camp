package jobs

import (
	"context"
	"os"
	"time"

	"github.com/Obedience-Corp/camp/internal/autowrite"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// What a deferred commit does when the message writer is not there at all.
//
// fallback.go settled that a commit must never be lost to a writer that cannot
// write it, and that is still the rule. This file is about the one case that
// rule was answering too quickly. "The writer ran and could not produce a
// message" and "the writer's daemon is down" arrive at the same place in the
// code and mean opposite things: the first is a verdict, the second is an
// outage, and landing a filler subject within seconds of an outage throws away
// the real message for nothing. The machine crashed on 2026-09-14, the daemon
// did not come back, and a deferred commit landed as "Update 4 files (writer
// unavailable)" seconds later, while nobody was waiting on anything it was
// holding up.
//
// So an unavailable writer (autowrite.ExitWriterUnavailable) buys the job a
// wait rather than a message. Three properties keep the wait honest:
//
//   - It is bounded. hooks.commit_message.retry_window is a deadline recorded
//     on the job, and at the deadline the old behavior happens exactly as
//     before.
//   - It costs nobody. The moment any camp command blocks on this lane, the
//     wait ends and the commit lands (see wanted.go).
//   - It is not a failure. The job goes back to pending, not to failed/, and
//     it does not spend the crash-recovery attempt budget, because a daemon
//     that is down says nothing about whether this job can run.

// writerBackoffMin and writerBackoffMax bound the gap between attempts.
//
// Thirty seconds because the cheapest outcome is the writer coming back on its
// own and nothing is waiting on the answer; five minutes because a window is
// an hour and an outage that lasts an hour deserves to be asked about twelve
// times rather than a hundred and twenty. Variables so tests drive the
// schedule instead of sleeping through it.
var (
	writerBackoffMin = 30 * time.Second
	writerBackoffMax = 5 * time.Minute
)

// writerWaitError reports that the writer said it was temporarily unavailable
// and this job should be asked again rather than described by camp.
//
// It is not a job failure and the worker must never park it: every field here
// is what the queue writes back onto the pending job so the next attempt knows
// what the last one found.
type writerWaitError struct {
	// RetryAt is the earliest the next attempt may start.
	RetryAt time.Time
	// FallbackAt is when camp stops waiting and writes the message itself.
	FallbackAt time.Time
	// Since is when the first unavailable attempt happened.
	Since time.Time
	// Attempts counts the unavailable attempts including the one that just
	// happened.
	Attempts int
	// Err is the writer's own unavailability error.
	Err error
}

func (e *writerWaitError) Error() string {
	return "the commit message writer is unavailable; retrying at " +
		e.RetryAt.Local().Format("15:04:05")
}

func (e *writerWaitError) Unwrap() error { return e.Err }

// writerWait decides whether a failed writer buys this job another attempt,
// and returns nil when the commit should be described and landed now.
//
// Everything except an unavailable writer answers nil, which is the whole of
// the old behavior: a writer that ran and failed, one that timed out, one that
// printed nothing, and a command that is not installed all land a commit
// immediately, as they did before this file existed.
func writerWait(ctx context.Context, campaignRoot string, job *Job, writerErr error, now time.Time) *writerWaitError {
	if !autowrite.WriterUnavailable(writerErr) {
		return nil
	}
	// Somebody's terminal is already blocked on this lane. Waiting would be
	// spending their time on a better commit message, which is not a trade
	// camp gets to make on their behalf.
	if laneWanted(QueueDir(campaignRoot), LaneSlug(job.Repo)) {
		return nil
	}
	return writerWaitFor(job, writerRetryWindow(ctx, campaignRoot), writerErr, now)
}

// writerWaitFor is writerWait's decision, given the window rather than the
// campaign it comes from.
//
// Separated so the policy — when a wait starts, how long the gap is, when the
// deadline ends it — is testable as arithmetic. Everything it decides ends up
// in a commit message or in how long a user's work sits unlanded, and neither
// is a thing to leave asserted only by a test that needs a daemon to be down.
func writerWaitFor(job *Job, window time.Duration, writerErr error, now time.Time) *writerWaitError {
	if window <= 0 {
		return nil
	}

	since, ok := jobTime(job.WriterUnavailableSince)
	if !ok {
		since = now
	}
	fallbackAt, ok := jobTime(job.WriterFallbackAt)
	if !ok {
		fallbackAt = since.Add(window)
	}
	if !now.Before(fallbackAt) {
		return nil // the window is spent; describe the commit and land it
	}

	attempts := job.WriterAttempts + 1
	retryAt := now.Add(writerBackoff(attempts))
	// Never past the deadline. Waking exactly on it is what gives the writer
	// one last chance to answer before camp writes the message itself.
	if retryAt.After(fallbackAt) {
		retryAt = fallbackAt
	}
	return &writerWaitError{
		RetryAt:    retryAt,
		FallbackAt: fallbackAt,
		Since:      since,
		Attempts:   attempts,
		Err:        writerErr,
	}
}

// writerRetryWindow reads how long an unavailable writer is waited for.
//
// Best effort, and zero (land now) is the answer to every problem reading it.
// A campaign whose config cannot be parsed still owes the user their commit,
// and the run path has already been through LoadCommitMessageHook by the time
// this is called, so a config that is broken enough to matter has already been
// reported through the writer's own failure.
func writerRetryWindow(ctx context.Context, campaignRoot string) time.Duration {
	hook, err := autowrite.LoadCommitMessageHook(ctx, campaignRoot)
	if err != nil {
		return 0
	}
	return hook.RetryWindow
}

// writerBackoff returns the gap before the nth unavailable attempt is retried,
// counting from one.
func writerBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := writerBackoffMin
	for range attempt - 1 {
		if d >= writerBackoffMax {
			break
		}
		d *= 2
	}
	return min(d, writerBackoffMax)
}

// jobTime reads a timestamp a job document carries, and whether it is there at
// all. Unreadable reads as absent: every caller has a correct answer for a
// field that was never written, and none of them has one for a nonsense time.
func jobTime(raw string) (time.Time, bool) {
	if raw == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(createdAtLayout, raw)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// formatJobTime renders a timestamp in the layout every job document uses.
func formatJobTime(t time.Time) string {
	return t.UTC().Format(createdAtLayout)
}

// claimableAt reports when a pending job may be claimed, and whether that is
// still in the future as of now.
//
// The wait is bounded twice over, because a queue file is plain JSON that
// anything can edit and a machine's clock can jump. Neither may be able to
// park a commit indefinitely: a worker honoring a hand-written "not before
// 2100" would hold its lane and everything queued behind it forever, which is
// a worse failure than any this file exists to fix. So the wait never outlasts
// the job's own fallback deadline, and never exceeds one backoff regardless.
func claimableAt(job *Job, now time.Time) (time.Time, bool) {
	until, ok := jobTime(job.NotBefore)
	if !ok || !now.Before(until) {
		return time.Time{}, false
	}
	if deadline, ok := jobTime(job.WriterFallbackAt); ok && until.After(deadline) {
		until = deadline
	}
	if longest := now.Add(writerBackoffMax); until.After(longest) {
		until = longest
	}
	if !now.Before(until) {
		return time.Time{}, false
	}
	return until, true
}

// waitingForWriter reports whether a job is waiting out an unavailable writer
// right now, and how long until it is asked again.
//
// The listing surfaces ask it, and they ask about a job document rather than
// about the worker, because the wait outlives any one worker: the job carries
// its whole state, so a listing is correct even when nothing is running.
//
// A job whose next attempt is already due is not waiting, whatever it did
// earlier. That is the difference between the row a person can do nothing
// about and the one already in progress: a retry that is running the writer
// this second would otherwise be described as sitting still.
func waitingForWriter(job *Job, now time.Time) (retryIn time.Duration, fallbackAt time.Time, ok bool) {
	if job.WriterAttempts <= 0 {
		return 0, time.Time{}, false
	}
	deadline, hasDeadline := jobTime(job.WriterFallbackAt)
	if !hasDeadline {
		return 0, time.Time{}, false
	}
	until, waiting := claimableAt(job, now)
	if !waiting {
		return 0, time.Time{}, false
	}
	return until.Sub(now), deadline, true
}

// writerWaitPoll is how often a sleeping worker looks up from its wait.
//
// It is not a poll of the queue: the wake time is already known. It is how
// quickly the worker can notice that something has blocked on its lane, and it
// is a second because that is under any drain's patience — a lane that a user
// is waiting on has to come free while they are still watching, not after the
// backoff they never asked for.
var writerWaitPoll = time.Second

// waitForClaim sleeps until the lane's next job is claimable, and reports
// whether the worker should carry on.
//
// It returns false only for cancellation, which is the one thing that ends the
// wait without the job being tried again. Everything else about the wait is a
// reason to wake early: the deadline arriving, or a command blocking on this
// lane, both of which end with this job being claimed and landed.
//
// The lane lock is held throughout, deliberately. A worker that let go while
// it waited would look abandoned, every enqueuer would spawn a replacement
// that did nothing but wait too, and the guarantee that the commit lands
// without anyone running camp again would go with it.
func waitForClaim(ctx context.Context, campaignRoot, repo string, until time.Time) bool {
	queueDir := QueueDir(campaignRoot)
	slug := LaneSlug(repo)
	for {
		remaining := time.Until(until)
		if remaining <= 0 {
			return true
		}
		if laneWanted(queueDir, slug) {
			logWorker(campaignRoot, "writer-wait-cut-short lane=%s reason=wanted", repo)
			return true
		}
		timer := time.NewTimer(min(remaining, writerWaitPoll))
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
	}
}

// deferForWriter returns a running job to pending to wait for the writer.
//
// It is the one transition in this queue that is not about failure or
// progress, so it counts nothing: Attempts is the crash-recovery budget and
// stays where it was, and the job leaves running/ carrying only what the next
// attempt needs to know.
//
// The write-then-unlink ordering is requeueJobFile's, for requeueJobFile's
// reason: a crash between them duplicates a job, and the reverse loses one.
func deferForWriter(campaignRoot, repo string, job *Job, wait *writerWaitError) error {
	pendingDir := laneDir(campaignRoot, statePending, repo)
	if err := os.MkdirAll(pendingDir, 0o755); err != nil {
		return camperrors.Wrapf(err, "create pending lane %s", pendingDir)
	}
	name := jobFilename(job.Seq)
	runningPath := runningJobPath(campaignRoot, repo, job.Seq)
	if !moveToPending(pendingDir, runningPath, name, func(j *Job) {
		j.NotBefore = formatJobTime(wait.RetryAt)
		j.WriterUnavailableSince = formatJobTime(wait.Since)
		j.WriterFallbackAt = formatJobTime(wait.FallbackAt)
		j.WriterAttempts = wait.Attempts
	}) {
		return camperrors.Newf("could not return job %s to pending to wait for the writer", job.ID)
	}
	return nil
}
