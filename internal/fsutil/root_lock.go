package fsutil

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// AcquireRootFileLock takes an exclusive lock on name inside root via
// O_CREATE|O_EXCL. name is relative to root. The returned release closure
// removes the lock file. Locks older than 30s are treated as stale and removed
// before retrying.
//
// Wait is bounded by defaultLockTimeout (5s) independently of a longer context
// deadline, matching AcquireFileLock. Every open, stat, rename, and remove
// stays on root.
func AcquireRootFileLock(ctx context.Context, root *os.Root, name string) (func(), error) {
	deadline := time.Now().Add(defaultLockTimeout)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		release, acquired, err := tryAcquireRootFileLock(root, name)
		if err != nil {
			return nil, err
		}
		if acquired {
			return release, nil
		}
		if time.Now().After(deadline) {
			return nil, camperrors.Wrap(camperrors.ErrTimeout,
				"timeout acquiring lock at "+name+" (another camp invocation holds it?)")
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

func tryAcquireRootFileLock(root *os.Root, name string) (func(), bool, error) {
	release, acquired, err := createRootFileLock(root, name)
	if err != nil || acquired {
		return release, acquired, err
	}
	if removed, err := removeStaleRootLock(root, name); err != nil {
		return nil, false, err
	} else if removed {
		return createRootFileLock(root, name)
	}
	return nil, false, nil
}

func createRootFileLock(root *os.Root, name string) (func(), bool, error) {
	f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err == nil {
		owned, statErr := f.Stat()
		_ = f.Close()
		if statErr != nil {
			return nil, false, camperrors.Wrap(statErr, "stat acquired lock")
		}
		stop, done := make(chan struct{}), make(chan struct{})
		go func() {
			defer close(done)
			ticker := time.NewTicker(staleLockAfter / 3)
			defer ticker.Stop()
			for {
				select {
				case <-stop:
					return
				case now := <-ticker.C:
					if current, err := root.Lstat(name); err == nil && os.SameFile(owned, current) {
						_ = root.Chtimes(name, now, now)
					}
				}
			}
		}()
		released := false
		return func() {
			if released {
				return
			}
			released = true
			close(stop)
			<-done
			if current, err := root.Lstat(name); err == nil && os.SameFile(owned, current) {
				_ = root.Remove(name)
			}
		}, true, nil
	}
	if errors.Is(err, fs.ErrExist) {
		return nil, false, nil
	}
	return nil, false, camperrors.Wrap(err, "acquire lock")
}

func removeStaleRootLock(root *os.Root, name string) (bool, error) {
	info, err := root.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, nil
	}
	if !info.Mode().IsRegular() || info.Size() != 0 {
		return false, camperrors.New("refusing to remove an unrecognized lock file")
	}
	if time.Since(info.ModTime()) <= staleLockAfter {
		return false, nil
	}

	// Same-directory rename is the steal operation. Camp's file locks target
	// local filesystems; NFS-style distributed lock semantics are out of scope.
	stealName := rootSibling(name, ".steal-"+randomHex(8))
	if err := root.Rename(name, stealName); err != nil {
		return false, nil
	}

	stolenInfo, err := root.Lstat(stealName)
	if err != nil || time.Since(stolenInfo.ModTime()) <= staleLockAfter {
		restoreStolenRootLock(root, name, stealName)
		return false, nil
	}
	if err := root.Remove(stealName); err != nil && !errors.Is(err, fs.ErrNotExist) {
		restoreStolenRootLock(root, name, stealName)
		return false, nil
	}
	return true, nil
}

func restoreStolenRootLock(root *os.Root, name, stealName string) {
	if _, err := root.Lstat(name); errors.Is(err, fs.ErrNotExist) {
		_ = root.Rename(stealName, name)
		return
	}
	_ = root.Remove(stealName)
}

// rootSibling is base+suffix in the same relative directory as name.
func rootSibling(name, suffix string) string {
	dir, base := path.Split(name)
	sibling := base + suffix
	if dir == "" {
		return sibling
	}
	return path.Join(path.Clean(dir), sibling)
}
