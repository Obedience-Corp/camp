package transfer

import (
	"errors"
	"os/exec"
)

// asExitError is errors.As specialized to *exec.ExitError, kept separate so the
// fallback rule reads as one condition rather than three lines of plumbing.
func asExitError(err error, target **exec.ExitError) bool {
	return errors.As(err, target)
}

// ErrDestinationExists reports that a copy was declined because the destination
// already existed and --force was not given. It is a distinct error so the
// caller can phrase it as the deliberate no-clobber outcome rather than a
// transport failure.
var ErrDestinationExists = errors.New("destination already exists (use --force to overwrite)")
