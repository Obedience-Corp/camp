//go:build !windows

package jobs

import (
	"os/exec"
	"syscall"
)

// detachProcess puts the worker in its own session.
//
// Setsid is what makes the child outlive the command that spawned it, which is
// the entire point of deferring: closing the shell must not take the worker
// with it. It lives behind a build tag because syscall.SysProcAttr has no such
// field on Windows, and camp has a real `just windows` target plus windows in
// its platforms aggregate, so referencing it unguarded broke a build the repo
// actually runs even though Windows is not a supported runtime yet.
func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
