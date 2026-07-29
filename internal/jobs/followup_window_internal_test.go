package jobs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// The durability claim under test: from the moment a parent job with a Then
// has executed, either the parent's queue file or its follow-up exists on
// disk. The instant between writing the follow-up and unlinking the parent is
// where the old ordering lost work, so the hook fires exactly there and the
// test asserts both files are present.

func parentWithFollowUp() Job {
	return Job{
		Kind:    KindCommitPaths,
		Repo:    "projects/widget",
		Paths:   []string{"README.md"},
		Message: "project commit",
		Then: &Follow{
			Kind:    KindCommitPaths,
			Repo:    ".",
			Paths:   []string{"projects/widget"},
			Message: "record widget",
		},
	}
}

func TestFollowUpIsDurableBeforeTheParentCompletes(t *testing.T) {
	root := t.TempDir()

	parent := enqueueForTest(t, root, parentWithFollowUp())

	oldExec := executeJob
	executeJob = func(context.Context, string, *Job) error { return nil }
	t.Cleanup(func() { executeJob = oldExec })

	sawWindow := false
	hookInFollowUpWindow = func(*Job) {
		sawWindow = true
		// The parent must still be visible. Completing it before the
		// follow-up is durable is exactly the crash that loses work.
		running := filepath.Join(laneDir(root, stateRunning, parent.Repo), jobFilename(parent.Seq))
		if _, err := os.Stat(running); err != nil {
			t.Errorf("parent not in running/ inside the window: %v", err)
		}
		pending, err := List(root, statePending, ".")
		if err != nil || len(pending) != 1 {
			t.Errorf("follow-up not durable inside the window: jobs=%d err=%v", len(pending), err)
		}
	}
	t.Cleanup(func() { hookInFollowUpWindow = nil })

	if err := Run(context.Background(), root); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !sawWindow {
		t.Fatal("the follow-up window hook never fired")
	}
}

func TestFollowUpEnqueueFailureFailsTheParent(t *testing.T) {
	root := t.TempDir()

	parent := enqueueForTest(t, root, parentWithFollowUp())

	oldExec := executeJob
	executeJob = func(context.Context, string, *Job) error { return nil }
	t.Cleanup(func() { executeJob = oldExec })

	// A regular file where the follow-up's pending lane directory must go
	// makes the enqueue fail. The parent's commit succeeded, but camp promised
	// a commit and a pointer update, so the parent must land in failed/ where
	// the broken half of the promise stays visible, not vanish as a success.
	rootLane := laneDir(root, statePending, ".")
	if err := os.MkdirAll(filepath.Dir(rootLane), 0o755); err != nil {
		t.Fatalf("prepare pending dir: %v", err)
	}
	if err := os.WriteFile(rootLane, []byte("in the way"), 0o644); err != nil {
		t.Fatalf("block root lane: %v", err)
	}

	if err := Run(context.Background(), root); err != nil {
		t.Fatalf("run: %v", err)
	}

	failed, err := List(root, stateFailed, parent.Repo)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(failed) != 1 || failed[0].ID != parent.ID {
		t.Fatalf("parent must be in failed/ after a follow-up enqueue failure, got %d jobs", len(failed))
	}
}
