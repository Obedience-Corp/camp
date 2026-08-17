package jobs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/autowrite"
)

// A writer that runs out of time parks its job, with the reason on the job.
//
// Parked rather than committed with a stand-in subject: camp does not invent a
// message, so a writer that never answered leaves a job the user can retry or
// drop, not a commit whose subject camp made up. Parked rather than requeued,
// because a timeout is a verdict on the work; the shutdown path is the one that
// declines to give a verdict, and the two must not be confused.
func TestWriterTimeoutParksTheJob(t *testing.T) {
	withFastTiming(t, time.Millisecond, time.Millisecond)
	root := testCampaign(t)
	ctx := context.Background()

	job, err := Enqueue(ctx, root, Job{
		Kind: KindCommitTree, Repo: ".", Tree: "t1", Parent: "p1", AutoWrite: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	// The gate before the writer needs a repository to answer; the writer is
	// the step under test, so it is the step allowed to fail.
	originalEmpty := isEmptyCommitTree
	isEmptyCommitTree = func(context.Context, string, *Job) (bool, error) { return false, nil }
	t.Cleanup(func() { isEmptyCommitTree = originalEmpty })

	timeout := &autowrite.TimeoutError{Command: "ob commit", Timeout: 5 * time.Minute}
	stubWriter(t, func(context.Context, string, string, *Job) (string, error) {
		return "", timeout
	})

	runLane(ctx, root, ".")

	if pending, err := List(root, statePending, "."); err != nil {
		t.Fatal(err)
	} else if len(pending) != 0 {
		t.Errorf("%d jobs went back to pending; a timed-out writer is a verdict on "+
			"the job, and requeueing it runs the same wedged writer again", len(pending))
	}

	failed, err := List(root, stateFailed, ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) != 1 || failed[0].ID != job.ID {
		t.Fatalf("failed lane = %+v, want the timed-out job %s", failed, job.ID)
	}
	if failed[0].Attempts != 1 {
		t.Errorf("Attempts = %d, want 1: a run that timed out is a run", failed[0].Attempts)
	}
	// The reason has to name both, because they are the two things the user
	// decides between: fix the writer, or raise the bound.
	for _, want := range []string{"ob commit", "5m0s"} {
		if !strings.Contains(failed[0].LastError, want) {
			t.Errorf("LastError = %q, want it to name %q", failed[0].LastError, want)
		}
	}
}

// A parked job's reason is one bounded line, because it is rendered in a table
// row and read by a person. A writer that fails by printing its own usage must
// not push every other column off the screen.
func TestParkedFailureReasonIsOneBoundedLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		err           error
		wantContains  string
		wantMaxLength int
	}{
		{
			name:          "a multi-line failure is flattened",
			err:           errString("first line\nsecond line"),
			wantContains:  "first line second line",
			wantMaxLength: maxJobErrorBytes + 3,
		},
		{
			name:          "an unbounded failure is truncated",
			err:           errString(strings.Repeat("x", maxJobErrorBytes*3)),
			wantContains:  "...",
			wantMaxLength: maxJobErrorBytes + 3,
		},
		{
			name:          "no error records no reason",
			err:           nil,
			wantContains:  "",
			wantMaxLength: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := failureReason(tt.err)
			if !strings.Contains(got, tt.wantContains) {
				t.Errorf("failureReason() = %q, want it to contain %q", got, tt.wantContains)
			}
			if len(got) > tt.wantMaxLength {
				t.Errorf("failureReason() length = %d, want at most %d", len(got), tt.wantMaxLength)
			}
			if strings.Contains(got, "\n") {
				t.Errorf("failureReason() = %q, want a single line", got)
			}
		})
	}
}

