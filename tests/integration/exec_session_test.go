//go:build integration
// +build integration

package integration

// Session-transport protocol tests. These drive the real frame writer and
// stream parser over a net.Pipe standing in for the daemon's hijacked
// connection, so every property the protocol claims — output fidelity, exit
// code fidelity, marker safety, deadline poisoning — is provable in
// milliseconds without a container. The dockerized end-to-end proof is the
// suite itself running on the session transport.

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

// muxFrame encodes one docker stream chunk (8-byte header + payload) the way
// the daemon multiplexes stdout/stderr on a non-TTY exec.
func muxFrame(stream byte, payload []byte) []byte {
	hdr := make([]byte, 8)
	hdr[0] = stream
	binary.BigEndian.PutUint32(hdr[4:8], uint32(len(payload)))
	return append(hdr, payload...)
}

// newPipeSession returns a session whose daemon side is the returned conn.
func newPipeSession(t *testing.T) (*execSession, net.Conn) {
	t.Helper()
	clientSide, daemonSide := net.Pipe()
	s := &execSession{
		conn:    clientSide,
		reader:  bufio.NewReader(clientSide),
		timeout: 5 * time.Second,
	}
	t.Cleanup(func() {
		_ = clientSide.Close()
		_ = daemonSide.Close()
	})
	return s, daemonSide
}

var frameNoncePattern = regexp.MustCompile(`\.camp_frame_([0-9a-f]{32})`)

// readFrameNonce consumes the frame the session wrote and extracts its nonce,
// so the fake daemon can answer with a matching marker.
func readFrameNonce(t *testing.T, daemon net.Conn) string {
	t.Helper()
	buf := make([]byte, 1<<20)
	n, err := daemon.Read(buf)
	if err != nil {
		t.Errorf("fake daemon: reading frame: %v", err)
		return ""
	}
	m := frameNoncePattern.FindSubmatch(buf[:n])
	if m == nil {
		t.Errorf("fake daemon: no nonce in frame %q", buf[:n])
		return ""
	}
	return string(m[1])
}

func TestSessionFrameRoundTrip(t *testing.T) {
	t.Parallel()

	bigBlob := bytes.Repeat([]byte("x\x00y\x1b[31m"), 512*1024) // ~3.5MB with NULs and ANSI
	outputs := map[string][]byte{
		"empty":              {},
		"no-trailing-nl":     []byte("abc"),
		"trailing-nl":        []byte("abc\n"),
		"binary-and-ansi":    {0, 1, 2, '\n', 0x1b, '[', '3', '1', 'm', 0xff, '\n'},
		"marker-lookalike":   []byte("\ndeadbeefdeadbeefdeadbeefdeadbeef 0\n"),
		"multi-megabyte":     bigBlob,
		"only-newlines":      []byte("\n\n\n"),
		"json-with-newlines": []byte("{\n  \"key\": \"value with spaces\"\n}\n"),
	}
	chunkings := map[string]func(nonce string, out []byte) [][]byte{
		"single-chunk": func(nonce string, out []byte) [][]byte {
			return [][]byte{muxFrame(1, append(append([]byte{}, out...), []byte("\n"+nonce+" 7\n")...))}
		},
		"output-then-marker": func(nonce string, out []byte) [][]byte {
			return [][]byte{muxFrame(1, out), muxFrame(1, []byte("\n" + nonce + " 7\n"))}
		},
		"tiny-chunks": func(nonce string, out []byte) [][]byte {
			whole := append(append([]byte{}, out...), []byte("\n"+nonce+" 7\n")...)
			var chunks [][]byte
			for len(whole) > 0 {
				n := 7
				if n > len(whole) {
					n = len(whole)
				}
				chunks = append(chunks, muxFrame(1, whole[:n]))
				whole = whole[n:]
			}
			return chunks
		},
		"marker-straddles-chunks": func(nonce string, out []byte) [][]byte {
			marker := []byte("\n" + nonce + " 7\n")
			return [][]byte{
				muxFrame(1, append(append([]byte{}, out...), marker[:5]...)),
				muxFrame(1, marker[5:]),
			}
		},
	}

	for outName, want := range outputs {
		for chunkName, chunker := range chunkings {
			if outName == "multi-megabyte" && chunkName == "tiny-chunks" {
				continue // ~500k frames adds seconds and proves nothing new
			}
			t.Run(outName+"/"+chunkName, func(t *testing.T) {
				t.Parallel()
				s, daemon := newPipeSession(t)

				go func() {
					nonce := readFrameNonce(t, daemon)
					if nonce == "" {
						return
					}
					for _, chunk := range chunker(nonce, want) {
						if _, err := daemon.Write(chunk); err != nil {
							return
						}
					}
				}()

				code, got, err := s.runScript("true")
				if err != nil {
					t.Fatalf("runScript: %v", err)
				}
				if code != 7 {
					t.Fatalf("exit code = %d, want 7", code)
				}
				if !bytes.Equal(got, want) {
					t.Fatalf("output fidelity lost:\ngot  %q\nwant %q", got, want)
				}
			})
		}
	}
}

