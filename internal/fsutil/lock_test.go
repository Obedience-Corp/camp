package fsutil

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
