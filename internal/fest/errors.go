package fest

import "errors"

var (
	// ErrFestNotFound is returned when the fest CLI cannot be found.
	ErrFestNotFound = errors.New("fest CLI not found")
	// ErrFestNotExecutable is returned when fest is found but not executable.
	ErrFestNotExecutable = errors.New("fest CLI not executable")
	// ErrFestInitFailed is returned when fest initialization fails.
	ErrFestInitFailed = errors.New("fest init failed")
)
