//go:build unix

package git

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsLockStale_NoProcess(t *testing.T) {
	// Create a lock file that no process is using
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "index.lock")
	os.WriteFile(lockPath, []byte{}, 0644)

	ctx := context.Background()
	stale, info, err := IsLockStale(ctx, lockPath)
	if err != nil {
		t.Fatalf("IsLockStale() error = %v", err)
	}
	if !stale {
		t.Error("IsLockStale() = false, want true for unused file")
	}
	if info.ProcessID != 0 {
		t.Errorf("ProcessID = %d, want 0 for stale lock", info.ProcessID)
	}
	if !info.Stale {
		t.Error("LockInfo.Stale = false, want true")
	}
}

func TestIsLockStale_ActiveProcess(t *testing.T) {
	// Create a lock file and keep it open
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "index.lock")

	// Open file and keep handle
	f, err := os.Create(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	ctx := context.Background()
	stale, info, err := IsLockStale(ctx, lockPath)
	if err != nil {
		t.Fatalf("IsLockStale() error = %v", err)
	}
	if stale {
		t.Error("IsLockStale() = true, want false for file held by process")
	}
	if info.ProcessID == 0 {
		t.Error("ProcessID = 0, want non-zero for active lock")
	}
	if info.Stale {
		t.Error("LockInfo.Stale = true, want false")
	}
}

func TestIsLockStale_NonExistentFile(t *testing.T) {
	ctx := context.Background()
	_, _, err := IsLockStale(ctx, "/nonexistent/path/index.lock")
	// This should not panic - it will either return an error or treat as stale
	// The behavior depends on fuser/lsof handling of non-existent files
	_ = err // Error is acceptable here
}

func TestIsLockStale_ContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "index.lock")
	os.WriteFile(lockPath, []byte{}, 0644)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, _, err := IsLockStale(ctx, lockPath)
	// Context cancellation may or may not propagate depending on timing
	// Just ensure no panic
	_ = err
}

func TestCheckLocksStaleness(t *testing.T) {
	t.Run("categorizes stale locks", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create two stale locks
		lock1 := filepath.Join(tmpDir, "index1.lock")
		lock2 := filepath.Join(tmpDir, "index2.lock")
		os.WriteFile(lock1, []byte{}, 0644)
		os.WriteFile(lock2, []byte{}, 0644)

		ctx := context.Background()
		stale, active, err := CheckLocksStaleness(ctx, []string{lock1, lock2})
		if err != nil {
			t.Fatalf("CheckLocksStaleness() error = %v", err)
		}

		if len(stale) != 2 {
			t.Errorf("stale count = %d, want 2", len(stale))
		}
		if len(active) != 0 {
			t.Errorf("active count = %d, want 0", len(active))
		}
	})

	t.Run("categorizes active locks", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create a stale lock
		staleLock := filepath.Join(tmpDir, "stale.lock")
		os.WriteFile(staleLock, []byte{}, 0644)

		// Create an active lock (file held open)
		activeLock := filepath.Join(tmpDir, "active.lock")
		f, err := os.Create(activeLock)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()

		ctx := context.Background()
		stale, active, err := CheckLocksStaleness(ctx, []string{staleLock, activeLock})
		if err != nil {
			t.Fatalf("CheckLocksStaleness() error = %v", err)
		}

		if len(stale) != 1 {
			t.Errorf("stale count = %d, want 1", len(stale))
		}
		if len(active) != 1 {
			t.Errorf("active count = %d, want 1", len(active))
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, _, err := CheckLocksStaleness(ctx, []string{"/some/path"})
		if err != context.Canceled {
			t.Errorf("error = %v, want context.Canceled", err)
		}
	})

	t.Run("handles empty list", func(t *testing.T) {
		ctx := context.Background()
		stale, active, err := CheckLocksStaleness(ctx, []string{})
		if err != nil {
			t.Fatalf("CheckLocksStaleness() error = %v", err)
		}
		if len(stale) != 0 || len(active) != 0 {
			t.Error("expected empty results for empty input")
		}
	})
}

func TestLockInfo(t *testing.T) {
	t.Run("stale lock info", func(t *testing.T) {
		info := LockInfo{
			Path:      "/path/to/index.lock",
			Stale:     true,
			ProcessID: 0,
			Command:   "",
		}

		if info.Path != "/path/to/index.lock" {
			t.Errorf("Path = %q, want /path/to/index.lock", info.Path)
		}
		if !info.Stale {
			t.Error("Stale = false, want true")
		}
		if info.ProcessID != 0 {
			t.Errorf("ProcessID = %d, want 0", info.ProcessID)
		}
	})

	t.Run("active lock info", func(t *testing.T) {
		info := LockInfo{
			Path:      "/path/to/index.lock",
			Stale:     false,
			ProcessID: 12345,
			Command:   "git",
		}

		if info.Stale {
			t.Error("Stale = true, want false")
		}
		if info.ProcessID != 12345 {
			t.Errorf("ProcessID = %d, want 12345", info.ProcessID)
		}
		if info.Command != "git" {
			t.Errorf("Command = %q, want git", info.Command)
		}
	})
}

