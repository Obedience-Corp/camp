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
	ageLockFileForTest(t, lockPath)

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

// A worker with nobody waiting on it must outlast a burst of concurrent agent
// sessions committing into one campaign root. The interactive profile gives up
// after six attempts and a single five-second wait, which is about ten seconds
// of patience in total; every index.lock failure in this campaign's worker log
// is a job parked inside that window.
func TestBackgroundRetryConfigOutlastsInteractive(t *testing.T) {
	bg := BackgroundRetryConfig()
	fg := DefaultRetryConfig()

	if bg.MaxCycles <= fg.MaxCycles {
		t.Errorf("background MaxCycles = %d, want more than the interactive %d",
			bg.MaxCycles, fg.MaxCycles)
	}
	if bg.ActiveLockWait <= fg.ActiveLockWait {
		t.Errorf("background ActiveLockWait = %v, want longer than the interactive %v",
			bg.ActiveLockWait, fg.ActiveLockWait)
	}
	if bg.MaxActiveLockWaits < 1 {
		t.Errorf("background MaxActiveLockWaits = %d, want at least one absorbed timeout",
			bg.MaxActiveLockWaits)
	}
	if !bg.WaitForActive {
		t.Error("background WaitForActive = false; a worker that will not wait for a live lock is the interactive profile")
	}
	// The interactive profile must stay impatient. A user in front of a prompt
	// waits for an answer, not for another process to finish.
	if fg.MaxActiveLockWaits != 0 {
		t.Errorf("interactive MaxActiveLockWaits = %d, want 0 so a live lock reports on the first timeout",
			fg.MaxActiveLockWaits)
	}
}

// An active-lock timeout is terminal by default and absorbed up to the
// caller's budget, which is the whole difference between a background worker
// that keeps trying and one that parks a commit it was told to make.
func TestWithLockRetryAbsorbsActiveLockTimeoutsWithinBudget(t *testing.T) {
	cases := []struct {
		name         string
		budget       int
		wantAttempts int
	}{
		{name: "no budget reports on the first timeout", budget: 0, wantAttempts: 1},
		{name: "one absorbed timeout buys another cycle", budget: 1, wantAttempts: 2},
		{name: "budget spent, then reports", budget: 3, wantAttempts: 4},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
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

			cfg := DefaultRetryConfig()
			cfg.AttemptsPerCycle = 1
			cfg.MaxCycles = 20
			cfg.InitialBackoff = time.Millisecond
			cfg.MaxBackoff = time.Millisecond
			cfg.ActiveLockWait = 20 * time.Millisecond
			cfg.MaxActiveLockWaits = tc.budget
			cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
			cfg.OperationName = "reset-index"

			attempts := 0
			err = WithLockRetry(context.Background(), tmpDir, cfg, func() error {
				attempts++
				return &LockError{Path: lockPath, Err: errors.New("index.lock exists")}
			})
			if !errors.Is(err, ErrLockActive) {
				t.Fatalf("WithLockRetry() error = %v, want ErrLockActive", err)
			}
			if attempts != tc.wantAttempts {
				t.Errorf("attempts = %d, want %d", attempts, tc.wantAttempts)
			}
		})
	}
}

// Cancellation is not a timeout. A worker told to stop must return the
// cancellation rather than spending its active-lock budget on it.
func TestWithLockRetryDoesNotAbsorbCancellation(t *testing.T) {
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

	cfg := BackgroundRetryConfig()
	cfg.AttemptsPerCycle = 1
	cfg.InitialBackoff = time.Millisecond
	cfg.MaxBackoff = time.Millisecond
	cfg.ActiveLockWait = time.Minute
	cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))

	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	err = WithLockRetry(ctx, tmpDir, cfg, func() error {
		attempts++
		cancel()
		return &LockError{Path: lockPath, Err: errors.New("index.lock exists")}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("WithLockRetry() error = %v, want context.Canceled", err)
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1; a cancelled wait must not buy another cycle", attempts)
	}
}
