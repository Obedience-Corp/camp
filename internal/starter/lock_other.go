//go:build !unix

package starter

import (
	"context"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

func lock(_ context.Context, _ string) (func(), error) {
	return nil, camperrors.New("automatic starter setup is supported on macOS and Linux")
}