func TestCheckLocksStaleness_NonExistentFiles(t *testing.T) {
	// Test with files that don't exist - should be treated as active for safety
	ctx := context.Background()
	locks := []string{"/nonexistent/lock1", "/nonexistent/lock2"}

	stale, active, err := CheckLocksStaleness(ctx, locks)
	if err != nil {
		t.Fatalf("CheckLocksStaleness() error = %v", err)
	}

	// Non-existent files might be treated as stale or active depending on tool behavior
	// Just verify we get some categorization without panicking
	total := len(stale) + len(active)
	if total != 2 {
		t.Errorf("total categorized = %d, want 2", total)
	}
}

func TestRemoveStaleLocks(t *testing.T) {
	t.Run("removes stale locks", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create two stale locks
		lock1 := filepath.Join(tmpDir, "lock1")
		lock2 := filepath.Join(tmpDir, "lock2")
		os.WriteFile(lock1, []byte{}, 0644)
		os.WriteFile(lock2, []byte{}, 0644)

		ctx := context.Background()
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))

		removed, active, failed, err := RemoveStaleLocks(ctx, []string{lock1, lock2}, logger)
		if err != nil {
			t.Fatalf("RemoveStaleLocks() error = %v", err)
		}

		if len(removed) != 2 {
			t.Errorf("removed %d locks, want 2", len(removed))
		}
		if len(active) != 0 {
			t.Errorf("active %d locks, want 0", len(active))
		}
		if len(failed) != 0 {
			t.Errorf("failed %d locks, want 0", len(failed))
		}

		// Verify files are gone
		if _, err := os.Stat(lock1); !os.IsNotExist(err) {
			t.Error("lock1 still exists after removal")
		}
		if _, err := os.Stat(lock2); !os.IsNotExist(err) {
			t.Error("lock2 still exists after removal")
		}
	})

	t.Run("skips active locks", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, "index.lock")

		// Create and hold open
		f, err := os.Create(lockPath)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()

		ctx := context.Background()
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))

		removed, active, failed, err := RemoveStaleLocks(ctx, []string{lockPath}, logger)
		if err != nil {
			t.Fatalf("RemoveStaleLocks() error = %v", err)
		}

		if len(removed) != 0 {
			t.Error("removed active lock, should have left it active")
		}
		if len(active) != 1 {
			t.Errorf("active %d locks, want 1", len(active))
		}
		if len(failed) != 0 {
			t.Errorf("failed %d locks, want 0", len(failed))
		}

		// Verify file still exists
		if _, err := os.Stat(lockPath); os.IsNotExist(err) {
			t.Error("active lock was removed, should still exist")
		}
	})

	t.Run("handles already removed files", func(t *testing.T) {
		ctx := context.Background()
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))

		// Non-existent file
		removed, active, failed, err := RemoveStaleLocks(ctx, []string{"/nonexistent/lock"}, logger)
		if err != nil {
			t.Fatalf("RemoveStaleLocks() error = %v", err)
		}

		// Should count as "removed" since it's already gone
		if len(removed) != 1 {
			t.Errorf("removed = %d, want 1 for already-gone file", len(removed))
		}
		if len(active) != 0 {
			t.Errorf("active = %d, want 0", len(active))
		}
		if len(failed) != 0 {
			t.Errorf("failed = %d, want 0", len(failed))
		}
	})

	t.Run("tracks stale lock removal failures separately", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, "index.lock")
		if err := os.WriteFile(lockPath, []byte{}, 0644); err != nil {
			t.Fatal(err)
		}

		info, err := os.Stat(tmpDir)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			_ = os.Chmod(tmpDir, info.Mode().Perm())
		}()

		if err := os.Chmod(tmpDir, 0555); err != nil {
			t.Fatalf("failed to make directory read-only: %v", err)
		}

		ctx := context.Background()
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))

		removed, active, failed, err := RemoveStaleLocks(ctx, []string{lockPath}, logger)
		if err != nil {
			t.Fatalf("RemoveStaleLocks() error = %v", err)
		}

		if len(removed) != 0 {
			t.Errorf("removed %d locks, want 0", len(removed))
		}
		if len(active) != 0 {
			t.Errorf("active %d locks, want 0", len(active))
		}
		if len(failed) != 1 {
			t.Fatalf("failed %d locks, want 1", len(failed))
		}
		if !errors.Is(failed[0].Err, ErrLockRemovalFailed) {
			t.Fatalf("failed[0].Err = %v, want ErrLockRemovalFailed", failed[0].Err)
		}
		if failed[0].Info.Path != lockPath {
			t.Fatalf("failed[0].Info.Path = %q, want %q", failed[0].Info.Path, lockPath)
		}
	})

	t.Run("uses default logger when nil", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, "lock")
		os.WriteFile(lockPath, []byte{}, 0644)

		ctx := context.Background()
		removed, _, _, err := RemoveStaleLocks(ctx, []string{lockPath}, nil)
		if err != nil {
			t.Fatalf("RemoveStaleLocks() error = %v", err)
		}

		if len(removed) != 1 {
			t.Errorf("removed = %d, want 1", len(removed))
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		_, _, _, err := RemoveStaleLocks(ctx, []string{"/some/path"}, logger)
		if err != context.Canceled {
			t.Errorf("error = %v, want context.Canceled", err)
		}
	})
}

