package jobs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/autowrite"
)

// A drain's job is to be a barrier, so these tests are about what it refuses to
// return early from. They drive the timing constants rather than sleeping
// through them, and stub the spawn so no real process starts.

// withFastDrain shrinks the drain constants for the duration of a test.
func withFastDrain(t *testing.T) {
	t.Helper()
	oldQuiet, oldMin, oldMax := drainQuiet, drainPollMin, drainPollMax
	oldProgress, oldRecheck, oldMax2 := drainProgressEvery, drainSpawnRecheck, drainMaxWait
	drainQuiet = 5 * time.Millisecond
	drainPollMin, drainPollMax = time.Millisecond, 5*time.Millisecond
	drainProgressEvery = 10 * time.Millisecond
	drainSpawnRecheck = 20 * time.Millisecond
	drainMaxWait = 2 * time.Second
	t.Cleanup(func() {
		drainQuiet, drainPollMin, drainPollMax = oldQuiet, oldMin, oldMax
		drainProgressEvery, drainSpawnRecheck, drainMaxWait = oldProgress, oldRecheck, oldMax2
	})
}

// captureSpawns replaces the spawn hook and returns the lanes it was asked for.
func captureSpawns(t *testing.T) func() []string {
	t.Helper()
	var mu sync.Mutex
	var lanes []string
	old := spawnLane
	spawnLane = func(_ context.Context, _, repo string) {
		mu.Lock()
		defer mu.Unlock()
		lanes = append(lanes, repo)
	}
	t.Cleanup(func() { spawnLane = old })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), lanes...)
	}
}

func enqueueForTest(t *testing.T, root string, job Job) *Job {
	t.Helper()
	out, err := Enqueue(context.Background(), root, job)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	return out
}

// The empty queue is the case that runs on every camp command, so it must cost
// nothing and say nothing.
func TestDrainOnEmptyQueueReturnsImmediatelyAndSilently(t *testing.T) {
	root := testCampaign(t)
	spoke := false

	start := time.Now()
	result, err := Drain(context.Background(), root, ".", DrainOptions{
		OnWaiting: func(DrainStatus) { spoke = true },
	})
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if spoke {
		t.Error("an empty queue must not print a waiting line")
	}
	if result.Waited != 0 {
		t.Errorf("waited %v on an empty queue, want 0", result.Waited)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("empty-queue drain took %v; it must be a directory read", elapsed)
	}
}

