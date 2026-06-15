//go:build unix

package git

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWithLockRetry_WaitsForActiveLockRelease(t *testing.T) {
	tmpDir := initTestRepo(t)
	lockPath := filepath.Join(tmpDir, ".git", "index.lock")

	f, err := os.Create(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = f.Close()
		_ = os.Remove(lockPath)
	})

	ready := make(chan struct{})
	released := make(chan struct{})
	go func() {
		<-ready
		_ = f.Close()
		_ = os.Remove(lockPath)
		close(released)
	}()

	cfg := DefaultRetryConfig()
	cfg.AttemptsPerCycle = 1
	cfg.MaxCycles = 2
	cfg.InitialBackoff = time.Millisecond
	cfg.MaxBackoff = time.Millisecond
	cfg.ActiveLockWait = 500 * time.Millisecond
	cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg.OperationName = "stage"

	ctx := context.Background()
	attempts := 0
	err = WithLockRetry(ctx, tmpDir, cfg, func() error {
		attempts++
		if _, statErr := os.Stat(lockPath); statErr == nil {
			if attempts == 1 {
				close(ready)
				<-released
			}
			return &LockError{Path: lockPath, Err: errors.New("index.lock exists")}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithLockRetry() error = %v, want nil", err)
	}
}

func TestWithLockRetry_ReturnsActiveLockErrorAfterTimeout(t *testing.T) {
	tmpDir := initTestRepo(t)
	lockPath := filepath.Join(tmpDir, ".git", "index.lock")

	f, err := os.Create(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	cfg := DefaultRetryConfig()
	cfg.AttemptsPerCycle = 1
	cfg.MaxCycles = 1
	cfg.ActiveLockWait = 100 * time.Millisecond
	cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg.OperationName = "stage"

	ctx := context.Background()
	err = WithLockRetry(ctx, tmpDir, cfg, func() error {
		return &LockError{Path: lockPath, Err: errors.New("index.lock exists")}
	})
	if err == nil {
		t.Fatal("WithLockRetry() error = nil, want active lock failure")
	}
	if !errors.Is(err, ErrLockActive) {
		t.Fatalf("WithLockRetry() error = %v, want ErrLockActive", err)
	}
	if !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("WithLockRetry() error = %v, want ErrLockTimeout", err)
	}
}

func TestWithLockRetry_ReturnsRemovalFailureForStaleLock(t *testing.T) {
	tmpDir := initTestRepo(t)
	lockPath := filepath.Join(tmpDir, ".git", "index.lock")
	if err := os.WriteFile(lockPath, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	gitDir := filepath.Join(tmpDir, ".git")
	info, err := os.Stat(gitDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = os.Chmod(gitDir, info.Mode().Perm())
		_ = os.Remove(lockPath)
	}()

	if err := os.Chmod(gitDir, 0555); err != nil {
		t.Fatalf("failed to make .git read-only: %v", err)
	}

	cfg := DefaultRetryConfig()
	cfg.AttemptsPerCycle = 1
	cfg.MaxCycles = 1
	cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg.OperationName = "stage"

	ctx := context.Background()
	err = WithLockRetry(ctx, tmpDir, cfg, func() error {
		return &LockError{Path: lockPath, Err: errors.New("index.lock exists")}
	})
	if err == nil {
		t.Fatal("WithLockRetry() error = nil, want stale lock removal failure")
	}
	if !errors.Is(err, ErrLockRemovalFailed) {
		t.Fatalf("WithLockRetry() error = %v, want ErrLockRemovalFailed", err)
	}
	if errors.Is(err, ErrLockActive) {
		t.Fatalf("WithLockRetry() error = %v, did not want ErrLockActive", err)
	}
}

func TestRetryLoop_ContextCancellation(t *testing.T) {
	tmpDir := initTestRepo(t)
	lockPath := filepath.Join(tmpDir, ".git", "index.lock")

	cfg := DefaultRetryConfig()
	cfg.AttemptsPerCycle = 1
	cfg.MaxCycles = 3
	cfg.InitialBackoff = time.Hour
	cfg.MaxBackoff = time.Hour
	cfg.WaitForActive = false
	cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg.OperationName = "stage"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := WithLockRetry(ctx, tmpDir, cfg, func() error {
		return &LockError{Path: lockPath, Err: errors.New("index.lock exists")}
	})
	elapsed := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("WithLockRetry() error = %v, want context.Canceled", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("WithLockRetry() took %s, want prompt cancellation", elapsed)
	}
}
