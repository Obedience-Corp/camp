//go:build !windows

package main

import camperrors "github.com/Obedience-Corp/camp/internal/errors"

var artifactOpenWindows = func(string) error {
	return camperrors.New("Windows artifact opener is unavailable on this platform")
}