// The ordering barrier: a drain does not return while a job it depends on is
// still queued, and returns as soon as the job is gone.
func TestDrainWaitsForAQueuedJobAndReturnsWhenItLands(t *testing.T) {
	withFastDrain(t)
	captureSpawns(t)
	root := testCampaign(t)

	job := enqueueForTest(t, root, Job{Kind: KindCommitPaths, Repo: ".", Paths: []string{"a.md"}})

	done := make(chan error, 1)
	go func() {
		_, err := Drain(context.Background(), root, ".", DrainOptions{Timeout: 5 * time.Second})
		done <- err
	}()

	select {
	case err := <-done:
		t.Fatalf("drain returned before the job landed: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	// Complete the job the way a worker would: the file leaves pending/.
	if err := os.Remove(filepath.Join(laneDir(root, statePending, "."),
		jobFilename(job.Seq))); err != nil {
		t.Fatalf("remove job: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("drain: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("drain did not return after the job landed")
	}
}

// A claimed job has not landed. Ignoring running/ would make the drain return
// exactly when the commit is most likely to be mid-flight.
func TestDrainWaitsForRunningJobsNotOnlyPendingOnes(t *testing.T) {
	root := testCampaign(t)
	enqueueForTest(t, root, Job{Kind: KindCommitPaths, Repo: ".", Paths: []string{"a.md"}})

	if _, _, err := Claim(context.Background(), root, "."); err != nil {
		t.Fatalf("claim: %v", err)
	}

	blocking, err := Outstanding(root, ".")
	if err != nil {
		t.Fatalf("outstanding: %v", err)
	}
	if len(blocking) != 1 {
		t.Fatalf("outstanding = %d jobs, want the running one to still block", len(blocking))
	}
}

// The manifest exemption. Blocking a push on a first-pass hash of a large
// artifact root would put the latency this subsystem removes right back on the
// user's critical path.
func TestDrainIgnoresManifestJobs(t *testing.T) {
	root := testCampaign(t)
	enqueueForTest(t, root, Job{
		Kind: KindCommitPaths, Class: ClassManifest, Repo: ".",
		Paths: []string{".campaign/manifests/videos.json"},
	})

	blocking, err := Outstanding(root, ".")
	if err != nil {
		t.Fatalf("outstanding: %v", err)
	}
	if len(blocking) != 0 {
		t.Fatalf("a manifest job blocked a drain: %+v", blocking)
	}

	result, err := Drain(context.Background(), root, ".", DrainOptions{})
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if result.Waited != 0 {
		t.Errorf("drain waited %v on a manifest-only queue", result.Waited)
	}
}

// Every class except manifest blocks, including one this build does not know.
// Mistaking a blocking job for an exempt one breaks the barrier silently;
// mistaking an exempt job for a blocking one costs a wait.
func TestUnknownClassBlocks(t *testing.T) {
	for _, class := range []Class{"", ClassCommit, "some-future-class"} {
		if !class.Blocking() {
			t.Errorf("class %q must block a drain", class)
		}
	}
	if ClassManifest.Blocking() {
		t.Error("manifest must be exempt from drains")
	}
}

// Enqueue rejects a class this build cannot act on, so a typo surfaces where it
// is cheap rather than as unexplained slowness later.
func TestEnqueueRejectsAnUnknownClass(t *testing.T) {
	root := testCampaign(t)
	_, err := Enqueue(context.Background(), root, Job{
		Kind: KindCommitPaths, Class: "manifets", Repo: ".", Paths: []string{"a.md"},
	})
	if err == nil {
		t.Fatal("enqueue accepted an unknown class")
	}
}

// The follow-up subtlety: a submodule job whose `then` targets the root means
// the root lane is not done, even though the root lane looks empty right now.
func TestRootDrainWaitsOnAnotherLanesRootTargetedFollowUp(t *testing.T) {
	root := testCampaign(t)
	enqueueForTest(t, root, Job{
		Kind: KindCommitPaths, Repo: "projects/camp", Paths: []string{"README.md"},
		Then: &Follow{Kind: KindCommitPaths, Repo: ".", Paths: []string{"projects/camp"}, Message: "update projects/camp submodule ref"},
	})

	if blocking, err := Outstanding(root, "."); err != nil || len(blocking) != 1 {
		t.Fatalf("root drain saw %d blocking jobs (err %v); the pending follow-up parent must count",
			len(blocking), err)
	}

	// A submodule job with no root-targeted follow-up must not hold the root.
	other := testCampaign(t)
	enqueueForTest(t, other, Job{
		Kind: KindCommitPaths, Repo: "projects/fest", Paths: []string{"README.md"},
	})
	if blocking, err := Outstanding(other, "."); err != nil || len(blocking) != 0 {
		t.Fatalf("root drain saw %d blocking jobs (err %v); an unrelated lane must not block the root",
			len(blocking), err)
	}
}

// A manifest's follow-up inherits the exemption. Otherwise the gitlink job the
// worker writes for a manifest would block the drains the manifest itself is
// exempt from, one level down.
func TestManifestFollowUpInheritsTheExemption(t *testing.T) {
	root := testCampaign(t)
	enqueueForTest(t, root, Job{
		Kind: KindCommitPaths, Class: ClassManifest, Repo: "projects/camp",
		Paths: []string{".campaign/manifests/videos.json"},
		Then:  &Follow{Kind: KindCommitPaths, Repo: ".", Paths: []string{"projects/camp"}, Message: "update projects/camp submodule ref"},
	})

	blocking, err := Outstanding(root, ".")
	if err != nil {
		t.Fatalf("outstanding: %v", err)
	}
	if len(blocking) != 0 {
		t.Fatalf("a manifest's root-targeted follow-up blocked a root drain: %+v", blocking)
	}
}

// The worker writes the follow-up, so the exemption has to survive that write
// too. Follow carries no class of its own; the parent's is copied, and this is
// the test that says so.
func TestWorkerWritesFollowUpsWithTheParentsClass(t *testing.T) {
	stubFollowUpCapture(t)
	for _, class := range []Class{ClassManifest, ClassCommit} {
		t.Run(string(class), func(t *testing.T) {
			root := testCampaign(t)
			parent := &Job{
				ID: "job-parent", Kind: KindCommitPaths, Class: class, Repo: "projects/camp",
				Paths: []string{"a.md"},
				Then:  &Follow{Kind: KindCommitPaths, Repo: ".", Paths: []string{"projects/camp"}, Message: "update projects/camp submodule ref"},
			}

			if err := enqueueFollowUp(context.Background(), root, parent); err != nil {
				t.Fatalf("enqueue follow-up: %v", err)
			}

			written, err := List(root, statePending, ".")
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			if len(written) != 1 {
				t.Fatalf("worker wrote %d follow-ups, want 1", len(written))
			}
			if written[0].Class != class {
				t.Errorf("follow-up class = %q, want the parent's %q", written[0].Class, class)
			}
		})
	}
}

// A drain against an unserved lane starts a worker before it waits. Waiting on
// a queue nobody is serving is a wedged terminal, not a slow one, and a drain
// that only spawned on a later re-check would still time out for any timeout
// shorter than that interval.
//
// The re-check is pushed out of reach on purpose: an earlier version of this
// test allowed it, and then passed with the spawn-before-wait call deleted,
// proving only that a spawn happened eventually.
func TestDrainSpawnsAWorkerBeforeItWaits(t *testing.T) {
	withFastDrain(t)
	drainSpawnRecheck = time.Hour
	spawns := captureSpawns(t)
	root := testCampaign(t)

	enqueueForTest(t, root, Job{Kind: KindCommitPaths, Repo: ".", Paths: []string{"a.md"}})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, _ = Drain(ctx, root, ".", DrainOptions{Timeout: time.Hour})

	got := spawns()
	if len(got) == 0 || got[0] != "." {
		t.Fatalf("spawned lanes = %v, want the drained lane spawned before the first wait", got)
	}
}

// A lane with a live worker is left alone: the worker is already serving it,
// and a second process would only find the lane taken and exit.
func TestDrainDoesNotSpawnForALaneWithALiveWorker(t *testing.T) {
	withFastDrain(t)
	spawns := captureSpawns(t)
	root := testCampaign(t)

	enqueueForTest(t, root, Job{Kind: KindCommitPaths, Repo: ".", Paths: []string{"a.md"}})
	lock, ok, err := acquireLane(QueueDir(root), LaneSlug("."))
	if err != nil || !ok {
		t.Fatalf("acquire lane: ok=%v err=%v", ok, err)
	}
	defer lock.release()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, _ = Drain(ctx, root, ".", DrainOptions{Timeout: time.Hour})

	if got := spawns(); len(got) != 0 {
		t.Fatalf("spawned %v for a lane a live worker already holds", got)
	}
}

// The timeout carries the jobs, not a rendered string, so a write command can
// refuse and name them while a read-only command warns and proceeds.
func TestDrainTimeoutReportsTheBlockingJobs(t *testing.T) {
	withFastDrain(t)
	captureSpawns(t)
	root := testCampaign(t)

	enqueueForTest(t, root, Job{
		Kind: KindCommitPaths, Repo: ".", Paths: []string{"a.md"},
		Message: "capture intent: ship the thing",
	})

	_, err := Drain(context.Background(), root, ".", DrainOptions{Timeout: 20 * time.Millisecond})

	var timeout *DrainTimeoutError
	if !errors.As(err, &timeout) {
		t.Fatalf("drain error = %v, want a *DrainTimeoutError", err)
	}
	if len(timeout.Blocking) != 1 {
		t.Fatalf("timeout carried %d jobs, want 1", len(timeout.Blocking))
	}
	if got := Describe(timeout.Blocking[0]); got != "capture intent: ship the thing" {
		t.Errorf("Describe = %q, want the job's subject", got)
	}
}

// Criterion 37i: a job-aware drain keeps waiting while the lane heartbeats, so
// a long deferred message writer does not refuse the user's next commit.
func TestJobAwareDrainWaitsPastTheTimeoutWhileTheLaneHeartbeats(t *testing.T) {
	withFastDrain(t)
	withFastTiming(t, 200*time.Millisecond, 5*time.Millisecond)
	captureSpawns(t)
	root := testCampaign(t)

	enqueueForTest(t, root, Job{
		Kind: KindCommitTree, Repo: ".", Tree: "deadbeef", Parent: "6000fd8f", AutoWrite: true,
	})
	lock, ok, err := acquireLane(QueueDir(root), LaneSlug("."))
	if err != nil || !ok {
		t.Fatalf("acquire lane: ok=%v err=%v", ok, err)
	}
	defer lock.release()

	const timeout = 30 * time.Millisecond
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, err = Drain(ctx, root, ".", DrainOptions{Timeout: timeout, JobAware: true})

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("job-aware drain ended with %v; it must keep waiting while the lane heartbeats", err)
	}
	if waited := time.Since(start); waited < 4*timeout {
		t.Fatalf("job-aware drain gave up after %v, barely past its %v timeout", waited, timeout)
	}
}

// The same drain without JobAware refuses on schedule, so the extension is the
// commit path's choice rather than something every command inherits.
func TestDrainWithoutJobAwareRefusesEvenWhileTheLaneHeartbeats(t *testing.T) {
	withFastDrain(t)
	withFastTiming(t, 200*time.Millisecond, 5*time.Millisecond)
	captureSpawns(t)
	root := testCampaign(t)

	enqueueForTest(t, root, Job{Kind: KindCommitPaths, Repo: ".", Paths: []string{"a.md"}})
	lock, ok, err := acquireLane(QueueDir(root), LaneSlug("."))
	if err != nil || !ok {
		t.Fatalf("acquire lane: ok=%v err=%v", ok, err)
	}
	defer lock.release()

	_, err = Drain(context.Background(), root, ".", DrainOptions{Timeout: 20 * time.Millisecond})

	var timeout *DrainTimeoutError
	if !errors.As(err, &timeout) {
		t.Fatalf("drain error = %v, want a *DrainTimeoutError", err)
	}
}

// A job-aware drain against a dead lane still refuses: the heartbeat is what
// distinguishes slow from abandoned, and a stale lock means nobody is coming.
func TestJobAwareDrainRefusesWhenTheLaneIsAbandoned(t *testing.T) {
	withFastDrain(t)
	withFastTiming(t, 10*time.Millisecond, time.Hour) // lock goes stale immediately
	captureSpawns(t)
	root := testCampaign(t)

	enqueueForTest(t, root, Job{Kind: KindCommitPaths, Repo: ".", Paths: []string{"a.md"}})
	lockPath := filepath.Join(QueueDir(root), laneLockName(LaneSlug(".")))
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-time.Hour)
	if err := os.Chtimes(lockPath, stale, stale); err != nil {
		t.Fatal(err)
	}

	_, err := Drain(context.Background(), root, ".", DrainOptions{
		Timeout: 20 * time.Millisecond, JobAware: true,
	})

	var timeout *DrainTimeoutError
	if !errors.As(err, &timeout) {
		t.Fatalf("drain error = %v; an abandoned lane must not extend a job-aware wait", err)
	}
}

// The waiting line stays quiet for the first quarter second, then names the
// work. A drain that announced immediately would put a spinner on every
// command for work that is already done.
func TestDrainStaysSilentThenNamesTheWork(t *testing.T) {
	withFastDrain(t)
	drainQuiet = 60 * time.Millisecond
	captureSpawns(t)
	root := testCampaign(t)

	enqueueForTest(t, root, Job{
		Kind: KindCommitPaths, Repo: ".", Paths: []string{"a.md"},
		Message: "record workitem WI-abc123",
	})

	var mu sync.Mutex
	var firstAt time.Duration
	var seen []DrainStatus
	start := time.Now()

	_, _ = Drain(context.Background(), root, ".", DrainOptions{
		Timeout: 150 * time.Millisecond,
		OnWaiting: func(s DrainStatus) {
			mu.Lock()
			defer mu.Unlock()
			if firstAt == 0 {
				firstAt = time.Since(start)
			}
			seen = append(seen, s)
		},
	})

	mu.Lock()
	defer mu.Unlock()
	if len(seen) == 0 {
		t.Fatal("a slow drain printed nothing")
	}
	if firstAt < drainQuiet {
		t.Errorf("first waiting line at %v, before the %v quiet period", firstAt, drainQuiet)
	}
	if got := Describe(seen[0].Blocking[0]); got != "record workitem WI-abc123" {
		t.Errorf("waiting line described %q, want the job's own subject", got)
	}
}

// DrainAll is what the whole-campaign commands use, so a job in any lane holds
// it even when that lane has no relationship to the root.
func TestDrainAllWaitsOnEveryLane(t *testing.T) {
	root := testCampaign(t)
	enqueueForTest(t, root, Job{Kind: KindCommitPaths, Repo: "projects/fest", Paths: []string{"a.md"}})

	all, err := OutstandingAll(root)
	if err != nil {
		t.Fatalf("outstanding all: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("OutstandingAll = %d jobs, want the project lane counted", len(all))
	}

	// The same job does not hold a root drain, since it has no root follow-up.
	rootOnly, err := Outstanding(root, ".")
	if err != nil {
		t.Fatalf("outstanding: %v", err)
	}
	if len(rootOnly) != 0 {
		t.Fatalf("a project lane job blocked a root-only drain: %+v", rootOnly)
	}
}

// A repo outside the campaign has no lane, so a drain for it is a no-op rather
// than a wait against a queue that could never hold its work.
func TestRepoForPath(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	tests := []struct {
		name string
		path string
		want string
	}{
		{"the campaign root is the root lane", root, "."},
		{"a project is its campaign-relative path", filepath.Join(root, "projects", "camp"), "projects/camp"},
		{"a worktree is its own lane", filepath.Join(root, "projects", "worktrees", "camp", "wt"), "projects/worktrees/camp/wt"},
		{"outside the campaign has no lane", outside, ""},
		{"empty path has no lane", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RepoForPath(root, tt.path); got != tt.want {
				t.Errorf("RepoForPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

// Describe is what the user reads while waiting, so it prefers the job's own
// message and falls back to something concrete rather than a kind name.
func TestDescribeNamesTheWork(t *testing.T) {
	tests := []struct {
		name string
		job  Job
		want string
	}{
		{
			name: "the message subject wins",
			job:  Job{Kind: KindCommitPaths, Message: "capture intent: fix the thing\n\nbody"},
			want: "capture intent: fix the thing",
		},
		{
			name: "a manifest names its root",
			job:  Job{Kind: KindCommitPaths, Class: ClassManifest, Repo: "projects/camp"},
			want: "artifact manifest for projects/camp",
		},
		{
			name: "otherwise the first path and its repo",
			job:  Job{Kind: KindCommitPaths, Repo: ".", Paths: []string{".campaign/intents/a.md"}},
			want: ".campaign/intents/a.md in .",
		},
		{
			name: "a deferred message writer says so",
			job:  Job{Kind: KindCommitTree, Repo: ".", AutoWrite: true},
			want: "writing a commit message in .",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Describe(tt.job); got != tt.want {
				t.Errorf("Describe = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDrainMaxWaitMatchesWriterTimeout(t *testing.T) {
	if drainMaxWait != autowrite.DefaultWriterTimeout {
		t.Fatalf("drainMaxWait = %v, want autowrite.DefaultWriterTimeout (%v)",
			drainMaxWait, autowrite.DefaultWriterTimeout)
	}
}
