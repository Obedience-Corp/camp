package jobs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The worker's guarantees are all about crashes and races, so these tests
// drive the timing constants rather than sleeping through the real ones. They
// live in-package because the properties under test (lane locking, the
// check-after-release window) are not observable from outside.

// withFastTiming shrinks the lane constants for the duration of a test.
func withFastTiming(t *testing.T, liveness, heartbeat time.Duration) {
	t.Helper()
	oldLiveness, oldHeartbeat := laneLiveness, heartbeatEvery
	laneLiveness, heartbeatEvery = liveness, heartbeat
	t.Cleanup(func() { laneLiveness, heartbeatEvery = oldLiveness, oldHeartbeat })
}

func testCampaign(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".campaign", "cache"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

// A lane is exclusive: a second acquirer is told the lane is taken rather than
// receiving an error, because contention is ordinary operation.
func TestAcquireLaneIsExclusive(t *testing.T) {
	dir := t.TempDir()

	first, ok, err := acquireLane(dir, "%2E")
	if err != nil || !ok {
		t.Fatalf("first acquire = (%v, %v), want held", ok, err)
	}
	defer first.release()

	second, ok, err := acquireLane(dir, "%2E")
	if err != nil {
		t.Fatalf("second acquire error = %v; contention is not an error", err)
	}
	if ok || second != nil {
		t.Fatal("second acquire took a lane already held by a live worker")
	}
}

// A lock whose holder died is stolen, or the lane would be unusable forever.
func TestAcquireLaneStealsAnAbandonedLock(t *testing.T) {
	withFastTiming(t, 20*time.Millisecond, time.Millisecond)
	dir := t.TempDir()

	// A lock with no live holder: written directly, never heartbeated.
	path := filepath.Join(dir, laneLockName("%2E"))
	if err := os.WriteFile(path, []byte("999999\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, stale, stale); err != nil {
		t.Fatal(err)
	}

	lock, ok, err := acquireLane(dir, "%2E")
	if err != nil || !ok {
		t.Fatalf("acquire over an abandoned lock = (%v, %v), want held", ok, err)
	}
	lock.release()
}

// A live worker's heartbeat is what stops its lane being stolen mid-job.
func TestHeartbeatKeepsALaneFromBeingStolen(t *testing.T) {
	withFastTiming(t, 60*time.Millisecond, 5*time.Millisecond)
	dir := t.TempDir()

	held, ok, err := acquireLane(dir, "%2E")
	if err != nil || !ok {
		t.Fatalf("acquire = (%v, %v), want held", ok, err)
	}
	defer held.release()

	// Wait past the liveness threshold. A lock that were not heartbeating
	// would be stealable by now.
	time.Sleep(120 * time.Millisecond)

	other, ok, err := acquireLane(dir, "%2E")
	if err != nil {
		t.Fatalf("second acquire error = %v", err)
	}
	if ok {
		other.release()
		t.Fatal("a heartbeating lane was stolen; a slow job would lose its lock")
	}
}

// release must be safe to call twice, so a deferred release cannot panic on a
// lane already released on the happy path.
func TestLaneReleaseIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	lock, ok, err := acquireLane(dir, "%2E")
	if err != nil || !ok {
		t.Fatalf("acquire = (%v, %v)", ok, err)
	}
	lock.release()
	lock.release()
}

// Criterion 37g, the strand race.
//
// An enqueuer that sees a fresh lane lock skips spawning a worker, so a job
// written between the worker's last claim and its lock release has nobody
// coming for it: that worker is about to exit, and no new one will start.
// The check-after-release re-scan is what makes "a queued job is always
// eventually served" true rather than usually true.
//
// This exercises runLane directly rather than Run. Run's outer loop
// rediscovers lanes, so it would serve the injected job even with the
// invariant removed, and a test that passes either way proves nothing. The
// property belongs to runLane: it must not return while its lane has work.
func TestRunLaneDoesNotReturnWhileItsLaneHasWork(t *testing.T) {
	withFastTiming(t, time.Second, 10*time.Millisecond)
	root := testCampaign(t)
	ctx := context.Background()

	if _, err := Enqueue(ctx, root, Job{
		Kind: KindCommitPaths, Repo: ".", Paths: []string{"first.md"},
	}); err != nil {
		t.Fatal(err)
	}

	var served atomic.Int32
	origExecute := executeJob
	executeJob = func(ctx context.Context, campaignRoot string, job *Job) error {
		served.Add(1)
		return nil
	}
	t.Cleanup(func() { executeJob = origExecute })

	// Inject exactly once, inside the release window: the lane has drained and
	// the lock is gone, which is the moment a job has nobody coming for it.
	var injected atomic.Bool
	hookInReleaseWindow = func(repo string) {
		if !injected.CompareAndSwap(false, true) {
			return
		}
		if _, err := Enqueue(ctx, root, Job{
			Kind: KindCommitPaths, Repo: ".", Paths: []string{"second.md"},
		}); err != nil {
			t.Errorf("inject enqueue: %v", err)
		}
	}
	t.Cleanup(func() { hookInReleaseWindow = nil })

	runLane(ctx, root, ".")

	if got := served.Load(); got != 2 {
		t.Errorf("runLane served %d jobs, want 2; a job written during the "+
			"lane's drain stranded", got)
	}
	pending, err := List(root, statePending, ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("%d jobs left pending after runLane returned, want 0", len(pending))
	}
}

// Criterion 37k: the lane cap bounds concurrent workers, and every lane is
// still eventually served.
func TestLaneCapBoundsConcurrencyAndStillServesEveryLane(t *testing.T) {
	withFastTiming(t, time.Second, 10*time.Millisecond)
	oldCap := laneCap
	laneCap = 2
	t.Cleanup(func() { laneCap = oldCap })

	root := testCampaign(t)
	ctx := context.Background()

	repos := []string{".", "projects/a", "projects/b", "projects/c", "projects/d", "projects/e"}
	for _, repo := range repos {
		if _, err := Enqueue(ctx, root, Job{
			Kind: KindCommitPaths, Repo: repo, Paths: []string{"f.md"},
		}); err != nil {
			t.Fatal(err)
		}
	}

	var mu sync.Mutex
	inFlight, peak := 0, 0
	seen := make(map[string]bool)

	origExecute := executeJob
	executeJob = func(ctx context.Context, campaignRoot string, job *Job) error {
		mu.Lock()
		inFlight++
		if inFlight > peak {
			peak = inFlight
		}
		seen[job.Repo] = true
		mu.Unlock()

		time.Sleep(20 * time.Millisecond) // hold the lane long enough to overlap

		mu.Lock()
		inFlight--
		mu.Unlock()
		return nil
	}
	t.Cleanup(func() { executeJob = origExecute })

	if err := Run(ctx, root); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if peak > laneCap {
		t.Errorf("peak concurrency %d exceeded the cap of %d", peak, laneCap)
	}
	if len(seen) != len(repos) {
		t.Errorf("served %d lanes, want all %d: a capped worker must not "+
			"leave a lane unserved", len(seen), len(repos))
	}
}

// A job that keeps failing is parked rather than retried forever, or a
// poisoned job hides behind a permanently busy queue.
func TestExhaustedJobIsParkedInFailed(t *testing.T) {
	withFastTiming(t, time.Millisecond, time.Millisecond)
	root := testCampaign(t)
	ctx := context.Background()

	job, err := Enqueue(ctx, root, Job{
		Kind: KindCommitPaths, Repo: ".", Paths: []string{"a.md"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Simulate three abandoned attempts: claim without completing, then let
	// reclaim age it back to pending each time.
	for range MaxAttempts {
		if _, _, err := Claim(ctx, root, "."); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Millisecond)
		if _, err := Reclaim(ctx, root, ".", laneLiveness); err != nil {
			t.Fatal(err)
		}
	}

	pending, err := List(root, statePending, ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Attempts < MaxAttempts {
		t.Fatalf("setup: want one pending job with >= %d attempts, got %+v", MaxAttempts, pending)
	}

	if err := parkExhausted(root, "."); err != nil {
		t.Fatalf("parkExhausted() error = %v", err)
	}

	failed, err := List(root, stateFailed, ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) != 1 || failed[0].ID != job.ID {
		t.Errorf("failed lane = %+v, want the exhausted job %s", failed, job.ID)
	}
	pending, _ = List(root, statePending, ".")
	if len(pending) != 0 {
		t.Errorf("%d jobs still pending, want 0: an exhausted job must not be "+
			"picked up again", len(pending))
	}
}

// A worker told to stop mid-job records no verdict on the job it was running.
//
// Cancellation kills the message writer and every git subprocess it started, so
// the error the job returns describes the shutdown rather than the work.
// Parking it spends the job's one recorded attempt on nothing, and a
// commit-tree job whose parent later stops being HEAD is then unretryable
// forever: the user is told a commit failed, offered a retry that cannot work,
// and left to notice the missing commit themselves.
func TestCancelledJobIsLeftForReclaimNotParkedInFailed(t *testing.T) {
	withFastTiming(t, time.Millisecond, time.Millisecond)
	root := testCampaign(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	job, err := Enqueue(ctx, root, Job{
		Kind: KindCommitPaths, Repo: ".", Paths: []string{"a.md"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// The shape of the real failure: SIGTERM reaches the detached worker while
	// the commit message writer is mid-generation, so exec.CommandContext kills
	// the writer and the job returns the cancellation rather than a verdict.
	origExecute := executeJob
	executeJob = func(ctx context.Context, _ string, _ *Job) error {
		cancel()
		return ctx.Err()
	}
	t.Cleanup(func() { executeJob = origExecute })

	runLane(ctx, root, ".")

	if failed, err := List(root, stateFailed, "."); err != nil {
		t.Fatal(err)
	} else if len(failed) != 0 {
		t.Errorf("%d jobs parked in failed/, want 0: a cancelled worker must not "+
			"record a verdict on the job it was running", len(failed))
	}

	running, err := List(root, stateRunning, ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(running) != 1 || running[0].ID != job.ID {
		t.Fatalf("running lane = %+v, want the cancelled job %s left for reclaim",
			running, job.ID)
	}

	// Reclaim is the path that picks it up, the same one a worker that died
	// outright takes: back to pending with the attempt counted and bounded.
	time.Sleep(2 * time.Millisecond)
	n, err := Reclaim(context.Background(), root, ".", laneLiveness)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("Reclaim() = %d, want 1: the cancelled job must come back", n)
	}
	pending, err := List(root, statePending, ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Attempts != 1 {
		t.Errorf("pending after reclaim = %+v, want the job requeued with attempts=1",
			pending)
	}
}

// A campaign with nothing queued does no work and reports no error.
func TestRunOnAnEmptyQueueIsANoOp(t *testing.T) {
	root := testCampaign(t)
	if err := Run(context.Background(), root); err != nil {
		t.Errorf("Run() on an empty queue error = %v, want nil", err)
	}
}

// Two workers racing for the same lanes must not double-serve a job.
func TestConcurrentWorkersServeEachJobOnce(t *testing.T) {
	withFastTiming(t, time.Second, 10*time.Millisecond)
	root := testCampaign(t)
	ctx := context.Background()

	const n = 10
	for i := range n {
		if _, err := Enqueue(ctx, root, Job{
			Kind: KindCommitPaths, Repo: ".", Paths: []string{fmt.Sprintf("f%d.md", i)},
		}); err != nil {
			t.Fatal(err)
		}
	}

	var mu sync.Mutex
	counts := make(map[string]int)
	origExecute := executeJob
	executeJob = func(ctx context.Context, campaignRoot string, job *Job) error {
		mu.Lock()
		counts[job.ID]++
		mu.Unlock()
		return nil
	}
	t.Cleanup(func() { executeJob = origExecute })

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = Run(ctx, root)
		}()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(counts) != n {
		t.Errorf("served %d distinct jobs, want %d", len(counts), n)
	}
	for id, c := range counts {
		if c != 1 {
			t.Errorf("job %s served %d times, want once", id, c)
		}
	}
}

// A second worker must not spin when every lane with work is already held.
func TestRunDoesNotSpinWhenAllLanesAreHeld(t *testing.T) {
	withFastTiming(t, 10*time.Second, time.Second)
	root := testCampaign(t)
	ctx := context.Background()

	if _, err := Enqueue(ctx, root, Job{
		Kind: KindCommitPaths, Repo: ".", Paths: []string{"a.md"},
	}); err != nil {
		t.Fatal(err)
	}

	// Another live worker owns the only lane with work.
	held, ok, err := acquireLane(QueueDir(root), LaneSlug("."))
	if err != nil || !ok {
		t.Fatalf("setup acquire = (%v, %v)", ok, err)
	}
	defer held.release()

	done := make(chan error, 1)
	go func() { done <- Run(ctx, root) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return; it is spinning on a lane owned by another worker")
	}
}

// SpawnIfNeeded's guards decide whether a detached worker starts at all. They
// are tested separately from the spawn itself: starting a real process in a
// unit test would be slow and racy, while the guards are where the decisions
// live and where a mistake is expensive in both directions. Spawning when a
// worker already exists wastes a process; failing to spawn strands work.
func TestSpawnIfNeededGuards(t *testing.T) {
	withFastTiming(t, time.Second, 10*time.Millisecond)
	ctx := context.Background()

	t.Run("empty lane does not spawn", func(t *testing.T) {
		root := testCampaign(t)
		SpawnIfNeeded(ctx, root, ".")
		assertNoWorkerStarted(t, root)
	})

	t.Run("lane held by a live worker does not spawn", func(t *testing.T) {
		root := testCampaign(t)
		if _, err := Enqueue(ctx, root, Job{
			Kind: KindCommitPaths, Repo: ".", Paths: []string{"a.md"},
		}); err != nil {
			t.Fatal(err)
		}
		lock, ok, err := acquireLane(QueueDir(root), LaneSlug("."))
		if err != nil || !ok {
			t.Fatalf("setup acquire = (%v, %v)", ok, err)
		}
		defer lock.release()

		if !laneLockFresh(QueueDir(root), LaneSlug(".")) {
			t.Fatal("setup: the lane lock should read as fresh")
		}
		SpawnIfNeeded(ctx, root, ".")
		assertNoWorkerStarted(t, root)
	})

	t.Run("at the cap does not spawn", func(t *testing.T) {
		root := testCampaign(t)
		if _, err := Enqueue(ctx, root, Job{
			Kind: KindCommitPaths, Repo: "projects/z", Paths: []string{"a.md"},
		}); err != nil {
			t.Fatal(err)
		}
		// Fill the cap with locks for other lanes.
		for i := range laneCap {
			lock, ok, err := acquireLane(QueueDir(root), LaneSlug(fmt.Sprintf("projects/held%d", i)))
			if err != nil || !ok {
				t.Fatalf("setup acquire %d = (%v, %v)", i, ok, err)
			}
			defer lock.release()
		}
		if got := countFreshLaneLocks(QueueDir(root)); got < laneCap {
			t.Fatalf("setup: %d fresh locks, want at least %d", got, laneCap)
		}
		SpawnIfNeeded(ctx, root, "projects/z")
		assertNoWorkerStarted(t, root)
	})
}

// assertNoWorkerStarted checks the worker log records no spawn.
func assertNoWorkerStarted(t *testing.T, root string) {
	t.Helper()
	data, err := os.ReadFile(WorkerLogPath(root))
	if err != nil {
		return // no log at all means nothing was spawned
	}
	if strings.Contains(string(data), "spawned") {
		t.Errorf("a worker was spawned when the guards should have prevented it:\n%s", data)
	}
}