// Retrying clears the reason along with the count. A pending job carrying why
// its last attempt failed reports a failure that has not happened yet.
func TestRetryClearsTheRecordedReason(t *testing.T) {
	root := testCampaign(t)
	ctx := context.Background()

	if _, err := Enqueue(ctx, root, Job{
		Kind: KindCommitPaths, Repo: ".", Paths: []string{"a.md"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, complete, err := Claim(ctx, root, "."); err != nil {
		t.Fatal(err)
	} else if err := complete(errString("writer boom")); err != nil {
		t.Fatal(err)
	}

	if _, err := Retry(ctx, root, SelectorAll); err != nil {
		t.Fatal(err)
	}
	pending, err := List(root, statePending, ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending holds %d jobs, want 1", len(pending))
	}
	if pending[0].LastError != "" {
		t.Errorf("LastError = %q, want it cleared with the attempt count", pending[0].LastError)
	}
}

// What a running job's row says about it.
//
// The incident this exists for: a job ran for fifty minutes with a live worker
// and a heartbeating lane, and every listing described it as running, which is
// what a job three seconds in also looks like. The listing has to distinguish
// them, and it has to distinguish the two ways a running job goes wrong,
// because they take opposite commands to fix.
func TestStallStatus(t *testing.T) {
	t.Parallel()

	const budget = 5 * time.Minute
	autoWrite := Job{Kind: KindCommitTree, AutoWrite: true}
	paths := Job{Kind: KindCommitPaths}

	tests := []struct {
		name       string
		job        Job
		laneAlive  bool
		runningFor time.Duration
		known      bool
		budget     time.Duration
		wantStall  bool
		wantReason string
	}{
		{
			name:       "no live worker is a stall whatever the clock says",
			job:        autoWrite,
			laneAlive:  false,
			runningFor: time.Second,
			known:      true,
			budget:     budget,
			wantStall:  true,
			wantReason: reasonNoWorker,
		},
		{
			name:       "a worker past the budget names the writer and the budget",
			job:        autoWrite,
			laneAlive:  true,
			runningFor: 51 * time.Minute,
			known:      true,
			budget:     budget,
			wantStall:  true,
			wantReason: "writer running 51m, budget 5m",
		},
		{
			// Nothing about a commit-paths job runs a writer, so calling one
			// out would send the user to look at a tool that was never
			// involved.
			name:       "a non-writing job past the budget names the attempt",
			job:        paths,
			laneAlive:  true,
			runningFor: 51 * time.Minute,
			known:      true,
			budget:     budget,
			wantStall:  true,
			wantReason: "attempt running 51m, budget 5m",
		},
		{
			// The file went away between the listing and the stat, which means
			// the job finished. Guessing a duration would invent trouble out of
			// a completed commit.
			name:      "an unknown start is never a stall",
			job:       autoWrite,
			laneAlive: true,
			known:     false,
			budget:    budget,
			wantStall: false,
		},
		{
			name:       "inside the budget is not a stall",
			job:        autoWrite,
			laneAlive:  true,
			runningFor: 4 * time.Minute,
			known:      true,
			budget:     budget,
			wantStall:  false,
		},
		{
			name:       "exactly at the budget is not yet over it",
			job:        autoWrite,
			laneAlive:  true,
			runningFor: budget,
			known:      true,
			budget:     budget,
			wantStall:  false,
		},
		{
			name:       "a budget of zero disables the clock, not the worker check",
			job:        autoWrite,
			laneAlive:  true,
			runningFor: 51 * time.Minute,
			known:      true,
			budget:     0,
			wantStall:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stalled, reason := stallStatus(tt.job, tt.laneAlive, tt.runningFor, tt.known, tt.budget)
			if stalled != tt.wantStall {
				t.Fatalf("stalled = %v, want %v (reason %q)", stalled, tt.wantStall, reason)
			}
			if reason != tt.wantReason {
				t.Fatalf("reason = %q, want %q", reason, tt.wantReason)
			}
		})
	}
}

// The listing reads the attempt's own clock, not the job's age.
//
// A queue that is behind can hold a job for hours before anyone claims it, and
// reporting that wait as "running" would call every backlog a stall. The number
// that matters is how long the thing in front of you has been in progress.
func TestSnapshotReportsTheAttemptClockNotTheAge(t *testing.T) {
	root := testCampaign(t)
	ctx := context.Background()

	if _, err := Enqueue(ctx, root, Job{
		Kind: KindCommitPaths, Repo: ".", Paths: []string{"a.md"},
	}); err != nil {
		t.Fatal(err)
	}
	// An enqueue that happened long ago: the document's timestamp is the age,
	// and the file's time is what the claim will overwrite.
	agedPending(t, root, time.Hour)

	if _, _, err := Claim(ctx, root, "."); err != nil {
		t.Fatal(err)
	}

	entries, err := Snapshot(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("snapshot holds %d entries, want 1", len(entries))
	}
	e := entries[0]
	if e.State != stateRunning {
		t.Fatalf("State = %q, want %q", e.State, stateRunning)
	}
	if e.RunningFor > time.Minute {
		t.Errorf("RunningFor = %v, want the age of the claim; an hour-old enqueue "+
			"served just now is not an hour-long attempt", e.RunningFor)
	}
	if e.Age(time.Now()) < 30*time.Minute {
		t.Errorf("Age = %v, want the age of the enqueue; the two are different "+
			"facts and the listing shows both", e.Age(time.Now()))
	}
}

// stubWriter replaces the message writer for the duration of a test.
func stubWriter(t *testing.T, fn func(context.Context, string, string, *Job) (string, error)) {
	t.Helper()
	original := writeMessage
	writeMessage = fn
	t.Cleanup(func() { writeMessage = original })
}

// agedPending backdates every pending job document and file in the root lane,
// standing in for a queue that has been waiting.
func agedPending(t *testing.T, root string, by time.Duration) {
	t.Helper()
	dir := laneDir(root, statePending, ".")
	names, err := sortedJobFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	then := time.Now().Add(-by)
	for _, name := range names {
		path := filepath.Join(dir, name)
		job, err := readJob(path)
		if err != nil {
			t.Fatal(err)
		}
		job.CreatedAt = then.UTC().Format("2006-01-02T15:04:05.000Z")
		data, err := marshalJob(job)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, then, then); err != nil {
			t.Fatal(err)
		}
	}
}

// errString is an error that is exactly its text, for asserting on what camp
// records rather than on how a helper wrapped it.
type errString string

func (e errString) Error() string { return string(e) }
