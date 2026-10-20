//go:build unix

package workspace

import (
	"context"
	"os"
	"syscall"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// acquireOSLock takes an exclusive advisory lock matching Rust
// std::fs::File::try_lock (flock LOCK_EX|LOCK_NB), with bounded wait and
// context cancellation. The returned release closes the file (kernel unlock).
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
	deadline := time.Now().Add(wait)
	for {
		if err := ctx.Err(); err != nil {
			_ = f.Close()
			return nil, err
		}
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { _ = f.Close() }, nil
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
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
