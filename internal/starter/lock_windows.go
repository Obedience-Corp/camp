//go:build windows

package starter

import (
	"context"
	"os"
	"syscall"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"golang.org/x/sys/windows"
)

// The kernel releases this lock when the handle closes on process exit. Setup
// can take longer than the short registry lock's stale threshold, so it must
// not use a timed lease.
func lock(ctx context.Context, path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, camperrors.Wrap(err, "open starter lock")
	}
	handle := windows.Handle(f.Fd())
	var ol windows.Overlapped
	for {
		if err := ctx.Err(); err != nil {
			_ = f.Close()
			return nil, err
		}
		err := windows.LockFileEx(
			handle,
			windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
			0,
			1,
			0,
			&ol,
		)
		if err == nil {
			return func() {
				_ = windows.UnlockFileEx(handle, 0, 1, 0, &ol)
				_ = f.Close()
			}, nil
		}
		if err != syscall.Errno(windows.ERROR_LOCK_VIOLATION) {
			_ = f.Close()
			return nil, camperrors.Wrap(err, "lock starter setup")
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}
