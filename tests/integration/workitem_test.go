//go:build integration
// +build integration

package integration

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/creack/pty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_WorkitemPrintUsesRelativeJumpPath(t *testing.T) {
	tc := GetSharedContainer(t)

	const campaignDir = "/test/workitem-print"
	_, err := tc.RunCamp(
		"init", campaignDir,
		"--name", "Workitem Print Test",
		"--type", "product",
		"-d", "Workitem print test campaign",
		"-m", "Verify relative workitem path output",
		"--force",
		"--no-register",
		"--no-git",
	)
	require.NoError(t, err, "camp init should succeed")

	_, err = tc.RunCampInDir(campaignDir,
		"intent", "add", "Relative print intent", "--no-commit",
	)
	require.NoError(t, err, "camp intent add should succeed")

	output, err := tc.RunCampInDir(campaignDir, "--no-color", "workitem", "--print")
	require.NoError(t, err, "camp workitem --print should succeed")

	got := strings.TrimSpace(output)
	assert.Equal(t, ".campaign/intents/inbox", got)
	assert.NotContains(t, got, campaignDir)
	assert.False(t, strings.HasPrefix(got, "/"), "workitem --print should not emit absolute paths")
}

func TestIntegration_WorkitemShellWrapperChangesDirectory(t *testing.T) {
	tc := GetSharedContainer(t)
	installShells(t, tc)
	ensureCampInPath(t, tc)

	const campaignDir = "/test/workitem-shell-jump"
	_, err := tc.RunCamp(
		"init", campaignDir,
		"--name", "Workitem Shell Jump Test",
		"--type", "product",
		"-d", "Workitem shell jump test campaign",
		"-m", "Verify workitem shell wrapper jumps",
		"--force",
		"--no-register",
		"--no-git",
	)
	require.NoError(t, err, "camp init should succeed")

	output, exitCode, err := tc.ExecCommand("sh", "-lc", fmt.Sprintf(
		"mkdir -p %s/workflow/design/shell-jump-design && printf '# Shell Jump Design\n' > %s/workflow/design/shell-jump-design/README.md",
		shellQuote(campaignDir),
		shellQuote(campaignDir),
	))
	require.NoError(t, err, "create design work item")
	require.Equal(t, 0, exitCode, "create design work item failed:\n%s", output)

	script := fmt.Sprintf(`
set -e
export PATH="/camp-bin:$PATH"
cd %s
eval "$(camp shell-init bash)"
camp workitem
printf '\nPWD_AFTER=%%s\n' "$PWD"
`, shellQuote(campaignDir))

	output, err = runInteractiveShellSteps(t, tc, script, []InteractiveStep{
		{WaitFor: "Shell Jump Design", Input: "\r"},
		{WaitFor: "PWD_AFTER="},
	})
	require.NoError(t, err, "shell-integrated camp workitem should succeed; output:\n%s", output)
	assert.Contains(t, output, "PWD_AFTER="+campaignDir+"/workflow/design/shell-jump-design")
}

