//go:build integration && !windows

package workitem

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/stretchr/testify/require"
)

func TestIntegration_WorkitemEditorHandsOffTTY(t *testing.T) {
	projectRoot := testProjectRoot(t)
	campBinary := buildCampBinary(t, projectRoot)

	tempRoot := t.TempDir()
	homeDir := filepath.Join(tempRoot, "home")
	require.NoError(t, os.MkdirAll(filepath.Join(homeDir, ".config"), 0o755))

	baseEnv := append(os.Environ(),
		"HOME="+homeDir,
		"XDG_CONFIG_HOME="+filepath.Join(homeDir, ".config"),
		"TERM=dumb",
		"NO_COLOR=1",
		// Test-only escape hatch — bypass Festival Methodology init so
		// this test does not depend on the fest CLI being installed.
		"CAMP_INIT_SKIP_FEST=1",
	)

	campaignRoot := filepath.Join(tempRoot, "campaign")
	runCommand(t, projectRoot, baseEnv, campBinary,
		"init", campaignRoot,
		"--name", "TTY Workitem Test",
		"--type", "product",
		"-d", "TTY integration test campaign",
		"-m", "Verify workitem editor handoff",
		"--force",
		"--no-register",
		"--no-git",
	)
	runCommand(t, campaignRoot, baseEnv, campBinary,
		"intent", "add", "TTY editor integration intent", "--no-commit",
	)

	intentDoc := findIntentDoc(t, campaignRoot)
	editorLog := filepath.Join(tempRoot, "editor.log")
	editorScript := writeTTYEditorScript(t, tempRoot, editorLog)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, campBinary, "--no-color", "workitem")
	cmd.Dir = campaignRoot
	cmd.Env = append(baseEnv, "EDITOR="+editorScript)

	ptmx, err := pty.Start(cmd)
	require.NoError(t, err)
	require.NoError(t, pty.Setsize(ptmx, &pty.Winsize{Rows: 40, Cols: 120}))

	t.Cleanup(func() {
		_ = ptmx.Close()
		killProcessGroup(cmd)
	})

	var output lockedBuffer
	readerDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(&output, ptmx)
		close(readerDone)
	}()

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	waitForOutput(t, &output, "TTY editor integration intent", 5*time.Second)

	_, err = ptmx.Write([]byte("e"))
	require.NoError(t, err)
	waitForOutput(t, &output, "EDITOR_START", 5*time.Second)

	_, err = ptmx.Write([]byte("hello-from-editor\r"))
	require.NoError(t, err)

	waitForFileContains(t, editorLog, "INPUT=hello-from-editor", 5*time.Second)
	waitForOutput(t, &output, "EDITOR_DONE", 5*time.Second)

	logData, err := os.ReadFile(editorLog)
	require.NoError(t, err)
	loggedPath := logFieldValue(t, string(logData), "PATH")
	require.Equal(t, canonicalPath(t, intentDoc), canonicalPath(t, loggedPath))

	intentBody, err := os.ReadFile(intentDoc)
	require.NoError(t, err)
	require.Contains(t, string(intentBody), "editor-input: hello-from-editor")

	_, err = ptmx.Write([]byte("q"))
	require.NoError(t, err)

	select {
	case err := <-waitCh:
		require.NoErrorf(t, err, "camp workitem exited unexpectedly\nterminal tail:\n%s", output.Tail(4000))
	case <-time.After(5 * time.Second):
		t.Fatalf("camp workitem did not exit after quitting\nterminal tail:\n%s", output.Tail(4000))
	}

	require.NoError(t, ptmx.Close())
	<-readerDone
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *lockedBuffer) Tail(max int) string {
	s := b.String()
	if len(s) <= max {
		return s
	}
	return s[len(s)-max:]
}

func waitForOutput(t *testing.T, output *lockedBuffer, want string, timeout time.Duration) {
	t.Helper()
	require.Eventuallyf(t, func() bool {
		return strings.Contains(output.String(), want)
	}, timeout, 25*time.Millisecond, "timed out waiting for %q\nterminal tail:\n%s", want, output.Tail(4000))
}

func waitForFileContains(t *testing.T, path, want string, timeout time.Duration) {
	t.Helper()
	require.Eventuallyf(t, func() bool {
		data, err := os.ReadFile(path)
		return err == nil && strings.Contains(string(data), want)
	}, timeout, 25*time.Millisecond, "timed out waiting for %q in %s", want, path)
}

func writeTTYEditorScript(t *testing.T, dir, logPath string) string {
	t.Helper()

	scriptPath := filepath.Join(dir, "tty-editor.sh")
	script := fmt.Sprintf(`#!/bin/sh
set -eu

log_file=%s

printf 'PATH=%%s\n' "$1" > "$log_file"
printf 'EDITOR_START\n'

IFS= read -r line < /dev/tty

printf 'INPUT=%%s\n' "$line" >> "$log_file"
printf '\neditor-input: %%s\n' "$line" >> "$1"
printf 'EDITOR_DONE\n'
`, shellQuote(logPath))

	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o755))
	return scriptPath
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

func findIntentDoc(t *testing.T, campaignRoot string) string {
	t.Helper()

	matches, err := filepath.Glob(filepath.Join(campaignRoot, ".campaign", "intents", "*", "*.md"))
	require.NoError(t, err)
	require.Len(t, matches, 1)
	return matches[0]
}

func logFieldValue(t *testing.T, logData, key string) string {
	t.Helper()

	prefix := key + "="
	for _, line := range strings.Split(logData, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix)
		}
	}

	t.Fatalf("missing %s in editor log:\n%s", key, logData)
	return ""
}

func canonicalPath(t *testing.T, path string) string {
	t.Helper()

	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("resolve path %s: %v", path, err)
	}
	return resolved
}

func runCommand(t *testing.T, dir string, env []string, name string, args ...string) string {
	t.Helper()

	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = env

	output, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "%s %v failed:\n%s", name, args, output)
	return string(output)
}

func buildCampBinary(t *testing.T, projectRoot string) string {
	t.Helper()

	binaryPath := filepath.Join(t.TempDir(), "camp")
	cmd := exec.Command("go", "build", "-tags=dev", "-o", binaryPath, "./cmd/camp")
	cmd.Dir = projectRoot
	output, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "go build failed:\n%s", output)
	return binaryPath
}

func testProjectRoot(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "../../.."))
}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil || cmd.ProcessState != nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	_ = cmd.Process.Kill()
}
