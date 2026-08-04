package jobs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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

// Parking must always empty running/, even when the job cannot be parsed.
//
// Reclaim skips what it cannot read, so anything left behind here sits in
// running/ with nothing ever coming for it. Recording the attempt is the part
// that may fail; leaving the lane is not optional.
func TestParkingAnUnreadableJobStillLeavesRunning(t *testing.T) {
	root := testCampaign(t)

	runningDir := laneDir(root, stateRunning, ".")
	if err := os.MkdirAll(runningDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const name = "0000001.json"
	path := filepath.Join(runningDir, name)
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := parkFailedJob(root, ".", path, name); err != nil {
		t.Fatalf("parkFailedJob() error = %v; an unparseable job must still be parked", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("the job is still in running/; nothing will ever pick it up again")
	}
	if _, err := os.Stat(filepath.Join(laneDir(root, stateFailed, "."), name)); err != nil {
		t.Errorf("the job did not reach failed/: %v", err)
	}
}

// The incremented job is written to failed/ rather than edited in place, so an
// interrupted park cannot leave unparseable JSON in running/.
//
// Asserted through the file that is left behind: an in-place implementation
// writes the count to the running path before moving it, and this checks that
// the running path is never the thing carrying the new count.
func TestParkingWritesTheCountToItsDestination(t *testing.T) {
	root := testCampaign(t)
	ctx := context.Background()

	if _, err := Enqueue(ctx, root, Job{
		Kind: KindCommitPaths, Repo: ".", Paths: []string{"a.md"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Claim(ctx, root, "."); err != nil {
		t.Fatal(err)
	}

	runningDir := laneDir(root, stateRunning, ".")
	names, err := sortedJobFiles(runningDir)
	if err != nil || len(names) != 1 {
		t.Fatalf("setup: want one claimed job in running/, got %v (%v)", names, err)
	}
	name := names[0]
	runningPath := filepath.Join(runningDir, name)
	if err := parkFailedJob(root, ".", runningPath, name); err != nil {
		t.Fatalf("parkFailedJob() error = %v", err)
	}

	if _, err := os.Stat(runningPath); !os.IsNotExist(err) {
		t.Error("running/ still holds the job after parking")
	}
	parked, err := readJob(filepath.Join(laneDir(root, stateFailed, "."), name))
	if err != nil {
		t.Fatalf("read the parked job: %v", err)
	}
	if parked.Attempts != 1 {
		t.Errorf("parked Attempts = %d, want 1", parked.Attempts)
	}
}
