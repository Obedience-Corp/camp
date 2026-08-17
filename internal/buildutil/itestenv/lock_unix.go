//go:build unix

package itestenv

import (
	"os"
	"syscall"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// lockFileNB takes an exclusive advisory lock without blocking. flock is used
// rather than a pid file because the kernel releases it when the holder exits,
// however it exits: a suite killed with Ctrl-C mid-run must not leave the next
// run waiting on a lock nobody holds.
func lockFileNB(file *os.File) error {
	err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	switch {
	case err == nil:
		return nil
	case camperrors.Is(err, syscall.EWOULDBLOCK):
		return errLockBusy
	default:
		return err
	}
}

func unlockFile(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}
