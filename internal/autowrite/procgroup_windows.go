//go:build windows

package autowrite

import (
	"os"
	"os/exec"
	"syscall"
)

// startInOwnProcessGroup gives the writer a console process group of its own.
//
// CREATE_NEW_PROCESS_GROUP is the closest Windows equivalent that matters
// here, the same call internal/jobs makes when it detaches a worker.
//
// This compiles but is not exercised: camp does not support Windows at runtime
// yet. It exists so `just windows` builds, and so whoever does the Windows
// port finds the decision recorded rather than a syscall that does not exist.
func startInOwnProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

// killProcessGroup ends the writer.
//
// Windows has no "kill the group" call with the semantics the unix build
// relies on, so this stops the process camp started and leaves whatever it
// spawned to the port. Recorded rather than silently equivalent: on Windows
// the timeout bounds camp's wait, not necessarily the tool's life.
func killProcessGroup(pid int) error {
	if pid <= 0 {
		return nil
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}
