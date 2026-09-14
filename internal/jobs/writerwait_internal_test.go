package jobs

import (
	"strings"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/autowrite"
)

// The wait policy decides how long a user's captured work sits unlanded and
// what the commit says when it finally does. Both are permanent, and neither
// is observable from a test that only asserts a job moved directories, so the
// arithmetic is asserted here directly.

// unavailable is the error a writer's exit 75 produces.
func unavailable() error {
	return &autowrite.WriterError{
		Command:     "ob commit",
		Reason:      "ob: daemon not running",
		Unavailable: true,
		Err:         errFake,
	}
}

var errFake = &fakeErr{}

type fakeErr struct{}

func (*fakeErr) Error() string { return "exit status 75" }

func TestWriterWaitForStartsTheWindowOnTheFirstUnavailableAttempt(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	job := &Job{ID: "job-1", Repo: "."}

	wait := writerWaitFor(job, time.Hour, unavailable(), now)
	if wait == nil {
		t.Fatal("writerWaitFor() = nil; an unavailable writer inside the window must be waited for")
	}
	if wait.Attempts != 1 {
		t.Errorf("Attempts = %d, want 1", wait.Attempts)
	}
	if !wait.Since.Equal(now) {
		t.Errorf("Since = %v, want the moment the writer first went missing (%v)", wait.Since, now)
	}
	if want := now.Add(time.Hour); !wait.FallbackAt.Equal(want) {
		t.Errorf("FallbackAt = %v, want %v", wait.FallbackAt, want)
	}
	if want := now.Add(writerBackoffMin); !wait.RetryAt.Equal(want) {
		t.Errorf("RetryAt = %v, want the first backoff (%v)", wait.RetryAt, want)
	}
}

// The deadline is the job's, not the config's. A job already waiting keeps the
// deadline it started with, so a listing that promised "falls back at 12:34"
// cannot be contradicted by the worker a minute later.
func TestWriterWaitForKeepsTheRecordedDeadline(t *testing.T) {
	start := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	now := start.Add(2 * time.Minute)
	job := &Job{
		ID:                     "job-1",
		WriterUnavailableSince: formatJobTime(start),
		WriterFallbackAt:       formatJobTime(start.Add(time.Hour)),
		WriterAttempts:         2,
	}

	wait := writerWaitFor(job, 10*time.Minute, unavailable(), now)
	if wait == nil {
		t.Fatal("writerWaitFor() = nil, want a continued wait")
	}
	if want := start.Add(time.Hour); !wait.FallbackAt.Equal(want) {
		t.Errorf("FallbackAt = %v, want the deadline the job already carried (%v)", wait.FallbackAt, want)
	}
	if !wait.Since.Equal(start) {
		t.Errorf("Since = %v, want %v", wait.Since, start)
	}
	if wait.Attempts != 3 {
		t.Errorf("Attempts = %d, want the job's count plus this attempt", wait.Attempts)
	}
}

func TestWriterWaitForEndsAtTheDeadline(t *testing.T) {
	start := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	job := &Job{
		WriterUnavailableSince: formatJobTime(start),
		WriterFallbackAt:       formatJobTime(start.Add(time.Hour)),
		WriterAttempts:         6,
	}

	if wait := writerWaitFor(job, time.Hour, unavailable(), start.Add(time.Hour)); wait != nil {
		t.Fatalf("writerWaitFor() = %+v at the deadline; the commit must land instead", wait)
	}
	if wait := writerWaitFor(job, time.Hour, unavailable(), start.Add(2*time.Hour)); wait != nil {
		t.Fatalf("writerWaitFor() = %+v past the deadline; the commit must land instead", wait)
	}
}

// retry_window: 0 is how a user asks for the behavior camp had before any of
// this existed, so it has to mean exactly that and not "a very short wait".
func TestWriterWaitForZeroWindowNeverWaits(t *testing.T) {
	now := time.Now()
	if wait := writerWaitFor(&Job{}, 0, unavailable(), now); wait != nil {
		t.Fatalf("writerWaitFor(window=0) = %+v, want the commit to land immediately", wait)
	}
}

// The last attempt lands on the deadline rather than past it: that attempt is
// the writer's final chance to answer before camp writes the message itself,
// and a backoff that overshot would throw it away.
func TestWriterWaitForNeverRetriesPastTheDeadline(t *testing.T) {
	start := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	deadline := start.Add(5 * time.Second)
	job := &Job{
		WriterUnavailableSince: formatJobTime(start),
		WriterFallbackAt:       formatJobTime(deadline),
	}

	wait := writerWaitFor(job, time.Hour, unavailable(), start)
	if wait == nil {
		t.Fatal("writerWaitFor() = nil inside the window")
	}
	if !wait.RetryAt.Equal(deadline) {
		t.Errorf("RetryAt = %v, want it clamped to the deadline (%v)", wait.RetryAt, deadline)
	}
}

