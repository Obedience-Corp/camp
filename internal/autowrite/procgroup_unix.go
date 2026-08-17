//go:build !windows

package autowrite

import (
	"os/exec"
	"syscall"
)

// startInOwnProcessGroup puts the writer in a process group of its own.
//
// Setpgid rather than Setsid: the writer still belongs to the worker's
// session, so it dies with the session if the worker's whole session is torn
// down, but it has a group id of its own that a cancel can address without
// naming the worker that started it.
//
// The cost is deliberate and worth stating. A writer in its own group no
// longer receives a signal sent to the worker's group, so a worker killed
// outright with SIGKILL leaves the writer orphaned. Every path camp controls
// stops the worker with SIGTERM first precisely so its cancel runs and takes
// the writer down with it.
func startInOwnProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup kills the writer and everything it started.
//
// The negative pid is the point: the configured command runs through a login
// shell, so the writer camp can see is often a shell with the real tool
// underneath it. Killing the process alone leaves that tool running, holding
// whatever session or connection made it hang in the first place.
//
// SIGKILL rather than SIGTERM because this runs only after the deadline has
// already passed. A process that has produced nothing for the whole bound has
// not earned a second grace period, and the deferred path has nobody watching
// to notice if it ignored one.
func killProcessGroup(pid int) error {
	if pid <= 0 {
		return nil
	}
	return syscall.Kill(-pid, syscall.SIGKILL)
}