func TestRemovalResult(t *testing.T) {
	t.Run("Summary with removed, active, and failed", func(t *testing.T) {
		result := &RemovalResult{
			Removed: []LockInfo{
				{Path: "/path/to/lock1", Stale: true},
			},
			Active: []LockInfo{
				{Path: "/path/to/lock2", ProcessID: 1234, Command: "git"},
			},
			Failed: []LockRemovalFailure{
				{
					Info: LockInfo{Path: "/path/to/lock3"},
					Err:  errors.New("operation not permitted"),
				},
			},
			TotalLocks: 2,
		}

		summary := result.Summary()
		if !strings.Contains(summary, "Processed 2 lock file(s)") {
			t.Error("summary missing total count")
		}
		if !strings.Contains(summary, "Removed (stale): 1") {
			t.Error("summary missing removed count")
		}
		if !strings.Contains(summary, "Active (waiting or blocked): 1") {
			t.Error("summary missing active count")
		}
		if !strings.Contains(summary, "PID 1234: git") {
			t.Error("summary missing PID info")
		}
		if !strings.Contains(summary, "Failed to remove (stale): 1") {
			t.Error("summary missing failed count")
		}
		if !strings.Contains(summary, "operation not permitted") {
			t.Error("summary missing failed removal detail")
		}
	})

	t.Run("AllRemoved true when no active or failed locks", func(t *testing.T) {
		result := &RemovalResult{
			Removed:    []LockInfo{{Path: "/lock"}},
			Active:     nil,
			Failed:     nil,
			TotalLocks: 1,
		}
		if !result.AllRemoved() {
			t.Error("AllRemoved() = false, want true")
		}
	})

	t.Run("AllRemoved false when active locks remain", func(t *testing.T) {
		result := &RemovalResult{
			Removed:    nil,
			Active:     []LockInfo{{Path: "/lock"}},
			TotalLocks: 1,
		}
		if result.AllRemoved() {
			t.Error("AllRemoved() = true, want false")
		}
	})

	t.Run("AllRemoved false when stale lock removal fails", func(t *testing.T) {
		result := &RemovalResult{
			Removed: nil,
			Failed: []LockRemovalFailure{{
				Info: LockInfo{Path: "/lock"},
				Err:  errors.New("operation not permitted"),
			}},
			TotalLocks: 1,
		}
		if result.AllRemoved() {
			t.Error("AllRemoved() = true, want false")
		}
	})
}

func TestCleanStaleLocks(t *testing.T) {
	t.Run("cleans stale locks from repository", func(t *testing.T) {
		tmpDir := t.TempDir()
		gitDir := filepath.Join(tmpDir, ".git")
		os.MkdirAll(gitDir, 0755)

		// Create a stale lock
		lockPath := filepath.Join(gitDir, "index.lock")
		os.WriteFile(lockPath, []byte{}, 0644)

		ctx := context.Background()
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))

		result, err := CleanStaleLocks(ctx, tmpDir, logger)
		if err != nil {
			t.Fatalf("CleanStaleLocks() error = %v", err)
		}

		if result.TotalLocks != 1 {
			t.Errorf("TotalLocks = %d, want 1", result.TotalLocks)
		}
		if len(result.Removed) != 1 {
			t.Errorf("Removed = %d, want 1", len(result.Removed))
		}

		// Verify lock is gone
		if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
			t.Error("lock still exists after cleanup")
		}
	})

	t.Run("handles no locks found", func(t *testing.T) {
		tmpDir := t.TempDir()
		gitDir := filepath.Join(tmpDir, ".git")
		os.MkdirAll(gitDir, 0755)

		ctx := context.Background()
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))

		result, err := CleanStaleLocks(ctx, tmpDir, logger)
		if err != nil {
			t.Fatalf("CleanStaleLocks() error = %v", err)
		}

		if result.TotalLocks != 0 {
			t.Errorf("TotalLocks = %d, want 0", result.TotalLocks)
		}
	})

	t.Run("returns error for non-repo", func(t *testing.T) {
		tmpDir := t.TempDir()
		// No .git directory

		ctx := context.Background()
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))

		_, err := CleanStaleLocks(ctx, tmpDir, logger)
		if err == nil {
			t.Error("CleanStaleLocks() expected error for non-repo")
		}
	})
}