// Everything that is not the exit-75 contract keeps the old behavior, which is
// the promise fallback.go makes: the commit lands now.
func TestWriterWaitIgnoresOtherFailures(t *testing.T) {
	now := time.Now()
	for name, err := range map[string]error{
		"ordinary failure": &autowrite.WriterError{Command: "ob commit", Reason: "boom"},
		"empty output":     autowrite.ErrCommitMessageHookEmptyOutput,
		"timeout":          &autowrite.TimeoutError{Command: "ob commit", Timeout: time.Minute},
	} {
		t.Run(name, func(t *testing.T) {
			if wait := writerWait(t.Context(), "", &Job{Repo: "."}, err, now); wait != nil {
				t.Fatalf("writerWait(%s) = %+v, want no wait", name, wait)
			}
		})
	}
}

// Doubling from 30s to a 5m ceiling: tight enough that a writer coming back is
// noticed quickly, bounded so an hour-long outage is a dozen attempts rather
// than a hundred.
func TestWriterBackoffSchedule(t *testing.T) {
	want := []time.Duration{
		30 * time.Second,
		time.Minute,
		2 * time.Minute,
		4 * time.Minute,
		5 * time.Minute,
		5 * time.Minute,
	}
	for i, expected := range want {
		attempt := i + 1
		if got := writerBackoff(attempt); got != expected {
			t.Errorf("writerBackoff(%d) = %v, want %v", attempt, got, expected)
		}
	}
	if got := writerBackoff(1000); got != writerBackoffMax {
		t.Errorf("writerBackoff(1000) = %v, want the cap %v", got, writerBackoffMax)
	}
	if got := writerBackoff(0); got != writerBackoffMin {
		t.Errorf("writerBackoff(0) = %v, want the first gap %v", got, writerBackoffMin)
	}
}

// A job with no NotBefore is claimable, which is every job the queue has ever
// written except one waiting for a writer.
func TestClaimableAt(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	if _, waiting := claimableAt(&Job{}, now); waiting {
		t.Error("claimableAt(no NotBefore) reported a wait")
	}
	if _, waiting := claimableAt(&Job{NotBefore: "not a time"}, now); waiting {
		t.Error("claimableAt(unreadable NotBefore) reported a wait; an unreadable job must still run")
	}
	if _, waiting := claimableAt(&Job{NotBefore: formatJobTime(now.Add(-time.Second))}, now); waiting {
		t.Error("claimableAt(past) reported a wait")
	}
	until, waiting := claimableAt(&Job{NotBefore: formatJobTime(now.Add(time.Minute))}, now)
	if !waiting {
		t.Fatal("claimableAt(future) reported no wait")
	}
	if want := now.Add(time.Minute); !until.Equal(want) {
		t.Errorf("claimableAt = %v, want %v", until, want)
	}
}

// What a row says while a commit waits. A listing that showed "pending" for an
// hour with no explanation is the failure this replaces.
func TestWriterWaitNote(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 30, 0, 0, time.UTC)
	fallbackAt := now.Add(40 * time.Minute)
	job := Job{
		WriterAttempts:         2,
		WriterUnavailableSince: formatJobTime(now.Add(-20 * time.Minute)),
		WriterFallbackAt:       formatJobTime(fallbackAt),
		NotBefore:              formatJobTime(now.Add(2 * time.Minute)),
	}

	note := writerWaitNote(&job, now)
	for _, want := range []string{
		"waiting for the message writer",
		"attempt 3",
		"retrying in 2m",
		"falls back at " + fallbackAt.Local().Format("15:04"),
	} {
		if !strings.Contains(note, want) {
			t.Errorf("writerWaitNote = %q, want it to contain %q", note, want)
		}
	}
	// AttemptNote is where every surface reads it from, so the routing is part
	// of the claim: a waiting job must not fall through to the attempt count.
	// It reads the wall clock, so the job it is asked about has to be anchored
	// there rather than on the fixed instant above.
	live := Job{
		Attempts:               1,
		WriterAttempts:         2,
		WriterUnavailableSince: formatJobTime(time.Now().Add(-time.Minute)),
		WriterFallbackAt:       formatJobTime(time.Now().Add(time.Hour)),
		NotBefore:              formatJobTime(time.Now().Add(2 * time.Minute)),
	}
	if got := AttemptNote(live, false); !strings.Contains(got, "waiting for the message writer") {
		t.Errorf("AttemptNote = %q, want the writer wait", got)
	}

	// The writer wait must not borrow the crash-recovery vocabulary: this job
	// has spent none of that budget.
	if strings.Contains(note, "of 3") {
		t.Errorf("writerWaitNote = %q, want no crash-budget count on a waiting job", note)
	}

	// A job whose next attempt is due is not waiting. Its worker is running
	// the writer, and a row calling that a wait describes the opposite of what
	// is happening.
	job.NotBefore = formatJobTime(now.Add(-time.Second))
	if note := writerWaitNote(&job, now); note != "" {
		t.Errorf("writerWaitNote = %q for a job that is due, want nothing", note)
	}
}

