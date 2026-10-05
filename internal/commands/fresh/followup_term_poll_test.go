//go:build !windows

package fresh

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

// blockingFollowUpPTY reproduces the Darwin/FreeBSD dependency construction on
// any Unix host: the descriptor is blocking when os.NewFile wraps it.
func blockingFollowUpPTY(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	original, slave, err := pty.Open()
	if err != nil {
		t.Fatalf("open PTY: %v", err)
	}
	t.Cleanup(func() { _ = original.Close(); _ = slave.Close() })
	duplicate, err := unix.FcntlInt(original.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("duplicate PTY: %v", err)
	}
	if err := unix.SetNonblock(duplicate, false); err != nil {
		_ = unix.Close(duplicate)
		t.Fatalf("make PTY blocking: %v", err)
	}
	master := os.NewFile(uintptr(duplicate), "blocking-follow-up-master")
	t.Cleanup(func() { _ = master.Close() })
	if err := master.SetReadDeadline(time.Time{}); !errors.Is(err, os.ErrNoDeadline) {
		t.Fatalf("fixture must start without runtime polling: %v", err)
	}
	return master, slave
}

func TestFollowUpPTYBlockingWrapperReadsDelayedOutput(t *testing.T) {
	original, slave := blockingFollowUpPTY(t)
	master, err := newFollowUpPTYMaster(original)
	if err != nil {
		t.Fatalf("configure pollable PTY: %v", err)
	}
	t.Cleanup(func() { _ = master.Close() })
	if err := original.Close(); err != nil {
		t.Fatalf("close original wrapper: %v", err)
	}
	if err := master.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("replacement must support read deadlines: %v", err)
	}
	writeErr := make(chan error, 1)
	go func() {
		time.Sleep(50 * time.Millisecond)
		_, err := slave.Write([]byte("delayed output"))
		writeErr <- err
	}()
	got := make([]byte, len("delayed output"))
	if _, err := io.ReadFull(master, got); err != nil {
		t.Fatalf("read delayed output: %v", err)
	}
	if string(got) != "delayed output" {
		t.Fatalf("output = %q", got)
	}
	if err := <-writeErr; err != nil {
		t.Fatalf("write delayed output: %v", err)
	}
}

