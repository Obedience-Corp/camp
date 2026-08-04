package jobs

import (
	"context"
	"errors"
	"testing"
)

// The retry path must recognize landed work before paying for message
// generation: the writer is an external process a retry should not run twice,
// and a writer failing on the retry must not fail a job whose commit is
// sitting in the log. Attempts is only incremented by reclaim, so the check
// runs on reclaimed jobs and never on a first run.

func TestCommitTreeRetryShortCircuitsBeforeTheWriter(t *testing.T) {
	cases := []struct {
		name          string
		attempts      int
		applied       bool
		wantErr       bool
		wantWriterRun bool
		wantCheckRun  bool
	}{
		{"retry of landed work never runs the writer", 1, true, false, false, true},
		{"first run never pays for the check", 0, true, true, true, false},
		{"retry of unlanded work proceeds to the writer", 1, false, true, true, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			checkRuns, writerRuns := 0, 0

			oldApplied := alreadyApplied
			alreadyApplied = func(context.Context, string, *Job) bool {
				checkRuns++
				return tc.applied
			}
			t.Cleanup(func() { alreadyApplied = oldApplied })

			// No real repository behind these jobs: the empty-tree gate would
			// fail resolving parent^{tree}. This test is about retry order, so
			// the empty check is forced off.
			oldEmpty := isEmptyCommitTree
			isEmptyCommitTree = func(context.Context, string, *Job) (bool, error) {
				return false, nil
			}
			t.Cleanup(func() { isEmptyCommitTree = oldEmpty })

			// The writer errors so the proceed cases stop before reaching
			// git: what is under test is the gating order, not the commit.
			oldWrite := writeMessage
			writeMessage = func(context.Context, string, string, *Job) (string, error) {
				writerRuns++
				return "", errors.New("writer boom")
			}
			t.Cleanup(func() { writeMessage = oldWrite })

			job := &Job{
				ID:        "job-retry-gate",
				Kind:      KindCommitTree,
				Repo:      ".",
				Tree:      "feedfacefeedfacefeedfacefeedfacefeedface",
				Parent:    "cafebabecafebabecafebabecafebabecafebabe",
				AutoWrite: true,
				Attempts:  tc.attempts,
			}

			err := executeCommitTree(context.Background(), t.TempDir(), t.TempDir(), job)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, want error %v", err, tc.wantErr)
			}
			if (writerRuns > 0) != tc.wantWriterRun {
				t.Errorf("writer runs = %d, want run %v", writerRuns, tc.wantWriterRun)
			}
			if (checkRuns > 0) != tc.wantCheckRun {
				t.Errorf("applied checks = %d, want run %v", checkRuns, tc.wantCheckRun)
			}
		})
	}
}
