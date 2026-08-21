package fsutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

const (
	defaultLockTimeout = 5 * time.Second
	// staleLockAfter is the age beyond which a lock file is considered
	// abandoned. Thirty seconds is intentionally conservative: current camp
	// locks guard short registry, priority-store, and link writes that should
	// complete well under a second. A legitimate holder beyond this threshold
	// is assumed to be crashed, SIGKILL'd, or wedged; future long-running lock
	// users should use a separate threshold rather than stretching this global
	// default.
	staleLockAfter = 30 * time.Second
	lockRetryDelay = 20 * time.Millisecond
)

// onContendedLock is an optional test hook invoked after a failed acquire
// attempt when the lock is held, immediately before the wait/select. Production
// leaves it nil. Tests use it to signal "inside the wait path" without racing
// the cancel that would otherwise fire between goroutine start and first
// tryAcquire.
var onContendedLock func()

// AcquireFileLock takes an exclusive lock on lockPath via O_CREATE|O_EXCL.
// The returned release closure removes the lock file. Locks older than 30s
// are treated as stale and removed before retrying.
//
// Wait is bounded by defaultLockTimeout (5s) independently of a longer
// context deadline. That fail-fast bound is the production contract: another
// camp invocation is assumed to hold a short write. Tests that must wait
// through contention without measuring the scheduler should inject a
// liveness budget via AcquireFileLockTimeout.
func AcquireFileLock(ctx context.Context, lockPath string) (func(), error) {
	return AcquireFileLockTimeout(ctx, lockPath, defaultLockTimeout)
}

// AcquireFileLockTimeout is AcquireFileLock with an explicit wait bound.
// Production callers should keep using AcquireFileLock. A timeout of 0 or
// negative uses the 5s production default.
func AcquireFileLockTimeout(ctx context.Context, lockPath string, timeout time.Duration) (func(), error) {
	if timeout <= 0 {
		timeout = defaultLockTimeout
	}
	deadline := time.Now().Add(timeout)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		release, acquired, err := tryAcquireFileLock(lockPath)
		if err != nil {
			return nil, err
		}
		if acquired {
			return release, nil
		}
		if time.Now().After(deadline) {
			return nil, camperrors.Wrap(camperrors.ErrTimeout,
				"timeout acquiring lock at "+lockPath+" (another camp invocation holds it?)")
		}
		if f := onContendedLock; f != nil {
			f()
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(lockRetryDelay):
		}
	}
}

func tryAcquireFileLock(lockPath string) (func(), bool, error) {
	release, acquired, err := createFileLock(lockPath)
	if err != nil || acquired {
		return release, acquired, err
	}
	if removed, err := removeStaleLock(lockPath); err != nil {
		return nil, false, err
	} else if removed {
		return createFileLock(lockPath)
	}
	return nil, false, nil
}

func createFileLock(lockPath string) (func(), bool, error) {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err == nil {
		_ = f.Close()
		released := false
		return func() {
			if released {
				return
			}
			released = true
			_ = os.Remove(lockPath)
		}, true, nil
	}
	if errors.Is(err, fs.ErrExist) {
		return nil, false, nil
	}
	return nil, false, camperrors.Wrap(err, "acquire lock")
}

func removeStaleLock(lockPath string) (bool, error) {
	info, err := os.Stat(lockPath)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, nil
	}
	if time.Since(info.ModTime()) <= staleLockAfter {
		return false, nil
	}

	// Same-directory rename is the steal operation. Camp's file locks target
	// local filesystems; NFS-style distributed lock semantics are out of scope.
	dir := filepath.Dir(lockPath)
	stealPath := filepath.Join(dir, filepath.Base(lockPath)+".steal-"+randomHex(8))
	if err := os.Rename(lockPath, stealPath); err != nil {
		return false, nil
	}

	stolenInfo, err := os.Stat(stealPath)
	if err != nil || time.Since(stolenInfo.ModTime()) <= staleLockAfter {
		restoreStolenLock(lockPath, stealPath)
		return false, nil
	}
	if err := os.Remove(stealPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		restoreStolenLock(lockPath, stealPath)
		return false, nil
	}

	return true, nil
}

func restoreStolenLock(lockPath, stealPath string) {
	if _, err := os.Stat(lockPath); errors.Is(err, fs.ErrNotExist) {
		_ = os.Rename(stealPath, lockPath)
		return
	}
	_ = os.Remove(stealPath)
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b)
}
