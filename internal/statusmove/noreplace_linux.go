//go:build linux

package statusmove

import (
	"errors"

	"golang.org/x/sys/unix"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

func noReplaceMove(src, dst string) error {
	err := unix.Renameat2(unix.AT_FDCWD, src, unix.AT_FDCWD, dst, unix.RENAME_NOREPLACE)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, unix.EEXIST), errors.Is(err, unix.ENOTEMPTY):
		return ErrAlreadyExists
	case errors.Is(err, unix.EXDEV):
		return crossDeviceMove(src, dst)
	default:
		return camperrors.Wrapf(err, "rename no-replace %s -> %s", src, dst)
	}
}
