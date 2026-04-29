//go:build integration
// +build integration

package integration

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/creack/pty"
)

// RunCampInteractiveInDir runs camp inside the shared test container through a
// real TTY so integration tests can exercise fuzzyfinder-driven picker flows.
func (tc *TestContainer) RunCampInteractiveInDir(dir, waitFor, input string, args ...string) (string, error) {
	return tc.RunCampInteractiveStepsInDir(dir, []InteractiveStep{{WaitFor: waitFor, Input: input}}, args...)
}

// InteractiveStep describes one wait/send step in an interactive terminal flow.
type InteractiveStep struct {
	WaitFor string
	Input   string
}

// RunCampInteractiveStepsInDir runs camp inside the shared test container
// through a real TTY and sends input only after each requested screen appears.
func (tc *TestContainer) RunCampInteractiveStepsInDir(dir string, steps []InteractiveStep, args ...string) (string, error) {
	if tc.t != nil {
		tc.t.Helper()
	}

	quotedArgs := make([]string, len(args))
	for i, arg := range args {
		quotedArgs[i] = shellQuote(arg)
	}

	cmdStr := fmt.Sprintf("cd %s && TERM=xterm /camp %s 2>&1", shellQuote(dir), strings.Join(quotedArgs, " "))
	ctx, cancel := context.WithTimeout(tc.ctx, 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "exec", "-i", "-t", tc.container.GetContainerID(), "sh", "-lc", cmdStr)
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 40, Cols: 120})
	if err != nil {
		return "", camperrors.Wrap(err, "failed to start interactive docker exec")
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
				return output.String(), camperrors.Wrapf(err, "interactive camp session did not reach %q\nterminal tail:\n%s", step.WaitFor, output.Tail(4000))
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
		return output.String(), camperrors.Wrapf(waitErr, "interactive camp command failed\nterminal tail:\n%s", output.Tail(4000))
	}

	return output.String(), nil
}

func writeInteractiveInput(ptmx *os.File, input string) error {
	for i := 0; i < len(input); {
		chunk := []byte{input[i]}
		if input[i] == '\x1b' && i+2 < len(input) && input[i+1] == '[' {
			chunk = []byte(input[i : i+3])
			i += 3
		} else {
			i++
		}

		if _, err := ptmx.Write(chunk); err != nil {
			return err
		}
		time.Sleep(25 * time.Millisecond)
	}
	return nil
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

func (b *lockedBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Len()
}

func (b *lockedBuffer) StringFrom(offset int) string {
	b.mu.Lock()
	defer b.mu.Unlock()

	s := b.buf.String()
	if offset > len(s) {
		offset = len(s)
	}
	return s[offset:]
}

func (b *lockedBuffer) Tail(max int) string {
	s := b.String()
	if len(s) <= max {
		return s
	}
	return s[len(s)-max:]
}

func waitForBufferContains(output *lockedBuffer, want string, timeout time.Duration) error {
	return waitForBufferContainsAfter(output, want, 0, timeout)
}

func waitForBufferContainsAfter(output *lockedBuffer, want string, offset int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(output.StringFrom(offset), want) {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}

	return camperrors.Wrapf(camperrors.ErrTimeout, "timed out waiting for %q", want)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

const (
	terminalRequestBackgroundColor = "\x1b]11;?\x1b\\"
	terminalBackgroundColorReply   = "\x1b]11;rgb:0000/0000/0000\x1b\\"
	terminalRequestCursorPosition  = "\x1b[6n"
	terminalCursorPositionReply    = "\x1b[1;1R"
	terminalRequestDeviceAttrs     = "\x1b[c"
	terminalDeviceAttrsReply       = "\x1b[?1;0c"
)

func copyTerminalOutput(ptmx *os.File, output *lockedBuffer) (int64, error) {
	var total int64
	var transcript strings.Builder
	var respondedBG, respondedCursor, respondedDA int
	buf := make([]byte, 1024)

	for {
		n, err := ptmx.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			written, writeErr := output.Write(chunk)
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}

			transcript.Write(chunk)
			raw := transcript.String()

			for respondedBG < strings.Count(raw, terminalRequestBackgroundColor) {
				if _, writeErr := ptmx.Write([]byte(terminalBackgroundColorReply)); writeErr != nil {
					return total, writeErr
				}
				respondedBG++
			}
			for respondedCursor < strings.Count(raw, terminalRequestCursorPosition) {
				if _, writeErr := ptmx.Write([]byte(terminalCursorPositionReply)); writeErr != nil {
					return total, writeErr
				}
				respondedCursor++
			}
			for respondedDA < strings.Count(raw, terminalRequestDeviceAttrs) {
				if _, writeErr := ptmx.Write([]byte(terminalDeviceAttrsReply)); writeErr != nil {
					return total, writeErr
				}
				respondedDA++
			}
		}

		if err != nil {
			return total, err
		}
	}
}
