//go:build integration
// +build integration

package integration

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/moby/moby/client"
	"github.com/testcontainers/testcontainers-go"
)

// Session transport: one long-lived `sh` per pooled container instead of one
// `docker exec` per command.
//
// The 2026-08-10 telemetry measured 9,976 exec round trips per run at p50
// 119ms — for trivial commands most of that median is the daemon's exec
// create/start/attach cycle, not the command. A persistent shell pays that
// cycle once per container and turns every subsequent command into a write
// and a read on an already-open stream. Each command travels as a framed
// unit: run the command with output captured to a nonce-named file, then cat
// the file back followed by a marker line carrying the same nonce and the
// exit code. The nonce is random per command, so command output cannot forge
// the frame boundary, and the exit code arrives in-band rather than being
// inferred from output.
//
// Every fault in this file is infrastructure by construction: the command
// under test cannot produce a session-transport error, because its exit code
// and output are data inside a healthy frame. A fault poisons the session —
// the connection is closed, the shell killed best-effort — and the next
// command opens a fresh one, so a single blip costs one infra-labelled
// failure, not a wedged container.

// execTransportEnv selects how mid-test container commands reach the daemon:
// "session" (one persistent shell per container, the default) or "exec" (one
// docker exec per command, the pre-2026-08 behavior). The exec value is the
// bisect lever for any fault that only reproduces under real docker exec; it
// is scheduled for removal after one stable release cycle (WI-719124 doc 02).
const execTransportEnv = "CAMP_TEST_EXEC_TRANSPORT"

const (
	transportSession = "session"
	transportExec    = "exec"
)

// validateExecTransport rejects unknown transport values loudly at TestMain,
// so a typo cannot silently select a default the operator did not intend.
func validateExecTransport() error {
	switch v := os.Getenv(execTransportEnv); v {
	case "", transportSession, transportExec:
		return nil
	default:
		return fmt.Errorf("%s=%q: unknown transport (want %q or %q)",
			execTransportEnv, v, transportSession, transportExec)
	}
}

// execTransportName resolves the transport once for the whole run. TestMain
// validates the value before any test can reach this.
var execTransportName = sync.OnceValue(func() string {
	if os.Getenv(execTransportEnv) == transportExec {
		return transportExec
	}
	return transportSession
})

// sessionDockerClient is the raw moby client sessions attach through. The
// testcontainers provider resolves the same DOCKER_HOST the pooled containers
// run on; it is intentionally never closed, since sessions live for the whole
// run and the process exits right after teardown.
var sessionDockerClient = sync.OnceValues(func() (client.APIClient, error) {
	provider, err := testcontainers.NewDockerProvider()
	if err != nil {
		return nil, fmt.Errorf("session transport: docker provider: %w", err)
	}
	return provider.Client(), nil
})

// sessionBox holds the live session for one pooled container. It lives on the
// pool member and is shared by pointer with every per-test wrapper, so the
// session survives across checkouts and a poisoned one is replaced exactly
// once no matter which wrapper saw the fault.
type sessionBox struct {
	mu  sync.Mutex
	cur *execSession
}

// closeIdle releases the current session for container teardown. No kill: the
// container is being terminated anyway.
func (b *sessionBox) closeIdle() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cur != nil {
		b.cur.close()
		b.cur = nil
	}
}

// execSession is one long-lived `sh` inside a container, driven over a
// hijacked exec attach. The stream from the daemon is stdout/stderr
// multiplexed (no TTY — a TTY would rewrite \n to \r\n and destroy output
// fidelity), so frames are demultiplexed inline as they are read.
type execSession struct {
	conn        net.Conn
	reader      *bufio.Reader
	shellPID    int
	containerID string
	// timeout overrides execTimeout per frame; tests use it to exercise the
	// deadline path without waiting two minutes. Zero means execTimeout.
	timeout time.Duration
	// diag retains shell-level stderr (never command output — commands merge
	// their stderr into the capture file) for fault messages.
	diag bytes.Buffer
}

// sessionMaxDockerFrame bounds a single multiplexed chunk from the daemon.
// Real chunks are kilobytes; a larger claim means the stream is corrupt, and
// honoring it would attempt a matching allocation.
const sessionMaxDockerFrame = 16 << 20

// sessionDiagCap bounds retained shell-level stderr.
const sessionDiagCap = 8 << 10

