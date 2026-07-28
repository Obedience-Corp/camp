package jobs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The read side has one job: agree with itself. A notice that says two commits
// failed while `camp jobs` shows one is worse than either being wrong alone,
// because the user cannot tell which to believe.

func failJobForTest(t *testing.T, root string, job Job) *Job {
	t.Helper()
	enqueued := enqueueForTest(t, root, job)
	claimed, complete, err := Claim(context.Background(), root, job.Repo)
	if err != nil || claimed == nil {
		t.Fatalf("claim: job=%v err=%v", claimed, err)
	}
	if err := complete(os.ErrPermission); err != nil {
		t.Fatalf("fail job: %v", err)
	}
	return enqueued
}

// FailedCount runs before every foreground command, so it has to be both cheap
// and exactly right; Snapshot is the slow path that must agree with it.
func TestFailedCountAgreesWithSnapshot(t *testing.T) {
	root := testCampaign(t)

	if got := FailedCount(root); got != 0 {
		t.Fatalf("FailedCount on an empty campaign = %d, want 0", got)
	}

	failJobForTest(t, root, Job{Kind: KindCommitPaths, Repo: ".", Paths: []string{"a.md"}})
	failJobForTest(t, root, Job{Kind: KindCommitPaths, Repo: "projects/camp", Paths: []string{"b.md"}})
	enqueueForTest(t, root, Job{Kind: KindCommitPaths, Repo: ".", Paths: []string{"c.md"}})

	if got := FailedCount(root); got != 2 {
		t.Errorf("FailedCount = %d, want 2", got)
	}

	entries, err := Snapshot(root)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	failed := 0
	for _, e := range entries {
		if e.State == stateFailed {
			failed++
		}
	}
	if failed != FailedCount(root) {
		t.Errorf("Snapshot saw %d failed, FailedCount said %d; the two surfaces must agree",
			failed, FailedCount(root))
	}
	if len(entries) != 3 {
		t.Errorf("Snapshot = %d entries, want every job in every state", len(entries))
	}
}

// Failures sort first: they are the rows that need a decision, and a listing
// that buries them under pending work makes the user scroll for the only line
// that asks something of them.
func TestSnapshotSortsFailuresFirst(t *testing.T) {
	root := testCampaign(t)
	enqueueForTest(t, root, Job{Kind: KindCommitPaths, Repo: ".", Paths: []string{"pending.md"}})
	failJobForTest(t, root, Job{Kind: KindCommitPaths, Repo: ".", Paths: []string{"failed.md"}})

	entries, err := Snapshot(root)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(entries) != 2 || entries[0].State != stateFailed {
		t.Fatalf("first entry state = %q, want the failure first", entries[0].State)
	}
}

// A running job with no live worker is stuck, not failed: a worker will reclaim
// it. Saying so is how a user tells "slow" from "nobody is home".
func TestSnapshotMarksRunningJobsWithNoWorkerStuck(t *testing.T) {
	root := testCampaign(t)
	enqueueForTest(t, root, Job{Kind: KindCommitPaths, Repo: ".", Paths: []string{"a.md"}})
	if _, _, err := Claim(context.Background(), root, "."); err != nil {
		t.Fatalf("claim: %v", err)
	}

	entries, err := Snapshot(root)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(entries) != 1 || !entries[0].Stuck {
		t.Fatalf("running job with no lane lock: Stuck = %v, want true", entries[0].Stuck)
	}

	// With a live worker holding the lane, the same job is merely running.
	lock, ok, err := acquireLane(QueueDir(root), LaneSlug("."))
	if err != nil || !ok {
		t.Fatalf("acquire lane: ok=%v err=%v", ok, err)
	}
	defer lock.release()

	entries, err = Snapshot(root)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if entries[0].Stuck {
		t.Error("a job whose lane has a live worker must not be marked stuck")
	}
}

// Retry resets attempts, because it is a human deciding the cause is gone.
// Carrying the count would park the job again on its first new attempt, which
// reads as camp ignoring the instruction.
func TestRetryResetsAttemptsAndRequeues(t *testing.T) {
	root := testCampaign(t)
	job := failJobForTest(t, root, Job{
		Kind: KindCommitPaths, Repo: ".", Paths: []string{"a.md"}, Attempts: 3,
	})

	requeued, err := Retry(context.Background(), root, SelectorAll)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if len(requeued) != 1 {
		t.Fatalf("retry moved %d jobs, want 1", len(requeued))
	}

	pending, err := List(root, statePending, ".")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending = %d jobs, want the retried one", len(pending))
	}
	if pending[0].ID != job.ID {
		t.Errorf("requeued id = %q, want %q", pending[0].ID, job.ID)
	}
	if pending[0].Attempts != 0 {
		t.Errorf("requeued attempts = %d, want 0; a retry starts the count over",
			pending[0].Attempts)
	}
	if failed, _ := List(root, stateFailed, "."); len(failed) != 0 {
		t.Errorf("failed lane still holds %d jobs after retry", len(failed))
	}
}

