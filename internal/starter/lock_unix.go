//go:build unix

package starter

import (
	"context"
	"os"
	"syscall"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// The kernel releases this lock on process exit. Setup can take longer than
// the short registry lock's stale threshold, so it must not use a timed lease.
func lock(ctx context.Context, path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, camperrors.Wrap(err, "open starter lock")
	}
	for {
		if err := ctx.Err(); err != nil {
			_ = f.Close()
			return nil, err
		}
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { _ = f.Close() }, nil
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
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
