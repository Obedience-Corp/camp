//go:build unix

package main

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A preparation that is cancelled has usually started ssh. It must be given
// the chance to end that, which a kill would not give it, and what it then
// says about having been cancelled is for nobody.
func TestHopPreparationCancelledEndsWhatItStarted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Stands in for `camp switch`: it starts a child and, when interrupted,
	// ends it and says so. Both hold the write end of held, so reading it to
	// its end is how the test knows that neither is left running.
	held, holding, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Close() }()
	cmd := exec.CommandContext(ctx, "sh", "-c", `sleep 60 >&3 2>&3 & child=$!; trap 'kill "$child"; echo interrupted >&2; exit 130' INT; echo "$child" >&3; wait`)
	cmd.ExtraFiles = []*os.File{holding}
	var said bytes.Buffer
	done := make(chan error, 1)
	go func() {
		_, err := runHopPreparation(ctx, cmd, &said)
		done <- err
	}()
	reader := bufio.NewReader(held)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	child, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatalf("child pid %q: %v", line, err)
	}
	t.Cleanup(func() { _ = syscall.Kill(child, syscall.SIGKILL) })

	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a cancelled preparation must not be reported as done")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a cancelled preparation never returned")
	}
	if said.Len() != 0 {
		t.Fatalf("what a cancelled preparation says must be dropped: %q", said.String())
	}
	_ = holding.Close()
	ended := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, reader)
		close(ended)
	}()
	select {
	case <-ended:
	case <-time.After(3 * time.Second):
		t.Fatal("a cancelled preparation left what it started running")
	}
}

func TestHopPreparationSaysWhyItFailed(t *testing.T) {
	ctx := context.Background()
	var said bytes.Buffer
	out, err := runHopPreparation(ctx, exec.CommandContext(ctx, "sh", "-c", `echo "no such camp" >&2; exit 3`), &said)
	if err == nil || out != "" || said.String() != "no such camp\n" {
		t.Fatalf("a failed preparation: out %q err %v said %q", out, err, said.String())
	}
	said.Reset()
	out, err = runHopPreparation(ctx, exec.CommandContext(ctx, "sh", "-c", `echo "cd -- /next"`), &said)
	if err != nil || out != "cd -- /next\n" || said.Len() != 0 {
		t.Fatalf("a preparation: out %q err %v said %q", out, err, said.String())
	}
	// One that ran out of time is still being waited for, and says why.
	ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	said.Reset()
	_, err = runHopPreparation(ctx, exec.CommandContext(ctx, "sh", "-c", `sleep 5 >/dev/null 2>&1 & child=$!; trap 'kill "$child"; echo "timed out" >&2; exit 1' INT; wait`), &said)
	if err == nil || said.String() != "timed out\n" {
		t.Fatalf("a preparation that ran out of time: err %v said %q", err, said.String())
	}
}
