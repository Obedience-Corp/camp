//go:build !windows

package autowrite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Tagged off Windows because it asserts on process-group semantics camp does
// not have there: the Windows build kills the writer alone and records that it
// cannot promise more, so asserting the grandchild dies would assert a promise
// this platform never made.

// Killing the writer has to kill what the writer started.
//
// The configured command runs through a login shell, which may or may not exec
// the tool it was given, so the process camp can see is often not the process
// doing the work. Signalling that one alone leaves an `ob commit` holding an
// LLM session with nobody left to collect it, which is the same leak the
// timeout was added to stop, one process further down.
func TestWriterTimeoutKillsWhatTheWriterStarted(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")

	// A grandchild in its own right: backgrounded by the shell, so killing the
	// shell alone leaves it running.
	command := "sh -c 'echo $$ > " + pidFile + "; sleep 30' & sleep 30"

	_, err := RunCommitMessageCommandWithOptions(context.Background(), ".", command, RunOptions{
		Timeout:         300 * time.Millisecond,
		OwnProcessGroup: true,
	})
	var timeoutErr *TimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("error = %v, want a *TimeoutError", err)
	}

	pid := readPIDFile(t, pidFile)
	if pid == 0 {
		t.Skip("the writer never recorded a grandchild pid; nothing to assert")
	}
	// Give the group kill a moment to be reaped before asking.
	for range 40 {
		if !alive(pid) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	// Do not leave it behind on failure.
	_ = syscall.Kill(pid, syscall.SIGKILL)
	t.Fatalf("the writer's grandchild (pid %d) outlived the timeout; killing the "+
		"shell alone leaves the real tool running", pid)
}

func readPIDFile(t *testing.T, path string) int {
	t.Helper()
	for range 40 {
		data, err := os.ReadFile(path)
		if err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
				return pid
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	return 0
}

func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
