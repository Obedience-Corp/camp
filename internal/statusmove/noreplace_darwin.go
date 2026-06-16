//go:build darwin

package statusmove

import (
	"errors"

	"golang.org/x/sys/unix"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

func noReplaceMove(src, dst string) error {
	// Darwin has no Linux-style renameat2(RENAME_NOREPLACE). The kernel
	// exposes renameatx_np(RENAME_EXCL), which x/sys/unix wraps and which
	// handles both files and directories. A link+unlink fallback gives atomic
	// file exclusivity but cannot move directory workflow items.
	err := unix.RenameatxNp(unix.AT_FDCWD, src, unix.AT_FDCWD, dst, unix.RENAME_EXCL)
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