func TestIntegration_WorkitemShellWrapperEnterOpensIntentEditor(t *testing.T) {
	tc := GetSharedContainer(t)
	installShells(t, tc)
	ensureCampInPath(t, tc)

	const (
		campaignDir = "/test/workitem-shell-open-intent"
		editorPath  = "/test/enter-intent-editor.sh"
		editorLog   = "/test/enter-intent-editor.log"
	)
	_, err := tc.RunCamp(
		"init", campaignDir,
		"--name", "Workitem Shell Open Intent Test",
		"--type", "product",
		"-d", "Workitem shell open intent test campaign",
		"-m", "Verify workitem shell wrapper opens intents",
		"--force",
		"--no-register",
		"--no-git",
	)
	require.NoError(t, err, "camp init should succeed")

	_, err = tc.RunCampInDir(campaignDir,
		"intent", "add", "Enter opens intent", "--no-commit",
	)
	require.NoError(t, err, "camp intent add should succeed")

	intentDoc := findContainerIntentDoc(t, tc, campaignDir)

	editorScript := fmt.Sprintf(`#!/bin/sh
set -eu

log_file=%s

printf 'PATH=%%s\n' "$1" > "$log_file"
printf 'EDITOR_START\n'
printf '\nenter-editor-opened\n' >> "$1"
printf 'EDITOR_DONE\n'
`, shellQuote(editorLog))

	require.NoError(t, tc.WriteFile(editorPath, editorScript))
	tc.Shell(t, fmt.Sprintf("chmod +x %s", editorPath))

	script := fmt.Sprintf(`
set -e
export PATH="/camp-bin:$PATH"
export EDITOR=%s
cd %s
eval "$(camp shell-init bash)"
camp workitem
printf '\nPWD_AFTER=%%s\n' "$PWD"
`, shellQuote(editorPath), shellQuote(campaignDir))

	output, err := runInteractiveShellSteps(t, tc, script, []InteractiveStep{
		{WaitFor: "Enter opens intent", Input: "\r"},
		{WaitFor: "EDITOR_DONE"},
		{WaitFor: "PWD_AFTER="},
	})
	require.NoError(t, err, "shell-integrated camp workitem should open intent; output:\n%s", output)
	assert.Contains(t, output, "PWD_AFTER="+campaignDir)
	assert.NotContains(t, output, "PWD_AFTER="+campaignDir+"/.campaign/intents/inbox")

	logData, err := tc.ReadFile(editorLog)
	require.NoError(t, err, "editor log should exist after editor exits")
	assert.Equal(t, intentDoc, logFieldValue(t, logData, "PATH"))

	intentBody, err := tc.ReadFile(intentDoc)
	require.NoError(t, err)
	assert.Contains(t, intentBody, "enter-editor-opened")
}

// TestIntegration_WorkitemEditorHandsOffTTY verifies that `camp workitem`
// hands the controlling TTY to $EDITOR cleanly: the editor receives the
// intent doc path on argv, can read user input from /dev/tty, and any
// content it appends shows up in the on-disk intent doc when the TUI
// resumes. Runs entirely inside the shared integration container so it
// does not touch the host filesystem.
func TestIntegration_WorkitemEditorHandsOffTTY(t *testing.T) {
	if !festAvailable {
		t.Skip("fest binary not available in container; skipping workitem TTY test")
	}

	tc := GetSharedContainer(t)

	const (
		campaignDir = "/test/workitem-tty"
		editorPath  = "/test/tty-editor.sh"
		editorLog   = "/test/editor.log"
	)

	_, err := tc.RunCamp(
		"init", campaignDir,
		"--name", "TTY Workitem Test",
		"--type", "product",
		"-d", "TTY integration test campaign",
		"-m", "Verify workitem editor handoff",
		"--force",
		"--no-register",
		"--no-git",
	)
	require.NoError(t, err, "camp init should succeed")

	_, err = tc.RunCampInDir(campaignDir,
		"intent", "add", "TTY editor integration intent", "--no-commit",
	)
	require.NoError(t, err, "camp intent add should succeed")

	intentDoc := findContainerIntentDoc(t, tc, campaignDir)

	editorScript := fmt.Sprintf(`#!/bin/sh
set -eu

log_file=%s

printf 'PATH=%%s\n' "$1" > "$log_file"
printf 'EDITOR_START\n'

IFS= read -r line < /dev/tty

printf 'INPUT=%%s\n' "$line" >> "$log_file"
printf '\neditor-input: %%s\n' "$line" >> "$1"
printf 'EDITOR_DONE\n'
`, shellQuote(editorLog))

	require.NoError(t, tc.WriteFile(editorPath, editorScript))
	tc.Shell(t, fmt.Sprintf("chmod +x %s", editorPath))

	output, err := tc.RunCampInteractiveStepsInDirWithEnv(
		campaignDir,
		map[string]string{"EDITOR": editorPath},
		[]InteractiveStep{
			{WaitFor: "TTY editor integration intent", Input: "e"},
			{WaitFor: "EDITOR_START", Input: "hello-from-editor\r"},
			{WaitFor: "EDITOR_DONE", Input: "q"},
		},
		"--no-color", "workitem",
	)
	require.NoError(t, err, "camp workitem TTY flow should succeed; output:\n%s", output)

	logData, err := tc.ReadFile(editorLog)
	require.NoError(t, err, "editor log should exist after editor exits")

	loggedPath := logFieldValue(t, logData, "PATH")
	assert.Equal(t, intentDoc, loggedPath,
		"editor should receive the intent doc path on argv[1]")
	loggedInput := logFieldValue(t, logData, "INPUT")
	assert.Equal(t, "hello-from-editor", loggedInput,
		"editor should read input from the controlling TTY")

	intentBody, err := tc.ReadFile(intentDoc)
	require.NoError(t, err)
	assert.Contains(t, intentBody, "editor-input: hello-from-editor",
		"intent doc should reflect content the editor appended")
}

