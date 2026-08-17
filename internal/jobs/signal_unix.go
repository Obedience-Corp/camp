//go:build !windows

package jobs

import (
	"errors"
	"syscall"
)

// signalWorker asks a worker to stop, or makes it.
//
// The signal goes to the worker's process group when the worker leads one,
// which a detached worker always does: SpawnIfNeeded starts it with setsid, so
// its group id is its own pid and the group holds nothing but the worker and
// what the worker started. Signalling the group is what reaches a message
// writer the worker is waiting on.
//
// A worker that does not lead its own group is one somebody ran in a terminal
// by hand, and its group is the shell's job. Signalling that group would take
// out whatever else the user has in the same pipeline, so this signals the
// process alone and accepts that a writer it started may survive: killing a
// bystander is worse than leaving an orphan camp can see in `camp jobs`.
func signalWorker(pid int, force bool) error {
	sig := syscall.SIGTERM
	if force {
		sig = syscall.SIGKILL
	}
	if pgid, err := syscall.Getpgid(pid); err == nil && pgid == pid {
		return syscall.Kill(-pgid, sig)
	}
	return syscall.Kill(pid, sig)
}

// processAlive reports whether a pid still names a running process.
//
// Signal zero performs the existence check without delivering anything.
// EPERM counts as alive: the process is there, it simply belongs to somebody
// else, and reporting it gone would have camp announce a worker had stopped
// while it kept running.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
