//go:build !linux && !darwin

package statusmove

import (
	"os"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

func noReplaceMove(src, dst string) error {
	// Best-effort fallback for platforms without atomic no-replace rename.
	// The final copy/open path still refuses to replace existing files, but
	// directory moves retain a pre-check window on these platforms.
	if _, err := os.Lstat(dst); err == nil {
		return ErrAlreadyExists
	} else if err != nil && !os.IsNotExist(err) {
		return camperrors.Wrapf(err, "checking destination %s", dst)
	}
	return crossDeviceMove(src, dst)
}