// findContainerIntentDoc returns the single intent file under the
// campaign's .campaign/intents/ tree. Fails the test if zero or more
// than one match is found.
func findContainerIntentDoc(t *testing.T, tc *TestContainer, campaignDir string) string {
	t.Helper()

	output, exitCode, err := tc.ExecCommand("sh", "-lc",
		fmt.Sprintf("ls %s/.campaign/intents/*/*.md", campaignDir))
	require.NoError(t, err, "list intent docs")
	require.Equal(t, 0, exitCode, "list intent docs failed:\n%s", output)

	matches := strings.Fields(strings.TrimSpace(output))
	require.Len(t, matches, 1, "expected exactly one intent doc, got: %v", matches)
	return matches[0]
}

// logFieldValue returns the value for `<key>=` from the editor log content.
// Fails the test if the key is missing.
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

func runInteractiveShellSteps(t *testing.T, tc *TestContainer, script string, steps []InteractiveStep) (string, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(tc.ctx, 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "exec", "-i", "-t", tc.container.GetContainerID(), "bash", "-lc", script)
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 40, Cols: 120})
	if err != nil {
		return "", camperrors.Wrap(err, "failed to start interactive shell")
	}
	defer func() { _ = ptmx.Close() }()

	var output lockedBuffer
	readerDone := make(chan struct{})
	go func() {
		_, _ = copyTerminalOutput(ptmx, &output)
		close(readerDone)
	}()

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	waitStart := 0
	for _, step := range steps {
		if step.WaitFor != "" {
			if err := waitForBufferContainsAfter(&output, step.WaitFor, waitStart, 5*time.Second); err != nil {
				if cmd.Process != nil {
					_ = cmd.Process.Kill()
				}
				select {
				case <-readerDone:
				case <-time.After(time.Second):
				}
				return output.String(), camperrors.Wrapf(err, "interactive shell did not reach %q\nterminal tail:\n%s", step.WaitFor, output.Tail(4000))
			}
		} else {
			time.Sleep(250 * time.Millisecond)
		}

		if step.Input != "" {
			waitStart = output.Len()
			if err := writeInteractiveInput(ptmx, step.Input); err != nil {
				if cmd.Process != nil {
					_ = cmd.Process.Kill()
				}
				select {
				case <-readerDone:
				case <-time.After(time.Second):
				}
				return output.String(), camperrors.Wrapf(err, "failed to send interactive input\nterminal tail:\n%s", output.Tail(4000))
			}
		}
	}

	var waitErr error
	select {
	case waitErr = <-waitCh:
	case <-ctx.Done():
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		waitErr = ctx.Err()
	}

	select {
	case <-readerDone:
	case <-time.After(time.Second):
	}

	if waitErr != nil {
		return output.String(), camperrors.Wrapf(waitErr, "interactive shell failed\nterminal tail:\n%s", output.Tail(4000))
	}

	return output.String(), nil
}
