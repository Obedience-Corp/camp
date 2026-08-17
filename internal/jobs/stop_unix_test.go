//go:build !windows

package jobs

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// Stopping a lane's worker against a real process.
//
// The rest of the drop path can be driven with a stub, but the part that
// matters here cannot: whether camp's escalation actually ends a process, and
// whether it waits before it stops asking politely. A stub that returns
// "stopped" would assert camp's opinion of itself.
func TestTerminateLaneWorker(t *testing.T) {
	tests := []struct {
		name       string
		setup      string
		body       string
		wantKilled bool
	}{
		{
			// The path camp wants: SIGTERM becomes a cancelled context, the
			// worker kills its writer and puts its other jobs back, and it is
			// gone before the grace period is up.
			name: "a worker that honors the stop signal is not killed",
			body: "sleep 30",
		},
		{
			// The path camp needs anyway. A worker wedged in an uninterruptible
			// state never gets to run its cancel, and waiting for it forever
			// would leave the user exactly where they started.
			name:       "a worker that ignores the stop signal is killed",
			setup:      "trap '' TERM",
			body:       "while :; do sleep 0.1; done",
			wantKilled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Short enough to keep the escalation quick, long enough that a
			// process which does stop is not raced into being reported killed.
			restoreTiming := withFastStopTiming(t, 500*time.Millisecond)
			defer restoreTiming()

			root := testCampaign(t)
			queueDir := QueueDir(root)
			if err := os.MkdirAll(queueDir, 0o755); err != nil {
				t.Fatal(err)
			}

			pid := startFakeWorker(t, tt.setup, tt.body)
			lockPath := filepath.Join(queueDir, laneLockName(LaneSlug(".")))
			if err := os.WriteFile(lockPath, []byte(strconv.Itoa(pid)+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}

			stop, err := terminateLaneWorker(root, ".")
			if err != nil {
				t.Fatalf("terminateLaneWorker() error = %v", err)
			}
			if stop.PID != pid {
				t.Errorf("WorkerStop.PID = %d, want %d: camp has to report which "+
					"process it stopped", stop.PID, pid)
			}
			if stop.Killed != tt.wantKilled {
				t.Errorf("WorkerStop.Killed = %v, want %v", stop.Killed, tt.wantKilled)
			}
			if processAlive(pid) {
				t.Errorf("the worker (pid %d) is still running after camp reported "+
					"it stopped", pid)
			}
			if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
				t.Error("the stopped worker's lane lock survived; no worker can take " +
					"that lane until it goes stale")
			}
		})
	}
}

// A lane whose lock names a process that is gone needs no stopping, and camp
// must not signal the pid anyway: pids are reused, and the next holder of that
// number did nothing to deserve a SIGTERM.
func TestTerminateLaneWorkerWithNobodyHome(t *testing.T) {
	root := testCampaign(t)
	queueDir := QueueDir(root)
	if err := os.MkdirAll(queueDir, 0o755); err != nil {
		t.Fatal(err)
	}

	pid := startFakeWorker(t, "", "true")
	// Wait for it to be gone, so the lock names a pid nothing holds.
	if !waitForExit(pid, 5*time.Second) {
		t.Fatalf("the setup process (pid %d) did not exit", pid)
	}
	lockPath := filepath.Join(queueDir, laneLockName(LaneSlug(".")))
	if err := os.WriteFile(lockPath, []byte(strconv.Itoa(pid)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stop, err := terminateLaneWorker(root, ".")
	if err != nil {
		t.Fatalf("terminateLaneWorker() error = %v", err)
	}
	if stop.PID != 0 || stop.Killed {
		t.Errorf("WorkerStop = %+v, want nothing signalled: the lock names a dead "+
			"process, and its pid belongs to whoever holds it next", stop)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Error("a stale lock was removed by a stop that signalled nothing; " +
			"acquireLane steals one this old on sight, and removing it here only " +
			"races a worker starting up")
	}
}

// startFakeWorker runs a shell command the way SpawnIfNeeded runs a worker:
// detached into its own session, so its process group id is its own pid and a
// group signal reaches everything it started.
//
// It returns only once the shell has run setup and said so. Signalling a shell
// that has not finished starting tests nothing: the signal arrives while the
// disposition is still the default, so a worker that was supposed to ignore
// SIGTERM dies of it and the escalation under test never runs.
func startFakeWorker(t *testing.T, setup, body string) int {
	t.Helper()

	ready := filepath.Join(t.TempDir(), "ready")
	script := "printf r > " + ready + "; " + body
	if setup != "" {
		script = setup + "; " + script
	}

	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the fake worker: %v", err)
	}
	pid := cmd.Process.Pid
	// Reaped in the background, because an unreaped child stays a zombie and a
	// zombie still answers signal zero: without this, waiting for it to exit
	// would wait forever for a process that is already dead.
	go func() { _ = cmd.Wait() }()
	t.Cleanup(func() {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	})

	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			return pid
		}
		if time.Now().After(deadline) {
			t.Fatalf("the fake worker never signalled it was ready (script: %s)", script)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// withFastStopTiming shortens the escalation so a test can drive it.
func withFastStopTiming(t *testing.T, grace time.Duration) func() {
	t.Helper()
	original := workerStopGrace
	workerStopGrace = grace
	restore := func() { workerStopGrace = original }
	t.Cleanup(restore)
	return restore
}
