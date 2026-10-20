//go:build container_fs && !windows

package plugin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// TestExecute_ContextCancelPreservesPluginExit proves Ctrl+C parity for a
// responsive plugin: context cancel sends Interrupt, the plugin finalizes and
// exits 6, and Execute reports that exit (not -1/255 from default Kill).
func TestExecute_ContextCancelPreservesPluginExit(t *testing.T) {
	dir := t.TempDir()
	ready := filepath.Join(dir, "ready")
	finalized := filepath.Join(dir, "finalized")
	bin := filepath.Join(dir, "camp-graceful")
	script := `#!/bin/sh
set -eu
ready=` + shellQuote(ready) + `
final=` + shellQuote(finalized) + `
trap 'printf done > "$final"; exit 6' INT
printf '%s' "$$" > "$ready"
while :; do sleep 0.05; done
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	prev := pluginCancelGrace
	pluginCancelGrace = 2 * time.Second
	t.Cleanup(func() { pluginCancelGrace = prev })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var runErr error
	var done atomic.Bool
	go func() {
		runErr = Execute(ctx, Plugin{Name: "graceful", Path: bin}, nil, dir)
		done.Store(true)
	}()

	waitForFile(t, ready, 3*time.Second)
	cancel()

	deadline := time.Now().Add(3 * time.Second)
	for !done.Load() {
		if time.Now().After(deadline) {
			t.Fatal("Execute did not return after cancel")
		}
		time.Sleep(20 * time.Millisecond)
	}

	waitForFile(t, finalized, time.Second)
	var cmdErr *camperrors.CommandError
	if !errors.As(runErr, &cmdErr) {
		t.Fatalf("error = %v (%T), want CommandError", runErr, runErr)
	}
	if cmdErr.ExitCode != 6 {
		t.Fatalf("ExitCode = %d, want 6 (plugin finalize), not Kill/-1", cmdErr.ExitCode)
	}
}

// TestExecute_UnresponsivePluginForcedCleanup proves a plugin that ignores
// Interrupt is killed after the grace bound and does not linger.
func TestExecute_UnresponsivePluginForcedCleanup(t *testing.T) {
	dir := t.TempDir()
	ready := filepath.Join(dir, "ready")
	bin := filepath.Join(dir, "camp-wedged")
	script := `#!/bin/sh
set -eu
trap '' INT TERM
printf '%s' "$$" > ` + shellQuote(ready) + `
while :; do sleep 0.1; done
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	prev := pluginCancelGrace
	pluginCancelGrace = 200 * time.Millisecond
	t.Cleanup(func() { pluginCancelGrace = prev })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- Execute(ctx, Plugin{Name: "wedged", Path: bin}, nil, dir)
	}()

	pid := waitForPIDFile(t, ready, 3*time.Second)
	cancel()

	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Fatal("wedged plugin was not force-cleaned within bound")
	}

	deadline := time.Now().Add(time.Second)
	for processAlive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("pid %d still alive after forced cleanup", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitForFile(t *testing.T, path string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("file %s not created within %s", path, within)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitForPIDFile(t *testing.T, path string, within time.Duration) int {
	t.Helper()
	waitForFile(t, path, within)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 1 {
		t.Fatalf("pid file %s: %q", path, data)
	}
	return pid
}

func shellQuote(path string) string {
	return "'" + path + "'"
}

func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}
