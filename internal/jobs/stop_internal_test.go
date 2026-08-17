package jobs

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Dropping a running job must survive the worker's own shutdown.
//
// A worker told to stop returns the jobs in its lane to pending, which is right
// for a shutdown and exactly wrong here: the user asked for this job to stop,
// and a job that reappears in pending is served again by the next worker, hangs
// on the same writer, and looks to the user like camp ignoring the instruction.
// The job file leaves running/ before the worker is signalled, so the worker's
// requeue finds nothing of ours to put back.
func TestDropRunningSurvivesTheWorkersRequeue(t *testing.T) {
	root := testCampaign(t)
	ctx := context.Background()

	job := claimedJob(t, root)

	// The worker's cancel path, as PR #571 wrote it: everything still in
	// running/ goes back to pending. Called from inside the stop, which is
	// exactly when the real one runs.
	var requeued int
	stubStop(t, func(campaignRoot, lane string) (WorkerStop, error) {
		n, err := RequeueRunning(campaignRoot, lane)
		requeued = n
		return WorkerStop{Lane: lane, PID: 4242}, err
	})

	dropped, stops, err := DropRunning(ctx, root, job.ID)
	if err != nil {
		t.Fatalf("DropRunning() error = %v", err)
	}
	if len(dropped) != 1 || dropped[0].ID != job.ID {
		t.Fatalf("dropped = %+v, want the running job %s", dropped, job.ID)
	}
	if requeued != 0 {
		t.Errorf("the worker requeued %d jobs; the dropped job must be gone from "+
			"running/ before the worker is signalled", requeued)
	}
	for _, state := range []string{statePending, stateRunning, stateFailed} {
		list, err := List(root, state, ".")
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 0 {
			t.Errorf("%s lane holds %+v, want nothing: a dropped job is dropped", state, list)
		}
	}
	if len(stops) != 1 || stops[0].PID != 4242 {
		t.Errorf("stops = %+v, want one stop naming the worker camp signalled", stops)
	}
}

// What DropRunning does when it is asked for something it cannot do.
func TestDropRunningRefusals(t *testing.T) {
	tests := []struct {
		name        string
		selector    string
		cancel      bool
		enqueue     bool
		wantDropped int
		wantStops   int
		wantErr     string
	}{
		{
			name:     "a cancelled context stops before anything is signalled",
			selector: SelectorAll,
			cancel:   true,
			enqueue:  true,
			wantErr:  context.Canceled.Error(),
		},
		{
			// Not an error: the queue is a moving target, and a selector that
			// matched nothing running is how "it already finished" looks.
			name:     "an id that names no running job drops nothing",
			selector: "job-does-not-exist",
			enqueue:  true,
		},
		{
			name:     "an empty queue has nothing to stop",
			selector: SelectorAll,
			enqueue:  false,
		},
		{
			name:        "all takes every running job and stops its lane once",
			selector:    SelectorAll,
			enqueue:     true,
			wantDropped: 1,
			wantStops:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := testCampaign(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.enqueue {
				claimedJob(t, root)
			}
			if tt.cancel {
				cancel()
			}
			stubStop(t, func(_, lane string) (WorkerStop, error) {
				return WorkerStop{Lane: lane, PID: 4242}, nil
			})

			dropped, stops, err := DropRunning(ctx, root, tt.selector)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want one containing %q", err, tt.wantErr)
				}
				if len(stops) != 0 {
					t.Errorf("stops = %+v, want none: a cancelled command must not "+
						"have killed a worker on its way out", stops)
				}
				return
			}
			if err != nil {
				t.Fatalf("DropRunning() error = %v", err)
			}
			if len(dropped) != tt.wantDropped {
				t.Errorf("dropped %d jobs, want %d", len(dropped), tt.wantDropped)
			}
			if len(stops) != tt.wantStops {
				t.Errorf("stops = %+v, want %d", stops, tt.wantStops)
			}
		})
	}
}

// A running job is not a failed job, and the plain drop still says so.
//
// The refusal has to be specific or it is a lie by omission: the user is
// looking at the job in 'camp jobs' and being told no such job failed. What
// they need is the state it is really in and the flag that acts on it.
func TestPlainDropLeavesRunningJobsAloneAndCanBeToldApart(t *testing.T) {
	root := testCampaign(t)
	ctx := context.Background()

	job := claimedJob(t, root)

	if _, err := Drop(ctx, root, job.ID); err == nil {
		t.Fatal("Drop() error = nil; a running job must not be dropped without the flag")
	}
	if running, err := List(root, stateRunning, "."); err != nil {
		t.Fatal(err)
	} else if len(running) != 1 {
		t.Fatalf("running lane holds %d jobs, want the job left alone", len(running))
	}

	entry, ok := RunningMatch(root, job.ID)
	if !ok {
		t.Fatal("RunningMatch() found nothing; the refusal cannot name what it refused")
	}
	if entry.ID != job.ID || entry.Lane != "." {
		t.Errorf("RunningMatch() = %s in %q, want %s in \".\"", entry.ID, entry.Lane, job.ID)
	}
	if _, ok := RunningMatch(root, "job-does-not-exist"); ok {
		t.Error("RunningMatch() matched an id no job has")
	}
}

