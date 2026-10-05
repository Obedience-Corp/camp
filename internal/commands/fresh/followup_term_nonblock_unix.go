//go:build !windows

package fresh

import (
	"os"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"golang.org/x/sys/unix"
)

// newFollowUpPTYMaster returns a separately owned, runtime-pollable descriptor.
// The caller retains ownership of master on both success and failure.
func newFollowUpPTYMaster(master *os.File) (*os.File, error) {
	raw, err := master.SyscallConn()
	if err != nil {
		return nil, err
	}
	duplicate := -1
	var duplicateErr error
	if err := raw.Control(func(fd uintptr) {
		duplicate, duplicateErr = unix.FcntlInt(fd, unix.F_DUPFD_CLOEXEC, 0)
	}); err != nil {
		if duplicate >= 0 {
			_ = unix.Close(duplicate)
		}
		return nil, err
	}
	if duplicateErr != nil {
		return nil, duplicateErr
	}
	// os.NewFile decides whether to register with Go polling at construction.
	// Darwin and FreeBSD's original PTY wrappers were constructed blocking.
	if err := unix.SetNonblock(duplicate, true); err != nil {
		_ = unix.Close(duplicate)
		return nil, err
	}
	replacement := os.NewFile(uintptr(duplicate), master.Name())
	if replacement == nil {
		_ = unix.Close(duplicate)
		return nil, camperrors.New("wrapping follow-up terminal descriptor")
	}
	// Fail before starting the child if runtime polling could not be enabled.
	if err := replacement.SetReadDeadline(time.Time{}); err != nil {
		_ = replacement.Close()
		return nil, err
	}
	return replacement, nil
}