// openExecSession starts the persistent shell in the given container and
// verifies it end to end with a handshake frame.
func openExecSession(ctx context.Context, containerID string) (*execSession, error) {
	cli, err := sessionDockerClient()
	if err != nil {
		return nil, err
	}

	createCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	created, err := cli.ExecCreate(createCtx, containerID, client.ExecCreateOptions{
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          []string{"sh"},
	})
	if err != nil {
		return nil, fmt.Errorf("exec create: %w", err)
	}
	attach, err := cli.ExecAttach(createCtx, created.ID, client.ExecAttachOptions{})
	if err != nil {
		return nil, fmt.Errorf("exec attach: %w", err)
	}

	s := &execSession{
		conn:        attach.Conn,
		reader:      attach.Reader,
		containerID: containerID,
	}

	// Handshake: prove the shell answers frames and record its PID so poison
	// can kill a wedged one later. Raw script on purpose — `$$` must reach
	// the shell unquoted.
	code, out, err := s.runScript("echo $$")
	if err != nil {
		s.close()
		return nil, fmt.Errorf("session handshake: %w", err)
	}
	pid, convErr := strconv.Atoi(strings.TrimSpace(string(out)))
	if code != 0 || convErr != nil {
		s.close()
		return nil, fmt.Errorf("session handshake returned code=%d out=%q", code, out)
	}
	s.shellPID = pid
	return s, nil
}

// runScript executes one framed command and returns its exit code and exact
// merged output. Any returned error means the session can no longer be
// trusted and the caller must poison it.
func (s *execSession) runScript(script string) (int, []byte, error) {
	nonce, err := frameNonce()
	if err != nil {
		return -1, nil, err
	}

	deadline := time.Now().Add(s.frameTimeout())
	_ = s.conn.SetWriteDeadline(deadline)
	if _, err := io.WriteString(s.conn, sessionFrame(script, nonce)); err != nil {
		return -1, nil, classifySessionFault(err, "session write", s.frameTimeout())
	}
	return s.readFrame(nonce, deadline)
}

// sessionFrame renders one framed command for the session shell.
//
//   - The subshell keeps cd, variables, and shell options from leaking into
//     the session shell, so every frame starts from the same state.
//   - stdin comes from /dev/null: a command that read the session's stdin
//     would consume the next frame (docker exec runs commands with no usable
//     stdin, so this also preserves the old transport's semantics).
//   - Output is captured to a nonce-named file and cat'd back, keeping
//     command bytes out of the control channel; the file is removed before
//     the marker prints so a detached child that inherited the descriptor
//     (camp's deferred-commit workers) writes into an unlinked inode instead
//     of into a later frame's capture.
//   - The exit code is read into a variable before cat and rm can overwrite
//     $?, and travels on the marker line with the nonce.
func sessionFrame(script, nonce string) string {
	capture := "/tmp/.camp_frame_" + nonce
	return "(\n" + script + "\n) </dev/null >" + capture + " 2>&1\n" +
		"__hc=$?\n" +
		"cat " + capture + "\n" +
		"rm -f " + capture + "\n" +
		"printf '\\n%s %s\\n' '" + nonce + "' \"$__hc\"\n"
}

// readFrame consumes the multiplexed stream until this frame's marker line
// arrives, returning the exit code and the exact output bytes.
func (s *execSession) readFrame(nonce string, deadline time.Time) (int, []byte, error) {
	_ = s.conn.SetReadDeadline(deadline)
	defer func() { _ = s.conn.SetReadDeadline(time.Time{}) }()

	marker := []byte("\n" + nonce + " ")
	var (
		out     bytes.Buffer
		hdr     [8]byte
		scanned int // prefix of out already known not to contain the marker
	)
	for {
		if _, err := io.ReadFull(s.reader, hdr[:]); err != nil {
			return -1, nil, classifySessionFault(err, "session read", s.frameTimeout())
		}
		size := int(binary.BigEndian.Uint32(hdr[4:8]))
		if size > sessionMaxDockerFrame {
			return -1, nil, newInfraError(nil,
				"session stream corrupt: daemon claimed a %d byte chunk", size)
		}
		payload := make([]byte, size)
		if _, err := io.ReadFull(s.reader, payload); err != nil {
			return -1, nil, classifySessionFault(err, "session read", s.frameTimeout())
		}
		if hdr[0] == 2 {
			s.observeDiag(payload)
			continue
		}
		out.Write(payload)

		// Scan only the unscanned tail (plus marker-length overlap) so
		// multi-megabyte outputs stay linear instead of rescanning the whole
		// buffer per chunk.
		from := scanned - len(marker)
		if from < 0 {
			from = 0
		}
		idx := bytes.Index(out.Bytes()[from:], marker)
		if idx < 0 {
			scanned = out.Len()
			continue
		}
		idx += from

		tail := out.Bytes()[idx+len(marker):]
		nl := bytes.IndexByte(tail, '\n')
		if nl < 0 {
			// Exit code still in flight; keep reading without advancing
			// scanned so the marker is refound with a complete line.
			continue
		}
		code, convErr := strconv.Atoi(string(tail[:nl]))
		if convErr != nil || nl != len(tail)-1 {
			return -1, nil, newInfraError(convErr,
				"session protocol violation after frame marker: %q (shell stderr: %q)",
				tail, s.diagTail())
		}
		return code, append([]byte(nil), out.Bytes()[:idx]...), nil
	}
}