func TestFollowUpPTYBlockingWrapperCloseInterruptsRead(t *testing.T) {
	original, _ := blockingFollowUpPTY(t)
	master, err := newFollowUpPTYMaster(original)
	if err != nil {
		t.Fatalf("configure pollable PTY: %v", err)
	}
	t.Cleanup(func() { _ = master.Close() })
	if err := original.Close(); err != nil {
		t.Fatalf("close original wrapper: %v", err)
	}
	readErr := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		_, err := master.Read(make([]byte, 1))
		readErr <- err
	}()
	<-started
	select {
	case err := <-readErr:
		t.Fatalf("reader finished before Close while the slave was open: %v", err)
	case <-time.After(50 * time.Millisecond):
		// The reader has started and remains pending with no slave output.
	}
	if err := master.Close(); err != nil {
		t.Fatalf("close replacement: %v", err)
	}
	select {
	case err := <-readErr:
		if !errors.Is(err, os.ErrClosed) {
			t.Fatalf("closed reader returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("closing the replacement did not interrupt its reader")
	}
}

func TestFollowUpOnTerminalPreservesDelayedOutput(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	parent := openSizedTerminal(t, 80, 24)
	var output bytes.Buffer
	if err := runFollowUpCommandOn(t.Context(), ".", "sleep 0.05; printf 'delayed output\\n'", &output, parent); err != nil {
		t.Fatalf("delayed follow-up: %v", err)
	}
	if output.String() != "delayed output\n" {
		t.Fatalf("delayed output lost: %q", output.String())
	}
}

func TestFollowUpOnTerminalPreservesHighVolumeOutput(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	parent := openSizedTerminal(t, 80, 24)
	var output bytes.Buffer
	command := "sleep 0.05; awk 'BEGIN { for (i = 0; i < 16384; i++) printf \"0123456789abcdef\" }'; printf 'final marker\\n' >&2"
	if err := runFollowUpCommandOn(t.Context(), ".", command, &output, parent); err != nil {
		t.Fatalf("high-volume follow-up: %v", err)
	}
	want := strings.Repeat("0123456789abcdef", 16384) + "final marker\n"
	if output.String() != want {
		t.Fatalf("high-volume output lost: got %d bytes, want %d", output.Len(), len(want))
	}
}

// delayFirstWrite stalls forwarding independently of PTY capture.
type delayFirstWrite struct {
	buffer  bytes.Buffer
	delayed bool
}

func (w *delayFirstWrite) Write(p []byte) (int, error) {
	if !w.delayed {
		w.delayed = true
		time.Sleep(150 * time.Millisecond)
	}
	return w.buffer.Write(p)
}
func (w *delayFirstWrite) String() string { return w.buffer.String() }
func (w *delayFirstWrite) Len() int       { return w.buffer.Len() }

func TestFollowUpOnTerminalPreservesOutputWithSlowWriter(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	parent := openSizedTerminal(t, 80, 24)
	var output delayFirstWrite
	command := "awk 'BEGIN { for (i = 0; i < 16384; i++) printf \"0123456789abcdef\" }'; printf 'final marker\\n' >&2"
	if err := runFollowUpCommandOn(t.Context(), ".", command, &output, parent); err != nil {
		t.Fatal(err)
	}
	want := strings.Repeat("0123456789abcdef", 16384) + "final marker\n"
	if !output.delayed {
		t.Fatal("slow writer was bypassed")
	}
	if output.String() != want {
		t.Fatalf("slow writer lost output: got %d, want %d", output.Len(), len(want))
	}
}
func TestFollowUpOnTerminalDoesNotFollowChattyDescendant(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	parent := openSizedTerminal(t, 80, 24)
	var output bytes.Buffer
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	started := time.Now()
	err := runFollowUpCommandOn(ctx, ".", "(while :; do printf 'background\\n'; done) & printf 'pid:%s\\n' $!", &output, parent)
	for _, line := range strings.Split(output.String(), "\n") {
		if strings.HasPrefix(line, "pid:") {
			pid, e := strconv.Atoi(strings.TrimPrefix(line, "pid:"))
			if e == nil {
				defer func() { _ = syscall.Kill(pid, syscall.SIGKILL) }()
			}
		}
	}
	if err != nil {
		t.Fatalf("chatty descendant: %v", err)
	}
	if time.Since(started) >= 3*time.Second {
		t.Fatal("follow-up waited for chatty descendant")
	}
	if !strings.Contains(output.String(), "pid:") {
		t.Fatalf("completion marker missing: %q", output.String())
	}
}

func TestFollowUpOnTerminalPreservesSlowOutputWithBackgroundDescendant(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	parent := openSizedTerminal(t, 80, 24)
	var output delayFirstWrite
	command := "sleep 30 & printf 'pid:%s\\n' $!; awk 'BEGIN { for (i = 0; i < 16384; i++) printf \"0123456789abcdef\" }'; printf 'final marker\\n' >&2"
	err := runFollowUpCommandOn(t.Context(), ".", command, &output, parent)
	if !output.delayed {
		t.Fatal("slow writer was bypassed")
	}
	first, body, ok := strings.Cut(output.String(), "\n")
	pid, parseErr := strconv.Atoi(strings.TrimPrefix(first, "pid:"))
	if parseErr == nil {
		defer func() { _ = syscall.Kill(pid, syscall.SIGKILL) }()
	}
	if err != nil {
		t.Fatal(err)
	}
	if !ok || parseErr != nil {
		t.Fatalf("missing PID: %q", first)
	}
	want := strings.Repeat("0123456789abcdef", 16384) + "final marker\n"
	if body != want {
		t.Fatalf("background + slow writer lost output: got %d, want %d", len(body), len(want))
	}
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("background process unexpectedly killed: %v", err)
	}
}

func TestFollowUpOnTerminalPreservesSmallTailAfterSlowWrite(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	parent := openSizedTerminal(t, 80, 24)
	var output delayFirstWrite
	command := "sleep 30 & printf 'pid:%s\\n' $!; printf 'small payload\\n'; sleep 0.01; printf 'final marker\\n' >&2"
	err := runFollowUpCommandOn(t.Context(), ".", command, &output, parent)
	if !output.delayed {
		t.Fatal("slow writer was bypassed")
	}
	first, body, ok := strings.Cut(output.String(), "\n")
	pid, parseErr := strconv.Atoi(strings.TrimPrefix(first, "pid:"))
	if parseErr == nil {
		defer func() { _ = syscall.Kill(pid, syscall.SIGKILL) }()
	}
	if err != nil {
		t.Fatal(err)
	}
	if !ok || parseErr != nil {
		t.Fatalf("missing PID: %q", first)
	}
	if body != "small payload\nfinal marker\n" {
		t.Fatalf("tail lost after slow write: %q", body)
	}
}
