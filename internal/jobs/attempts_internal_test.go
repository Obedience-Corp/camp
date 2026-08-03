package jobs

import (
	"context"
	"errors"
	"testing"
	"time"
)

var errJobFailed = errors.New("the job failed")

// A parked job must report the same number of tries however you ask.
//
// The listing and --json read the same job document, so a job that ran and
// failed without recording the run made them contradict each other: the text
// said "gave up after 1 attempt" beside a field that said 0. An agent reading
// the queue could not tell a failure that had been retried from one that never
// was, and both are shown identically.
func TestExecutionFailureRecordsTheAttempt(t *testing.T) {
	withFastTiming(t, time.Millisecond, time.Millisecond)
	root := testCampaign(t)
	ctx := context.Background()

	origExecute := executeJob
	executeJob = func(context.Context, string, *Job) error { return errJobFailed }
	t.Cleanup(func() { executeJob = origExecute })

	job, err := Enqueue(ctx, root, Job{
		Kind: KindCommitPaths, Repo: ".", Paths: []string{"a.md"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := Run(ctx, root); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	failed, err := List(root, stateFailed, ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) != 1 || failed[0].ID != job.ID {
		t.Fatalf("failed lane = %+v, want the job that failed (%s)", failed, job.ID)
	}
	if failed[0].Attempts != 1 {
		t.Errorf("Attempts = %d, want 1: a job that ran once and failed has "+
			"used one attempt", failed[0].Attempts)
	}

	// The contradiction this fixes, stated as the invariant it broke: the
	// count the user reads and the count a script reads are the same count.
	note := AttemptNote(failed[0].Attempts, true)
	if note != "gave up after 1 attempt" {
		t.Errorf("AttemptNote(%d, true) = %q, want it to agree with the "+
			"recorded count", failed[0].Attempts, note)
	}
}

// Reclaims and executions both count, because both are a job that started and
// did not finish. A job abandoned twice and then failed has tried three times.
func TestReclaimAndExecutionAttemptsAccumulate(t *testing.T) {
	withFastTiming(t, time.Millisecond, time.Millisecond)
	root := testCampaign(t)
	ctx := context.Background()

	if _, err := Enqueue(ctx, root, Job{
		Kind: KindCommitPaths, Repo: ".", Paths: []string{"a.md"},
	}); err != nil {
		t.Fatal(err)
	}

	// Two workers die mid-job.
	for range 2 {
		if _, _, err := Claim(ctx, root, "."); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Millisecond)
		if _, err := Reclaim(ctx, root, ".", laneLiveness); err != nil {
			t.Fatal(err)
		}
	}

	origExecute := executeJob
	executeJob = func(context.Context, string, *Job) error { return errJobFailed }
	t.Cleanup(func() { executeJob = origExecute })

	if err := Run(ctx, root); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	failed, err := List(root, stateFailed, ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) != 1 {
		t.Fatalf("failed lane holds %d jobs, want 1", len(failed))
	}
	if failed[0].Attempts != 3 {
		t.Errorf("Attempts = %d, want 3: two abandoned runs plus one that "+
			"failed", failed[0].Attempts)
	}
}

// Parking an exhausted job must not invent a run it never had.
//
// parkExhausted moves a job that has already burned its attempts out of
// pending. It did not run here, so counting one would push the total past
// MaxAttempts and make the listing claim a try that never happened.
func TestParkingAnExhaustedJobDoesNotCountAnotherAttempt(t *testing.T) {
	withFastTiming(t, time.Millisecond, time.Millisecond)
	root := testCampaign(t)
	ctx := context.Background()

	if _, err := Enqueue(ctx, root, Job{
		Kind: KindCommitPaths, Repo: ".", Paths: []string{"a.md"},
	}); err != nil {
		t.Fatal(err)
	}

	for range MaxAttempts {
		if _, _, err := Claim(ctx, root, "."); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Millisecond)
		if _, err := Reclaim(ctx, root, ".", laneLiveness); err != nil {
			t.Fatal(err)
		}
	}

	if err := parkExhausted(root, "."); err != nil {
		t.Fatalf("parkExhausted() error = %v", err)
	}

	failed, err := List(root, stateFailed, ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) != 1 {
		t.Fatalf("failed lane holds %d jobs, want 1", len(failed))
	}
	if failed[0].Attempts != MaxAttempts {
		t.Errorf("Attempts = %d, want %d: parking is not a run",
			failed[0].Attempts, MaxAttempts)
	}
}
