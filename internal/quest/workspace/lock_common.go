package workspace

import (
	"io/fs"
	"os"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

func rejectSymlink(lockPath string) error {
	info, err := os.Lstat(lockPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return camperrors.Wrap(err, "stat ui-state.lock")
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return camperrors.Wrap(camperrors.ErrInvalidInput, "ui-state.lock must not be a symlink")
	}
	return nil
}
