//go:build windows

package jobs

import (
	"os/exec"
	"syscall"
)

// detachProcess starts the worker without a console of its own.
//
// Windows has no setsid. CREATE_NEW_PROCESS_GROUP is the closest equivalent
// that matters here: it stops the child from receiving the parent console's
// Ctrl+C, so closing the terminal does not kill a worker mid-commit.
//
// This compiles but is not exercised: camp does not support Windows at runtime
// yet. It exists so `just windows` builds, and so whoever does the Windows port
// finds the detach decision recorded rather than a syscall that does not exist.
func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}
