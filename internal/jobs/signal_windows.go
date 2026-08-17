//go:build windows

package jobs

import (
	"os"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// signalWorker stops a worker.
//
// Windows has no signal with SIGTERM's meaning, so there is no polite half to
// this: a stop is a kill, and the worker never runs the cancel that would have
// taken its message writer down with it.
//
// This compiles but is not exercised: camp does not support Windows at runtime
// yet. It exists so `just windows` builds, and so whoever does the Windows port
// finds the difference recorded rather than assumed away.
func signalWorker(pid int, _ bool) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return camperrors.Wrapf(err, "find worker process %d", pid)
	}
	return p.Kill()
}

// processAlive reports whether a pid still names a running process.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Windows FindProcess opens a handle, so a successful open is the
	// existence check; releasing it immediately keeps this from leaking one
	// per poll.
	_ = p.Release()
	return true
}
