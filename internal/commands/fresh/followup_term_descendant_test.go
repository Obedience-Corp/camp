//go:build !windows

package fresh

import (
	"bytes"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestFollowUpOnTerminalDoesNotWaitForBackgroundDescendant(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	parent := openSizedTerminal(t, 80, 24)
	var output bytes.Buffer
	started := time.Now()
	err := runFollowUpCommandOn(t.Context(), ".", "sleep 30 & printf 'done:%s\\n' $!", &output, parent)
	elapsed := time.Since(started)

	// The command deliberately leaves a background process holding its PTY.
	// It must survive ordinary shell completion, but not this isolated test.
	pid, parseErr := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(output.String()), "done:"))
	if parseErr == nil {
		defer func() { _ = syscall.Kill(pid, syscall.SIGKILL) }()
	}
	if err != nil {
		t.Fatalf("completed follow-up returned an error: %v", err)
	}
	if parseErr != nil {
		t.Fatalf("completion output lost: %q", output.String())
	}
	if elapsed > 3*time.Second {
		t.Fatalf("completed follow-up waited for its background descendant: %s", elapsed)
	}
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("normal completion killed the background descendant: %v", err)
	}
}
