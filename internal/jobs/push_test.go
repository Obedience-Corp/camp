package jobs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A push job names itself in the drain's waiting line so a user watching a
// deferred push knows what camp is doing, rather than seeing a bare "push in ."
// that could mean anything.
func TestDescribePushJob(t *testing.T) {
	cases := []struct {
		name string
		job  Job
		want string
	}{
		{
			name: "root push",
			job:  Job{Kind: KindPush, Repo: ".", Remote: "origin", Branch: "main"},
			want: "push main to origin in .",
		},
		{
			name: "submodule push",
			job:  Job{Kind: KindPush, Repo: "projects/camp", Remote: "origin", Branch: "feature"},
			want: "push feature to origin in projects/camp",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Describe(tc.job)
			if got != tc.want {
				t.Errorf("Describe() = %q, want %q", got, tc.want)
			}
		})
	}
}

// EnqueuePush records a push job that Validate accepts and a worker can claim.
// The round-trip through disk is the property that makes deferral safe: a
// job that could not survive a process exit would be a silently-droppable
// promise.
func TestEnqueuePushRoundTrips(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(root, ".campaign", "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	job, err := EnqueuePush(ctx, root, "projects/camp", "origin", "main")
	if err != nil {
		t.Fatalf("EnqueuePush: %v", err)
	}
	if job == nil || job.Kind != KindPush {
		t.Fatalf("got nil or wrong kind: %+v", job)
	}
	if job.Remote != "origin" || job.Branch != "main" {
		t.Errorf("remote=%q branch=%q, want origin/main", job.Remote, job.Branch)
	}
	if job.Class != ClassCommit {
		t.Errorf("class=%q, want %q: a push job must block drains so a later "+
			"command does not skip ahead of it", job.Class, ClassCommit)
	}

	// The job is durable on disk.
	pending, err := List(root, statePending, "projects/camp")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("got %d pending jobs, want 1", len(pending))
	}
	if pending[0].Kind != KindPush {
		t.Errorf("disk job kind=%q, want %q", pending[0].Kind, KindPush)
	}
	if pending[0].Remote != "origin" || pending[0].Branch != "main" {
		t.Errorf("disk job remote=%q branch=%q, want origin/main",
			pending[0].Remote, pending[0].Branch)
	}
}

// EnqueuePush rejects empty remote or branch so a malformed push cannot become
// a promise camp cannot keep.
func TestEnqueuePushRejectsMissing(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, ".campaign", "cache"), 0o755)
	ctx := context.Background()

	if _, err := EnqueuePush(ctx, root, ".", "", "main"); err == nil {
		t.Error("EnqueuePush accepted an empty remote")
	}
	if _, err := EnqueuePush(ctx, root, ".", "origin", ""); err == nil {
		t.Error("EnqueuePush accepted an empty branch")
	}
}

// A non-fast-forward rejection must be classified from git's output, not from
// a synthetic err.Error(): executePush's cmd.Run() is typically "exit status 1".
func TestClassifyPushError(t *testing.T) {
	err := classifyPushError(&simpleError{msg: "exit status 1"},
		"! [rejected] main -> main (non-fast-forward)")
	if err == nil {
		t.Fatal("classifyPushError returned nil for a rejection")
	}
	if !strings.Contains(err.Error(), "non-fast-forward") {
		t.Errorf("classified error does not mention non-fast-forward: %v", err)
	}
	if !strings.Contains(err.Error(), "[rejected]") {
		t.Errorf("classified error dropped git's rejection text: %v", err)
	}

	if err := classifyPushError(nil, "ignored"); err != nil {
		t.Errorf("classifyPushError(nil) = %v, want nil", err)
	}

	bare := classifyPushError(&simpleError{msg: "exit status 1"}, "")
	if bare == nil {
		t.Fatal("bare exit status 1 must still be an error")
	}
	if strings.Contains(bare.Error(), "push rejected") {
		t.Errorf("bare exit status 1 must not be classified as non-fast-forward: %v", bare)
	}
}

// executePush must classify from CombinedOutput, not from err.Error() alone.
// A live git push that is rejected reports "exit status 1"; the phrase
// "non-fast-forward" is on stderr.
func TestExecutePushClassifiesNonFastForwardFromStderr(t *testing.T) {
	old := gitPushCombined
	t.Cleanup(func() { gitPushCombined = old })
	gitPushCombined = func(context.Context, string, *Job) ([]byte, error) {
		return []byte("! [rejected]        main -> main (non-fast-forward)\nerror: failed to push some refs to 'origin'\n"),
			&simpleError{msg: "exit status 1"}
	}

	err := executePush(context.Background(), t.TempDir(), &Job{
		ID: "job-push", Remote: "origin", Branch: "main",
	})
	if err == nil {
		t.Fatal("executePush returned nil for a rejected push")
	}
	if !strings.Contains(err.Error(), "non-fast-forward") {
		t.Errorf("executePush error does not mention non-fast-forward: %v", err)
	}
	if !strings.Contains(err.Error(), "[rejected]") {
		t.Errorf("executePush error dropped git's rejection text: %v", err)
	}
}

// Failures that are not rejections still keep git's output, so LastError is
// not a useless "exit status 1".
func TestExecutePushKeepsStderrOnGenericFailure(t *testing.T) {
	old := gitPushCombined
	t.Cleanup(func() { gitPushCombined = old })
	gitPushCombined = func(context.Context, string, *Job) ([]byte, error) {
		return []byte("fatal: could not read Username for 'https://example.com'\n"),
			&simpleError{msg: "exit status 128"}
	}

	err := executePush(context.Background(), t.TempDir(), &Job{
		ID: "job-push", Remote: "origin", Branch: "main",
	})
	if err == nil {
		t.Fatal("executePush returned nil for an auth failure")
	}
	if !strings.Contains(err.Error(), "could not read Username") {
		t.Errorf("executePush error lost git stderr: %v", err)
	}
}

func TestPushGitEnvSetsPromptAndCLocale(t *testing.T) {
	got := pushGitEnv([]string{
		"PATH=/bin",
		"LANG=en_US.UTF-8",
		"LC_ALL=en_US.UTF-8",
		"GIT_TERMINAL_PROMPT=1",
	})
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "GIT_TERMINAL_PROMPT=0") {
		t.Errorf("missing GIT_TERMINAL_PROMPT=0: %v", got)
	}
	if !strings.Contains(joined, "LC_ALL=C") {
		t.Errorf("missing LC_ALL=C: %v", got)
	}
	for _, item := range got {
		if item == "GIT_TERMINAL_PROMPT=1" || item == "LANG=en_US.UTF-8" || item == "LC_ALL=en_US.UTF-8" {
			t.Errorf("stale env leaked through: %q in %v", item, got)
		}
	}
}

type simpleError struct{ msg string }

func (e *simpleError) Error() string { return e.msg }
