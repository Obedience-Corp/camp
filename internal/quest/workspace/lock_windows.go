//go:build windows

package workspace

import (
	"context"
	"os"
	"syscall"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"golang.org/x/sys/windows"
)

// acquireOSLock matches Rust File::try_lock on Windows (LockFileEx fail-immediately).
func acquireOSLock(ctx context.Context, lockPath string, wait time.Duration) (func(), error) {
	if wait <= 0 {
		wait = DefaultLockWait
	}
	if err := rejectSymlink(lockPath); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, camperrors.Wrap(err, "open ui-state.lock")
	}
	handle := windows.Handle(f.Fd())
	var ol windows.Overlapped
	deadline := time.Now().Add(wait)
	for {
		if err := ctx.Err(); err != nil {
			_ = f.Close()
			return nil, err
		}
		err := windows.LockFileEx(
			handle,
			windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
			0, 1, 0, &ol,
		)
		if err == nil {
			return func() {
				_ = windows.UnlockFileEx(handle, 0, 1, 0, &ol)
				_ = f.Close()
			}, nil
		}
		if err != syscall.Errno(windows.ERROR_LOCK_VIOLATION) {
			_ = f.Close()
			return nil, camperrors.Wrap(err, "lock ui-state.lock")
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, camperrors.Wrap(camperrors.ErrTimeout, "timeout acquiring ui-state.lock")
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}