// A parked job is history, so it reports how it failed rather than a wait that
// is over.
func TestWriterWaitNoteYieldsToFailure(t *testing.T) {
	job := Job{
		Attempts:               1,
		WriterAttempts:         4,
		WriterUnavailableSince: formatJobTime(time.Now()),
		WriterFallbackAt:       formatJobTime(time.Now().Add(time.Hour)),
	}
	if note := AttemptNote(job, true); note != "failed after 1 attempt" {
		t.Errorf("AttemptNote(failed) = %q, want the failure count", note)
	}
}

// The commit body is the only account of the wait that outlives the cache
// directory, so it has to carry both halves: how long camp held on, and how
// many times it asked.
func TestFallbackBodyReportsTheWait(t *testing.T) {
	body := fallbackBody("ob: daemon not running",
		&writerWaited{For: time.Hour, Attempts: 7})

	for _, want := range []string{
		"camp wrote this message itself",
		"the writer was unreachable for 1h0m0s across 7 attempts",
		"ob: daemon not running",
		"git commit --amend",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("fallbackBody = %q, want it to contain %q", body, want)
		}
	}

	if got := fallbackBody("boom", &writerWaited{For: 30 * time.Second, Attempts: 1}); !strings.Contains(got,
		"unreachable for 30s across 1 attempt.") {
		t.Errorf("fallbackBody = %q, want a singular attempt", got)
	}

	// A writer that simply failed never waited, and a body claiming otherwise
	// would send the user looking for an outage that did not happen.
	if got := fallbackBody("boom", nil); strings.Contains(got, "camp waited") {
		t.Errorf("fallbackBody = %q, want no wait line when there was no wait", got)
	}
}

// The attempt that lands the commit counts only if it too found the writer
// missing: a writer that came back broken ended the outage.
func TestWriterWaitSummary(t *testing.T) {
	start := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	now := start.Add(90 * time.Second)
	job := &Job{WriterUnavailableSince: formatJobTime(start), WriterAttempts: 3}

	got := writerWaitSummary(job, unavailable(), now)
	if got == nil {
		t.Fatal("writerWaitSummary() = nil, want the wait this job carried")
	}
	if got.For != 90*time.Second || got.Attempts != 4 {
		t.Errorf("writerWaitSummary() = %+v, want 90s across 4 attempts", got)
	}

	got = writerWaitSummary(job, &autowrite.WriterError{Reason: "boom"}, now)
	if got == nil || got.Attempts != 3 {
		t.Errorf("writerWaitSummary() = %+v, want the unavailable attempts only", got)
	}

	if got := writerWaitSummary(&Job{}, unavailable(), now); got != nil {
		t.Errorf("writerWaitSummary(no wait) = %+v, want nil", got)
	}
}

// A queue file is hand-editable and a clock can jump, and either could
// otherwise park a commit for years behind a lane nobody can take.
func TestClaimableAtBoundsAnAbsurdWait(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

	far := &Job{NotBefore: formatJobTime(now.AddDate(50, 0, 0))}
	until, waiting := claimableAt(far, now)
	if !waiting {
		t.Fatal("claimableAt(far future) reported no wait")
	}
	if want := now.Add(writerBackoffMax); !until.Equal(want) {
		t.Errorf("claimableAt = %v, want it bounded to one backoff (%v)", until, want)
	}

	// The job's own deadline binds first: past it the commit is owed, whatever
	// the next attempt was scheduled for.
	deadline := now.Add(20 * time.Second)
	bounded := &Job{
		NotBefore:        formatJobTime(now.Add(time.Minute)),
		WriterFallbackAt: formatJobTime(deadline),
	}
	until, waiting = claimableAt(bounded, now)
	if !waiting || !until.Equal(deadline) {
		t.Errorf("claimableAt = %v (waiting=%v), want the fallback deadline %v", until, waiting, deadline)
	}

	// A deadline already behind us makes the job due now rather than later.
	if _, waiting := claimableAt(&Job{
		NotBefore:        formatJobTime(now.Add(time.Minute)),
		WriterFallbackAt: formatJobTime(now.Add(-time.Second)),
	}, now); waiting {
		t.Error("claimableAt reported a wait past the job's own fallback deadline")
	}
}
