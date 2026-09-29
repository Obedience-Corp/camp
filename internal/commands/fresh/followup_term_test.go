package fresh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/creack/pty"
	"golang.org/x/term"
)

func TestMain(m *testing.M) {
	if os.Getenv("CAMP_FRESH_FOLLOWUP_PROBE") == "1" {
		writeFollowUpProbe()
		return
	}
	os.Exit(m.Run())
}

// writeFollowUpProbe prints whether this process has a terminal, and the width
// that terminal reports. camp fresh uses those two facts to decide color and
// layout for a follow-up command.
func writeFollowUpProbe() {
	info, statErr := os.Stdout.Stat()
	// Match buildutil's own terminal check (char device), plus the width ioctl
	// the summary card uses. Either one failing is the uncolored pipe path.
	charDevice := statErr == nil && info.Mode()&os.ModeCharDevice != 0
	if !charDevice || !term.IsTerminal(int(os.Stdout.Fd())) {
		_, _ = fmt.Fprint(os.Stdout, "plain\n")
		return
	}
	cols, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || cols <= 0 {
		_, _ = fmt.Fprint(os.Stdout, "tty:err\n")
		return
	}
	_, _ = fmt.Fprintf(os.Stdout, "\033[32mtty:%d\033[0m\n", cols)
}

func followUpProbeCommand() string {
	return "CAMP_FRESH_FOLLOWUP_PROBE=1 exec " + shellSingleQuote(os.Args[0])
}

func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

func openSizedTerminal(t *testing.T, cols, rows uint16) *os.File {
	t.Helper()
	master, slave, err := pty.Open()
	if errors.Is(err, pty.ErrUnsupported) {
		t.Skip("pty is not available")
	}
	if err != nil {
		t.Fatalf("open pty: %v", err)
	}
	t.Cleanup(func() {
		_ = master.Close()
		_ = slave.Close()
	})
	if err := pty.Setsize(slave, &pty.Winsize{Cols: cols, Rows: rows}); err != nil {
		t.Fatalf("set size: %v", err)
	}
	return slave
}

func TestFollowUpOnTerminalKeepsColorAndWidth(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	parent := openSizedTerminal(t, 100, 32)

	var buf bytes.Buffer
	step := newFreshLive(&buf, "install", "running", "$ probe")
	step.animated = true
	step.width = 80
	step.Begin()

	err := runFollowUpCommandOn(t.Context(), ".", followUpProbeCommand(), step.Stream(), parent)
	if err != nil {
		t.Fatalf("follow-up: %v", err)
	}
	step.Finish("done", ui.StatusSuccess)

	raw := buf.String()
	if !strings.Contains(raw, "\033[32mtty:100\033[0m") {
		t.Fatalf("child did not see a 100-column terminal:\n%s", raw)
	}
	if strings.Contains(raw, "\r") {
		t.Fatalf("pty translated newlines before the user's terminal could:\n%q", raw)
	}
	plain := stripFreshANSI(applyFreshTerm(raw))
	for _, want := range []string{"install", "$ probe", "tty:100", "done"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("missing %q\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "running") {
		t.Fatalf("spinner stayed on screen:\n%s", plain)
	}
}

func TestFollowUpOnPipeStaysPlain(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = devNull.Close() })

	var buf bytes.Buffer
	if err := runFollowUpCommandOn(t.Context(), ".", followUpProbeCommand(), &buf, devNull); err != nil {
		t.Fatalf("follow-up: %v", err)
	}
	if strings.Contains(buf.String(), "\033") || !strings.Contains(buf.String(), "plain\n") {
		t.Fatalf("pipe output: %q", buf.String())
	}
}

func TestFollowUpDumbTerminalStaysPlain(t *testing.T) {
	t.Setenv("TERM", "dumb")
	parent := openSizedTerminal(t, 100, 32)

	var buf bytes.Buffer
	if err := runFollowUpCommandOn(t.Context(), ".", followUpProbeCommand(), &buf, parent); err != nil {
		t.Fatalf("follow-up: %v", err)
	}
	if strings.Contains(buf.String(), "\033") || !strings.Contains(buf.String(), "plain\n") {
		t.Fatalf("dumb terminal output: %q", buf.String())
	}
}

func TestFollowUpOnTerminalReportsExitCode(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	parent := openSizedTerminal(t, 80, 24)

	var buf bytes.Buffer
	err := runFollowUpCommandOn(t.Context(), ".", "exit 9", &buf, parent)
	var cmdErr *camperrors.CommandError
	if !errors.As(err, &cmdErr) || cmdErr.ExitCode != 9 {
		t.Fatalf("exit status: %v", err)
	}
}

func TestFollowUpOnTerminalStopsWhenCancelled(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	parent := openSizedTerminal(t, 80, 24)

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	started := time.Now()
	err := runFollowUpCommandOn(ctx, ".", "sleep 30", io.Discard, parent)
	if err == nil {
		t.Fatal("sleep finished instead of stopping")
	}
	if time.Since(started) > 3*time.Second {
		t.Fatalf("cancel took %s", time.Since(started))
	}
}
