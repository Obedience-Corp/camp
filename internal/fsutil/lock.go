package fsutil

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

const (
	defaultLockTimeout = 5 * time.Second
	staleLockAfter     = 30 * time.Second
	lockRetryDelay     = 20 * time.Millisecond
)

// AcquireFileLock takes an exclusive lock on lockPath via O_CREATE|O_EXCL.
// The returned release closure removes the lock file. Locks older than 30s
// are treated as stale and removed before retrying.
func AcquireFileLock(ctx context.Context, lockPath string) (func(), error) {
	deadline := time.Now().Add(defaultLockTimeout)
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
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(lockRetryDelay):
		}
	}
}

func tryAcquireFileLock(lockPath string) (func(), bool, error) {
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
	if !errors.Is(err, fs.ErrExist) {
		return nil, false, camperrors.Wrap(err, "acquire lock")
	}
	if removed, err := removeStaleLock(lockPath); err != nil {
		return nil, false, err
	} else if removed {
		return nil, false, nil
	}
	return nil, false, nil
}

func removeStaleLock(lockPath string) (bool, error) {
	info, err := os.Stat(lockPath)
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, camperrors.Wrap(err, "stat lock")
	}
	if time.Since(info.ModTime()) <= staleLockAfter {
		return false, nil
	}
	if err := os.Remove(lockPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, camperrors.Wrap(err, "remove stale lock")
	}
	return true, nil
}
