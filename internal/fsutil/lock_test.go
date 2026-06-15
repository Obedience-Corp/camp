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
	go func() {
		_, err := AcquireFileLock(ctx, lockPath)
		done <- err
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("AcquireFileLock canceled error = %v, want context.Canceled", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("AcquireFileLock did not return promptly after context cancellation")
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
