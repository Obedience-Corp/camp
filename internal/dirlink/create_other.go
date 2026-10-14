//go:build !windows

package dirlink

import (
	"os"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

func createPlatform(opts CreateOptions) error {
	target := opts.StoreTarget
	if target == "" {
		target = opts.TargetPath
	}
	if err := os.Symlink(target, opts.LinkPath); err != nil {
		return camperrors.Wrap(err, "create directory symlink")
	}
	return nil
}

func statIrregular(string, os.FileInfo) (Stat, error) {
	return Stat{}, nil
}