func (s *execSession) frameTimeout() time.Duration {
	if s.timeout > 0 {
		return s.timeout
	}
	return execTimeout
}

func (s *execSession) observeDiag(p []byte) {
	s.diag.Write(p)
	if s.diag.Len() > sessionDiagCap {
		tail := append([]byte(nil), s.diag.Bytes()[s.diag.Len()-sessionDiagCap:]...)
		s.diag.Reset()
		s.diag.Write(tail)
	}
}

func (s *execSession) diagTail() string { return s.diag.String() }

// close releases the connection without touching container state.
func (s *execSession) close() { _ = s.conn.Close() }

// poison closes the session and best-effort kills its shell tree, so a
// wedged command does not keep running behind the fresh session that
// replaces this one. Failures are ignored: if the daemon cannot service the
// kill, the daemon is already the fault being reported.
func (s *execSession) poison(container testcontainers.Container) {
	s.close()
	if s.shellPID <= 0 || container == nil {
		return
	}
	killCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, _ = container.Exec(killCtx, []string{"sh", "-c", fmt.Sprintf(
		"pkill -9 -P %d 2>/dev/null; kill -9 %d 2>/dev/null", s.shellPID, s.shellPID)})
}

// execViaSession runs one command through the container's persistent shell,
// opening the session on first use and replacing it after any fault.
//
// The per-frame deadline, not ctx, bounds the command — the pool wires
// context.Background() into every member, so ctx here exists for the open
// path's dial timeouts. This matches what the exec transport enforces in
// practice.
func (tc *TestContainer) execViaSession(ctx context.Context, cmd []string) (int, []byte, error) {
	box := tc.sessions
	box.mu.Lock()
	defer box.mu.Unlock()

	if box.cur == nil {
		s, err := openExecSession(ctx, tc.container.GetContainerID())
		if err != nil {
			var ie *infraError
			if !errors.As(err, &ie) {
				err = newInfraError(err,
					"could not open exec session: %v"+
						"\n\nThe Docker daemon rejected or dropped the session attach. "+
						"Common cause: several suites or gates running at once. Re-run "+
						"on an idle machine, or lower CAMP_TEST_POOL_SIZE", err)
			}
			return -1, nil, err
		}
		box.cur = s
	}

	code, out, err := box.cur.runScript(shellJoin(cmd))
	if err != nil {
		box.cur.poison(tc.container)
		box.cur = nil
		return -1, nil, err
	}
	return code, out, nil
}

// classifySessionFault labels a transport-level session error. The deadline
// case mirrors classifyExecOutcome's wedged-daemon message; everything else
// is a dropped stream. Both are infrastructure: the frame protocol leaves the
// command under test no path to a conn error.
func classifySessionFault(err error, op string, timeout time.Duration) error {
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return newInfraError(err,
			"%s did not complete within %s: %v"+
				"\n\nThe session shell stopped responding (wedged command or wedged "+
				"daemon). The session is killed and reopened for the next command. "+
				"Common cause: several suites or gates running at once. Re-run on an "+
				"idle machine, or lower CAMP_TEST_POOL_SIZE",
			op, timeout, err)
	}
	return newInfraError(err,
		"%s failed: %v"+
			"\n\nThe exec session stream dropped. Common cause: several suites or "+
			"gates running at once. Re-run on an idle machine, or lower "+
			"CAMP_TEST_POOL_SIZE", op, err)
}

// shellJoin renders an argv as one shell command line, single-quoting every
// argument with the same idiom the Run* wrappers use for theirs.
func shellJoin(argv []string) string {
	quoted := make([]string, len(argv))
	for i, arg := range argv {
		quoted[i] = "'" + strings.ReplaceAll(arg, "'", `'"'"'`) + "'"
	}
	return strings.Join(quoted, " ")
}

func frameNonce() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", newInfraError(err, "session nonce: %v", err)
	}
	return hex.EncodeToString(b[:]), nil
}
