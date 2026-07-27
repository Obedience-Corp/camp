package fsutil

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// lockLivenessBudget bounds tests that must not hang, without asserting how
// fast the scheduler is.
//
// These tests previously used budgets in the 200ms-2s range, which made them
// fail on a loaded machine while passing at idle: `just gate` runs the stable
// suite at roughly 450s of CPU in 40s of wall time, and a goroutine can easily
// wait longer than that for a slot. A flaky gate is worse than a slow one,
// because the honest response to a red gate becomes "run it again" rather than
// "read it".
const lockLivenessBudget = 60 * time.Second

func TestAcquireFileLock_RemovesStaleLock(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "links.yaml.lock")
	if err := os.WriteFile(lockPath, []byte("orphaned"), 0o644); err != nil {
		t.Fatal(err)
	}
	staleTime := time.Now().Add(-(staleLockAfter + time.Second))
	if err := os.Chtimes(lockPath, staleTime, staleTime); err != nil {
		t.Fatal(err)
	}

	release, err := AcquireFileLock(context.Background(), lockPath)
	if err != nil {
		t.Fatalf("AcquireFileLock stale lock: %v", err)
	}
	release()
}

func TestAcquireFileLock_FreshLockIsNotStolen(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "links.yaml.lock")
	if err := os.WriteFile(lockPath, []byte("held"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := AcquireFileLock(ctx, lockPath)
	if err == nil {
		t.Fatal("expected error acquiring fresh lock")
	}
	if _, statErr := os.Stat(lockPath); statErr != nil {
		t.Fatalf("fresh lock should still exist: %v", statErr)
	}
}

func TestTryAcquireFileLock_StaleLockStealRaceHasOneWinner(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "links.yaml.lock")
	if err := os.WriteFile(lockPath, []byte("orphaned"), 0o644); err != nil {
		t.Fatal(err)
	}
	staleTime := time.Now().Add(-(staleLockAfter + time.Second))
	if err := os.Chtimes(lockPath, staleTime, staleTime); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	releases := make([]func(), 2)
	acquired := make([]bool, 2)
	errs := make([]error, 2)
	wg.Add(2)
	for i := range 2 {
		i := i
		go func() {
			defer wg.Done()
			releases[i], acquired[i], errs[i] = tryAcquireFileLock(lockPath)
		}()
	}
	wg.Wait()
	for _, release := range releases {
		if release != nil {
			defer release()
		}
	}

	successes := 0
	for i, err := range errs {
		if err != nil {
			t.Fatalf("tryAcquireFileLock goroutine %d error = %v", i, err)
		}
		if acquired[i] {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("acquired count = %d, want 1", successes)
	}
}

func TestAcquireFileLock_StaleLockStealRaceAcquiresSerially(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "links.yaml.lock")
	if err := os.WriteFile(lockPath, []byte("orphaned"), 0o644); err != nil {
		t.Fatal(err)
	}
	staleTime := time.Now().Add(-(staleLockAfter + time.Second))
	if err := os.Chtimes(lockPath, staleTime, staleTime); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	for i := range 2 {
		i := i
		go func() {
			defer wg.Done()
			// The budget here is a liveness bound, not a performance
			// assertion. What is under test is that both acquirers succeed
			// *serially*: one steals the stale lock, and the other waits for
			// it rather than stealing concurrently. The loser's wait is
			// therefore as long as the winner's whole critical section, so a
			// tight budget measures the scheduler rather than the lock. It is
			// set far above any plausible scheduling delay so that reaching it
			// means genuinely stuck, and costs nothing in the normal case
			// where both finish in milliseconds.
			ctx, cancel := context.WithTimeout(context.Background(), lockLivenessBudget)
			defer cancel()
			release, err := AcquireFileLock(ctx, lockPath)
			errs[i] = err
			if release != nil {
				release()
			}
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("AcquireFileLock goroutine %d error = %v", i, err)
		}
	}
}

func TestAcquireFileLock_ContextCancellation(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "links.yaml.lock")
	if err := os.WriteFile(lockPath, []byte("held"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := AcquireFileLock(ctx, lockPath)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("AcquireFileLock canceled error = %v, want context.Canceled", err)
	}
}

func TestAcquireFileLock_ContextCancellationWhileWaiting(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "links.yaml.lock")
	release, err := AcquireFileLock(context.Background(), lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	waiting := make(chan struct{})
	go func() {
		// Signal that the goroutine is scheduled and about to block, rather
		// than sleeping in the parent and hoping. A sleep makes the test
		// assert that the scheduler ran this goroutine within 50ms, which is
		// not the property under test and is exactly what fails under load.
		close(waiting)
		_, err := AcquireFileLock(ctx, lockPath)
		done <- err
	}()

	<-waiting
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("AcquireFileLock canceled error = %v, want context.Canceled", err)
		}
	case <-time.After(lockLivenessBudget):
		t.Fatal("AcquireFileLock did not return after context cancellation")
	}
}

func TestAcquireFileLock_TimeoutIsCategorized(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "links.yaml.lock")
	if err := os.WriteFile(lockPath, []byte("held"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	_, err := AcquireFileLock(ctx, lockPath)
	if err == nil {
		t.Fatal("expected lock acquisition error")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, camperrors.ErrTimeout) {
		t.Fatalf("AcquireFileLock timeout error = %v, want deadline or ErrTimeout", err)
	}
}
