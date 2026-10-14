//go:build container_fs && windows

package flow

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestPowerShellFlowCancellationStopsDescendant(t *testing.T) {
	root := t.TempDir()
	childPIDFile := filepath.Join(root, "child.pid")
	completedFile := filepath.Join(root, "completed.txt")
	f := Flow{
		Command: `$payload = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes('Start-Sleep -Seconds 30')); $child = Start-Process -FilePath (Get-Command powershell.exe).Source -ArgumentList @('-NoProfile', '-NonInteractive', '-EncodedCommand', $payload) -PassThru; [IO.File]::WriteAllText($env:FLOW_CHILD_PID_FILE, [string]$child.Id); Start-Sleep -Seconds 30; [IO.File]::WriteAllText($env:FLOW_COMPLETED_FILE, 'complete')`,
		Env: map[string]string{
			"FLOW_CHILD_PID_FILE": childPIDFile,
			"FLOW_COMPLETED_FILE": completedFile,
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- NewRunner(root).Run(ctx, f, nil) }()

	var childPID int
	deadline := time.After(10 * time.Second)
	for childPID == 0 {
		data, err := os.ReadFile(childPIDFile)
		if err == nil {
			childPID, err = strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				t.Fatalf("invalid child PID %q: %v", data, err)
			}
			break
		}
		if !os.IsNotExist(err) {
			t.Fatalf("read child PID: %v", err)
		}
		select {
		case err := <-done:
			t.Fatalf("flow exited before child started: %v", err)
		case <-deadline:
			t.Fatal("flow did not start a child within 10 seconds")
		case <-time.After(25 * time.Millisecond):
		}
	}
	defer exec.Command("taskkill.exe", "/PID", strconv.Itoa(childPID), "/T", "/F").Run()
	alive, err := windowsProcessAlive(childPID)
	if err != nil || !alive {
		t.Fatalf("child %d was not live before cancellation: alive=%t err=%v", childPID, alive, err)
	}

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled flow error = %v, want context.Canceled", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("cancelled flow did not return within 10 seconds")
	}

	stopDeadline := time.Now().Add(3 * time.Second)
	for {
		alive, err = windowsProcessAlive(childPID)
		if err != nil {
			t.Fatalf("inspect child %d after cancellation: %v", childPID, err)
		}
		if !alive {
			break
		}
		if time.Now().After(stopDeadline) {
			t.Fatalf("PowerShell descendant %d survived flow cancellation", childPID)
		}
		time.Sleep(25 * time.Millisecond)
	}
	if _, err := os.Stat(completedFile); !os.IsNotExist(err) {
		t.Fatalf("cancelled flow wrote completion state: stat error = %v", err)
	}
}

func windowsProcessAlive(pid int) (bool, error) {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer windows.CloseHandle(handle)
	state, err := windows.WaitForSingleObject(handle, 0)
	if err != nil {
		return false, err
	}
	switch state {
	case windows.WAIT_OBJECT_0:
		return false, nil
	case uint32(windows.WAIT_TIMEOUT):
		return true, nil
	default:
		return false, errors.New("unexpected process wait state")
	}
}