func TestSessionExitCodeFidelity(t *testing.T) {
	t.Parallel()

	for _, code := range []int{0, 1, 42, 255} {
		s, daemon := newPipeSession(t)
		go func() {
			nonce := readFrameNonce(t, daemon)
			if nonce == "" {
				return
			}
			_, _ = daemon.Write(muxFrame(1, fmt.Appendf(nil, "\n%s %d\n", nonce, code)))
		}()
		got, out, err := s.runScript("true")
		if err != nil {
			t.Fatalf("runScript: %v", err)
		}
		if got != code || len(out) != 0 {
			t.Fatalf("got code=%d out=%q, want code=%d out empty", got, out, code)
		}
	}
}

func TestSessionStderrIsDiagnosticsNotOutput(t *testing.T) {
	t.Parallel()

	s, daemon := newPipeSession(t)
	go func() {
		nonce := readFrameNonce(t, daemon)
		if nonce == "" {
			return
		}
		_, _ = daemon.Write(muxFrame(2, []byte("sh: some shell-level noise\n")))
		_, _ = daemon.Write(muxFrame(1, []byte("real output\n"+nonce+" 0\n")))
	}()

	code, out, err := s.runScript("true")
	if err != nil {
		t.Fatalf("runScript: %v", err)
	}
	if code != 0 || string(out) != "real output" {
		t.Fatalf("got code=%d out=%q, want 0/%q", code, out, "real output")
	}
	if !strings.Contains(s.diagTail(), "shell-level noise") {
		t.Fatalf("shell stderr not retained for diagnostics: %q", s.diagTail())
	}
}

func TestSessionProtocolViolationTrailingBytes(t *testing.T) {
	t.Parallel()

	s, daemon := newPipeSession(t)
	go func() {
		nonce := readFrameNonce(t, daemon)
		if nonce == "" {
			return
		}
		_, _ = daemon.Write(muxFrame(1, []byte("out\n"+nonce+" 0\nGARBAGE")))
	}()

	_, _, err := s.runScript("true")
	var ie *infraError
	if !errors.As(err, &ie) {
		t.Fatalf("trailing bytes after the marker must poison as infra, got %v", err)
	}
	if !strings.Contains(err.Error(), "protocol violation") {
		t.Fatalf("error should name the protocol violation, got %q", err)
	}
}

func TestSessionCorruptChunkHeader(t *testing.T) {
	t.Parallel()

	s, daemon := newPipeSession(t)
	go func() {
		_ = readFrameNonce(t, daemon)
		hdr := make([]byte, 8)
		hdr[0] = 1
		binary.BigEndian.PutUint32(hdr[4:8], uint32(sessionMaxDockerFrame+1))
		_, _ = daemon.Write(hdr)
	}()

	_, _, err := s.runScript("true")
	var ie *infraError
	if !errors.As(err, &ie) {
		t.Fatalf("an absurd chunk size must classify as infra, got %v", err)
	}
	if !strings.Contains(err.Error(), "corrupt") {
		t.Fatalf("error should name the corrupt stream, got %q", err)
	}
}

