package intent

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

// TestQuietSlogDuringTUI_RedirectsToLogFile asserts that slog.Default no
// longer points at stderr after quietSlogDuringTUI runs, and that messages
// emitted while the swap is active land in the campaign log file instead of
// the user-facing TTY.
//
// Regression test for bug 003: auto-commit INFO logs (now Debug) used to
// collide with bubbletea rendering on the same TTY. Even after demoting the
// known noise to Debug, this swap is the defense-in-depth that prevents any
// future INFO emission from anywhere in the call chain from corrupting the
// TUI render frame.
func TestQuietSlogDuringTUI_RedirectsToLogFile(t *testing.T) {
	campaignRoot := t.TempDir()
	previousDefault := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previousDefault) })

	// Capture stderr while the swap is active. If any slog call leaks to
	// stderr, this buffer would not be empty (we cannot literally redirect
	// os.Stderr here without affecting the test runner, so we assert on the
	// handler identity instead: the swapped default must NOT be the previous
	// default that was pointing at stderr).
	stderrBuf := &bytes.Buffer{}
	_ = stderrBuf // kept for parity; identity assertion below is the real check

	restore, err := quietSlogDuringTUI(campaignRoot)
	if err != nil {
		t.Fatalf("quietSlogDuringTUI returned error: %v", err)
	}
	t.Cleanup(restore)

	if slog.Default() == previousDefault {
		t.Fatal("slog.Default was not swapped; TUI would still leak logs to stderr")
	}

	// Emit at every level. None should hit stderr.
	slog.Debug("debug-from-test", "key", "value")
	slog.Info("info-from-test", "key", "value")
	slog.Warn("warn-from-test", "key", "value")
	slog.Error("error-from-test", "key", "value")

	// Restore must put the previous default back so non-TUI code paths are
	// unaffected.
	restore()
	if slog.Default() != previousDefault {
		t.Fatal("restore() did not reinstall the previous slog.Default")
	}

	// The log file should exist and contain the messages we emitted.
	logPath := filepath.Join(campaignRoot, ".campaign", "logs", "intent-explore.log")
	contents, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected log file at %s, got error: %v", logPath, err)
	}
	body := string(contents)

	wantSubstrings := []string{
		"debug-from-test",
		"info-from-test",
		"warn-from-test",
		"error-from-test",
	}
	for _, want := range wantSubstrings {
		if !bytes.Contains(contents, []byte(want)) {
			t.Errorf("log file missing %q. body=%q", want, body)
		}
	}
}

// TestQuietSlogDuringTUI_FallsBackToDiscardOnDirError verifies that if the
// log directory cannot be created (e.g. permission denied on a read-only
// campaign), the function still installs a quiet handler rather than letting
// slog continue to write to stderr.
func TestQuietSlogDuringTUI_FallsBackToDiscardOnDirError(t *testing.T) {
	// Create a regular file where .campaign would need to live; MkdirAll
	// will fail because the parent path component is not a directory.
	parent := t.TempDir()
	blocker := filepath.Join(parent, ".campaign")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("preparing blocker file: %v", err)
	}

	previousDefault := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previousDefault) })

	restore, err := quietSlogDuringTUI(parent)
	if err != nil {
		t.Fatalf("quietSlogDuringTUI returned error on fallback path: %v", err)
	}
	t.Cleanup(restore)

	if slog.Default() == previousDefault {
		t.Fatal("slog.Default was not swapped on fallback path; TUI would still leak to stderr")
	}

	// Should not panic when emitting to the discard handler.
	slog.Info("info-to-discard", "key", "value")
}
