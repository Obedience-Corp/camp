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

	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = f.Close()
		_ = os.Remove(lockPath)
	}()

	cfg := DefaultRetryConfig()
	cfg.AttemptsPerCycle = 1
	cfg.MaxCycles = 2
	cfg.ActiveLockWait = 500 * time.Millisecond
	cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg.OperationName = "stage"

	ctx := context.Background()
	err = WithLockRetry(ctx, tmpDir, cfg, func() error {
		if _, statErr := os.Stat(lockPath); statErr == nil {
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