// Stopping a worker frees every lane it held, not only the one whose job was
// dropped.
//
// A worker serves as many lanes as it can. Killing it leaves its locks behind,
// and a lock that recent reads as a live worker, so SpawnIfNeeded declines to
// start a replacement: the other lanes' jobs would sit unserved for the whole
// liveness window having done nothing wrong.
func TestReleasingLocksOfAStoppedWorker(t *testing.T) {
	queueDir := t.TempDir()
	const stopped, other = 4242, 4243

	writeLock(t, queueDir, LaneSlug("."), stopped)
	writeLock(t, queueDir, LaneSlug("projects/camp"), stopped)
	writeLock(t, queueDir, LaneSlug("projects/fest"), other)

	releaseLocksHeldBy(queueDir, stopped)

	for _, lane := range []string{".", "projects/camp"} {
		if _, ok := laneWorkerPID(queueDir, LaneSlug(lane)); ok {
			t.Errorf("lane %q still holds the stopped worker's lock; nothing can "+
				"take that lane until it goes stale", lane)
		}
	}
	if pid, ok := laneWorkerPID(queueDir, LaneSlug("projects/fest")); !ok || pid != other {
		t.Errorf("another worker's lock was removed (pid %d, ok %v); the file says "+
			"who owns it and that is the only safe way to tell", pid, ok)
	}
}

// The pid in a lane lock is how camp finds the worker to stop, so a lock it
// cannot read has to answer "unknown" rather than a number: a guess is a signal
// sent to whatever else holds that pid.
func TestLaneWorkerPID(t *testing.T) {
	tests := []struct {
		name    string
		content string
		write   bool
		want    int
		wantOK  bool
	}{
		{name: "no lock at all", write: false},
		{name: "empty lock", content: "", write: true},
		{name: "not a number", content: "worker\n", write: true},
		{name: "zero is not a process", content: "0\n", write: true},
		{name: "negative is not a process", content: "-1\n", write: true},
		{name: "a pid with the trailing newline camp writes", content: "4242\n", write: true, want: 4242, wantOK: true},
		{name: "a pid with surrounding whitespace", content: " 4242 \n", write: true, want: 4242, wantOK: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queueDir := t.TempDir()
			if tt.write {
				path := filepath.Join(queueDir, laneLockName(LaneSlug(".")))
				if err := os.WriteFile(path, []byte(tt.content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			pid, ok := laneWorkerPID(queueDir, LaneSlug("."))
			if ok != tt.wantOK || pid != tt.want {
				t.Fatalf("laneWorkerPID() = (%d, %v), want (%d, %v)", pid, ok, tt.want, tt.wantOK)
			}
		})
	}
}

// claimedJob enqueues a job and claims it, leaving it in running/ the way a
// worker mid-commit would.
func claimedJob(t *testing.T, root string) *Job {
	t.Helper()
	ctx := context.Background()
	if _, err := Enqueue(ctx, root, Job{
		Kind: KindCommitPaths, Repo: ".", Paths: []string{"a.md"},
	}); err != nil {
		t.Fatal(err)
	}
	job, _, err := Claim(ctx, root, ".")
	if err != nil || job == nil {
		t.Fatalf("Claim() = (%v, %v), want the enqueued job", job, err)
	}
	return job
}

// stubStop replaces the worker stop for the duration of a test, so the
// ordering can be asserted without a process to signal.
func stubStop(t *testing.T, fn func(campaignRoot, lane string) (WorkerStop, error)) {
	t.Helper()
	original := stopLaneWorker
	stopLaneWorker = fn
	t.Cleanup(func() { stopLaneWorker = original })
}

func writeLock(t *testing.T, queueDir, slug string, pid int) {
	t.Helper()
	path := filepath.Join(queueDir, laneLockName(slug))
	if err := os.WriteFile(path, []byte(strconv.Itoa(pid)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := os.Chtimes(path, now, now); err != nil {
		t.Fatal(err)
	}
}
