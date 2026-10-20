//go:build !unix && !windows

package workspace

import (
	"context"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

func acquireOSLock(ctx context.Context, lockPath string, wait time.Duration) (func(), error) {
	_ = ctx
	_ = lockPath
	_ = wait
	return nil, camperrors.Wrap(camperrors.ErrInvalidInput, "ui-state.lock OS locking is unsupported on this platform")
}
