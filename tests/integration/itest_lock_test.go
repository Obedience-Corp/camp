//go:build integration
// +build integration

package integration

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/buildutil/itestenv"
)

// The suite lock is what stops two runs from collapsing each other on one
// daemon, so its contention behavior is worth proving against a real file and
// a real kernel lock rather than a stub. These exercise host filesystem and
// flock semantics, which is why they live in the integration lane rather than
// beside the package's pure-logic tests.

const lockTestWait = 750 * time.Millisecond

func TestSuiteLockSerializesRuns(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "camp-itest.lock")
	var notices bytes.Buffer

	first, err := itestenv.Acquire(context.Background(), path, itestenv.LockOptions{
		Wait:  lockTestWait,
		Label: "first run",
	})
	if err != nil {
		t.Fatalf("Acquire() first = %v", err)
	}

	// A second entrant must not proceed while the first holds the lock, and
	// must name the holder rather than reporting an anonymous stall.
	_, err = itestenv.Acquire(context.Background(), path, itestenv.LockOptions{
		Wait:  lockTestWait,
		Poll:  20 * time.Millisecond,
		Out:   &notices,
		Label: "second run",
	})
	if err == nil {
		t.Fatal("Acquire() succeeded while the lock was held")
	}
	pid := strconv.Itoa(os.Getpid())
	if !strings.Contains(err.Error(), pid) {
		t.Errorf("Acquire() error = %v, want it to name the holder pid %s", err, pid)
	}
	if notice := notices.String(); !strings.Contains(notice, "waiting for second run") {
		t.Errorf("waiting notice = %q, want a visible wait line", notice)
	}

	if err := first.Release(); err != nil {
		t.Fatalf("Release() = %v", err)
	}

	// Once the first run finishes, the daemon is free and the next run takes it.
	second, err := itestenv.Acquire(context.Background(), path, itestenv.LockOptions{
		Wait:  lockTestWait,
		Label: "second run",
	})
	if err != nil {
		t.Fatalf("Acquire() after release = %v", err)
	}
	if err := second.Release(); err != nil {
		t.Fatalf("Release() second = %v", err)
	}
}

// A waiting run has to be interruptible: Ctrl-C on a queued suite must return
// immediately rather than sit out the full wait budget.
func TestSuiteLockWaitHonoursCancellation(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "camp-itest.lock")
	held, err := itestenv.Acquire(context.Background(), path, itestenv.LockOptions{Label: "holder"})
	if err != nil {
		t.Fatalf("Acquire() = %v", err)
	}
	t.Cleanup(func() { _ = held.Release() })

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	if _, err := itestenv.Acquire(ctx, path, itestenv.LockOptions{
		Wait: time.Hour,
		Poll: 10 * time.Millisecond,
	}); err == nil {
		t.Fatal("Acquire() returned a lock that was held")
	}
	if waited := time.Since(start); waited > 10*time.Second {
		t.Errorf("Acquire() took %s to notice cancellation", waited)
	}
}

// The doctor reads this file to answer "is a run in progress", so a held lock
// has to be reported as held, by pid, and a released one as free.
func TestSuiteLockStatusReflectsTheHolder(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "camp-itest.lock")
	if held, description := itestenv.LockStatus(path); held {
		t.Fatalf("LockStatus() on an untouched path = held (%s), want free", description)
	}

	lock, err := itestenv.Acquire(context.Background(), path, itestenv.LockOptions{Label: "a run"})
	if err != nil {
		t.Fatalf("Acquire() = %v", err)
	}
	held, description := itestenv.LockStatus(path)
	if !held {
		t.Errorf("LockStatus() while held = %q, want held", description)
	}
	if pid := strconv.Itoa(os.Getpid()); !strings.Contains(description, pid) {
		t.Errorf("LockStatus() = %q, want it to name pid %s", description, pid)
	}

	if err := lock.Release(); err != nil {
		t.Fatalf("Release() = %v", err)
	}
	if held, description := itestenv.LockStatus(path); held {
		t.Fatalf("LockStatus() after release = %q, want free", description)
	}
}
