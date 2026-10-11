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
}

// timeUp is a context whose time runs out when the test says so.
type timeUp struct {
	context.Context
	up chan struct{}
}

func (c timeUp) Done() <-chan struct{} { return c.up }

func (c timeUp) Err() error {
	select {
	case <-c.up:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

// A preparation that ran out of time, unlike one that was cancelled, is still
// being waited for, and says why it failed.
func TestHopPreparationThatRanOutOfTimeSaysSo(t *testing.T) {
	ctx := timeUp{context.Background(), make(chan struct{})}
	// The script says on fd 3 when it is ready to be interrupted, so its time
	// runs out only once it is.
	ready, readying, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ready.Close() }()
	cmd := exec.CommandContext(ctx, "sh", "-c", `sleep 60 >/dev/null 2>&1 3>&- & child=$!; trap 'kill "$child"; echo "timed out" >&2; exit 1' INT; echo ready >&3; wait`)
	cmd.ExtraFiles = []*os.File{readying}
	var said bytes.Buffer
	done := make(chan error, 1)
	go func() {
		_, err := runHopPreparation(ctx, cmd, &said)
		done <- err
	}()
	if line, err := bufio.NewReader(ready).ReadString('\n'); err != nil || line != "ready\n" {
		t.Fatalf("the script never got ready: %q %v", line, err)
	}
	close(ctx.up)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a preparation that ran out of time must not be reported as done")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a preparation that ran out of time never returned")
	}
	_ = readying.Close()
	if said.String() != "timed out\n" {
		t.Fatalf("a preparation that ran out of time must say why: %q", said.String())
	}
}