func TestSessionDeadlineClassifiesAsWedged(t *testing.T) {
	t.Parallel()

	s, daemon := newPipeSession(t)
	s.timeout = 50 * time.Millisecond
	go func() {
		_ = readFrameNonce(t, daemon) // consume the frame, then never answer
	}()

	start := time.Now()
	_, _, err := s.runScript("sleep 9999")
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("deadline did not bound the read: took %s", elapsed)
	}
	var ie *infraError
	if !errors.As(err, &ie) {
		t.Fatalf("a frame deadline must classify as infra, got %v", err)
	}
	if !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("cause must stay inspectable as a deadline, got %v", err)
	}
	if !strings.Contains(err.Error(), "did not complete within") {
		t.Fatalf("error should read as the wedged case, got %q", err)
	}
}

func TestClassifySessionFault(t *testing.T) {
	t.Parallel()

	wedged := classifySessionFault(os.ErrDeadlineExceeded, "session read", time.Minute)
	var ie *infraError
	if !errors.As(wedged, &ie) || !strings.Contains(wedged.Error(), "stopped responding") {
		t.Fatalf("deadline fault misclassified: %v", wedged)
	}

	dropped := classifySessionFault(errors.New("unexpected EOF"), "session read", time.Minute)
	if !errors.As(dropped, &ie) || !strings.Contains(dropped.Error(), "stream dropped") {
		t.Fatalf("stream fault misclassified: %v", dropped)
	}
	for _, fault := range []error{wedged, dropped} {
		if !strings.Contains(fault.Error(), "INFRASTRUCTURE FAILURE") {
			t.Fatalf("session faults must carry the infra banner: %q", fault)
		}
	}
}

func TestSessionFrameShape(t *testing.T) {
	t.Parallel()

	nonce := strings.Repeat("ab", 16)
	frame := sessionFrame("'git' 'status'", nonce)

	for _, want := range []string{
		"(\n'git' 'status'\n)", // subshell: no cd/env bleed into the session shell
		"</dev/null",           // a stdin-reading command must not eat the next frame
		"/tmp/.camp_frame_" + nonce,
		"rm -f /tmp/.camp_frame_" + nonce, // unlink before marker: detached children write to a dead inode
		"'" + nonce + "' \"$__hc\"",
	} {
		if !strings.Contains(frame, want) {
			t.Fatalf("frame missing %q:\n%s", want, frame)
		}
	}
	if strings.Index(frame, "rm -f") < strings.Index(frame, "cat ") {
		t.Fatal("capture must be cat'd before it is removed")
	}
}

func TestShellJoin(t *testing.T) {
	t.Parallel()

	cases := []struct {
		argv []string
		want string
	}{
		{[]string{"git", "init", "/test/repo"}, "'git' 'init' '/test/repo'"},
		{[]string{"sh", "-c", "echo hi"}, "'sh' '-c' 'echo hi'"},
		{[]string{"echo", "it's"}, `'echo' 'it'"'"'s'`},
		{[]string{"printf", "%s", "$HOME `id` \"quoted\""}, `'printf' '%s' '$HOME ` + "`id`" + ` "quoted"'`},
		{[]string{"echo", "multi\nline"}, "'echo' 'multi\nline'"},
	}
	for _, tc := range cases {
		if got := shellJoin(tc.argv); got != tc.want {
			t.Fatalf("shellJoin(%q) = %q, want %q", tc.argv, got, tc.want)
		}
	}
}

func TestValidateExecTransport(t *testing.T) {
	// Not parallel: mutates the process environment.
	t.Setenv(execTransportEnv, "sessions")
	if err := validateExecTransport(); err == nil {
		t.Fatal("a typo'd transport must be rejected, not silently defaulted")
	}
	for _, ok := range []string{"", "session", "exec"} {
		t.Setenv(execTransportEnv, ok)
		if err := validateExecTransport(); err != nil {
			t.Fatalf("validateExecTransport(%q) = %v", ok, err)
		}
	}
}
