//go:build integration
// +build integration

package integration

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