// Drop removes the job, never the content. "Give up on this commit" must not
// mean "throw away my work", which is unrecoverable if it is wrong.
func TestDropRemovesTheJobAndLeavesContentAlone(t *testing.T) {
	root := testCampaign(t)
	content := filepath.Join(root, "note.md")
	if err := os.WriteFile(content, []byte("the user's work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	failJobForTest(t, root, Job{Kind: KindCommitPaths, Repo: ".", Paths: []string{"note.md"}})

	dropped, err := Drop(context.Background(), root, SelectorAll)
	if err != nil {
		t.Fatalf("drop: %v", err)
	}
	if len(dropped) != 1 {
		t.Fatalf("drop removed %d jobs, want 1", len(dropped))
	}
	if failed, _ := List(root, stateFailed, "."); len(failed) != 0 {
		t.Errorf("failed lane still holds %d jobs after drop", len(failed))
	}
	if _, err := os.Stat(content); err != nil {
		t.Fatalf("drop deleted the content the job was going to commit: %v", err)
	}
}

// A selector that names nothing is an error, not a silent success: a user who
// mistypes an id must not be told the job is gone.
func TestDropRejectsAnUnknownID(t *testing.T) {
	root := testCampaign(t)
	failJobForTest(t, root, Job{Kind: KindCommitPaths, Repo: ".", Paths: []string{"a.md"}})

	if _, err := Drop(context.Background(), root, "job-does-not-exist"); err == nil {
		t.Fatal("drop reported success for an id that does not exist")
	}
	if failed, _ := List(root, stateFailed, "."); len(failed) != 1 {
		t.Error("a failed drop must leave the queue untouched")
	}
}

// An unreadable job file can never be matched by id, so only "all" can clear
// it. Without that, the failed-job notice would be permanent with nothing able
// to resolve it.
func TestDropAllClearsAnUnreadableJobFile(t *testing.T) {
	root := testCampaign(t)
	failJobForTest(t, root, Job{Kind: KindCommitPaths, Repo: ".", Paths: []string{"a.md"}})

	failedDir := laneDir(root, stateFailed, ".")
	names, err := sortedJobFiles(failedDir)
	if err != nil || len(names) != 1 {
		t.Fatalf("expected one failed job file, got %v (err %v)", names, err)
	}
	if err := os.WriteFile(filepath.Join(failedDir, names[0]), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Drop(context.Background(), root, SelectorAll); err != nil {
		t.Fatalf("drop all: %v", err)
	}
	if got := FailedCount(root); got != 0 {
		t.Errorf("FailedCount = %d after drop all; an unreadable job must be clearable", got)
	}
}

// The two attempt tenses. Conflating them produces "attempt 4 of 3", which is
// what a job parked after exhausting MaxAttempts rendered as.
func TestAttemptNote(t *testing.T) {
	tests := []struct {
		name     string
		attempts int
		failed   bool
		want     string
	}{
		{"a fresh pending job says nothing", 0, false, ""},
		{"a retried pending job counts forward", 1, false, "attempt 2 of 3"},
		{"a failed job counts what it used", 3, true, "gave up after 3 attempts"},
		{"one attempt agrees in number", 1, true, "gave up after 1 attempt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AttemptNote(tt.attempts, tt.failed); got != tt.want {
				t.Errorf("AttemptNote(%d, %v) = %q, want %q",
					tt.attempts, tt.failed, got, tt.want)
			}
		})
	}
}

// A failed job's forward-looking count would exceed the bound it already hit,
// so the two must never render the same way.
func TestAttemptNoteNeverExceedsTheBound(t *testing.T) {
	got := AttemptNote(MaxAttempts, true)
	if got == "attempt 4 of 3" {
		t.Fatalf("AttemptNote = %q; a parked job must not describe a run that will never happen", got)
	}
}

// Age comes from the job's own timestamp, and an unreadable one is zero rather
// than a duration computed from a zero time.
func TestEntryAge(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	e := Entry{Job: Job{CreatedAt: "2026-07-28T11:30:00.000Z"}}
	if got := e.Age(now); got != 30*time.Minute {
		t.Errorf("Age = %v, want 30m", got)
	}

	bad := Entry{Job: Job{CreatedAt: "not a timestamp"}}
	if got := bad.Age(now); got != 0 {
		t.Errorf("Age on an unreadable timestamp = %v, want 0", got)
	}
}
