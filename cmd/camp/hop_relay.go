package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/pathutil"
	"github.com/Obedience-Corp/camp/internal/remote"
)

// The relay is the far end of a hop's second resume route. The shell that
// opens a hop starts `camp hop-resume relay <id>` over its own ssh session,
// beside the interactive one. The relay listens on a unix socket in the far
// user's own ~/.obey/hop-resume and carries each exchange over its stdio, so
// the route needs no forwarded socket or port: nothing another user on the far
// machine can connect to, bind, or replace.
//
// One exchange is one line each way, then the far shell's confirmation:
//
//	far shell -> relay -> opening shell   request
//	opening shell -> relay -> far shell   response
//	far shell -> relay -> opening shell   confirm
//
// The opening shell queues the switch only on that confirmation. If the far
// shell hangs up first, the relay says so and the switch is dropped.
const (
	hopRelayVersion     = 2
	hopRelayMaxLine     = 8192
	hopRelayRequestWait = 5 * time.Second
	hopRelayConfirmWait = 5 * time.Second

	// hopResumeSettle bounds how long an ended hop waits for a switch that was
	// in flight when its ssh exited.
	hopResumeSettle = time.Second
)

var errHopAlreadySwitching = camperrors.New("hop-resume: this hop is already switching")

var hopResumeRelayCmd = &cobra.Command{
	Use:    "relay <id>",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runHopRelay(cmd.Context(), args[0], cmd.InOrStdin(), cmd.OutOrStdout())
	},
}

// hopRelayFrame is one line between the relay and the opening shell. Seq names
// the exchange, so an answer to one the far shell already abandoned is dropped
// instead of reaching the next.
type hopRelayFrame struct {
	Version  int             `json:"version"`
	Seq      int             `json:"seq"`
	Request  json.RawMessage `json:"request,omitempty"`
	Response json.RawMessage `json:"response,omitempty"`
	Confirm  bool            `json:"confirm,omitempty"`
	Gone     bool            `json:"gone,omitempty"`
}

type hopRelayConfirm struct {
	Version int  `json:"version"`
	Confirm bool `json:"confirm"`
}

// hopSwitch admits one onward switch per hop, whichever route carries it. A
// request holds it while its route is prepared and acknowledged and gives it
// back when either step fails, so a switch the far shell never completed does
// not block the next attempt.
type hopSwitch struct {
	mu    sync.Mutex
	taken bool
}

func (s *hopSwitch) reserve() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.taken {
		return false
	}
	s.taken = true
	return true
}

func (s *hopSwitch) release() {
	s.mu.Lock()
	s.taken = false
	s.mu.Unlock()
}

func (s *hopSwitch) held() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.taken
}

func newHopRelayID() (string, error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", camperrors.Wrap(err, "hop-resume: relay id")
	}
	return hex.EncodeToString(raw[:]), nil
}

// hopRelaySocketPath is where a hop's relay listens. The id comes from the
// environment and is only ever a file name here.
func hopRelaySocketPath(id string) (string, error) {
	if raw, err := hex.DecodeString(id); err != nil || len(raw) != 8 {
		return "", camperrors.New("hop-resume: malformed relay id")
	}
	home, err := pathutil.Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".obey", "hop-resume", id+".sock"), nil
}

// trustedHopSocket is the one rule for every socket a hopped shell is pointed
// at. The shell can outlive its hop and keeps the names its environment gave
// it, and whoever answers at one of them can end the shell with an
// acknowledgement. So a name is used only while it is a socket this user owns,
// in a directory where nobody else can put something of theirs in its place:
// the user's own, or a sticky one such as /tmp. An ssh server that binds the
// forwarded socket as the login user passes; a socket bound as anyone else,
// root included, does not.
func trustedHopSocket(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	dir, err := os.Stat(filepath.Dir(path))
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 || !ownedByThisUser(info) || !keepsOwnEntries(dir) {
		return camperrors.New(path + " is not a socket only this user could have put there")
	}
	return nil
}

func ownedByThisUser(info os.FileInfo) bool {
	uid, ok := hopFileOwner(info)
	return ok && uid == os.Geteuid()
}

func keepsOwnEntries(dir os.FileInfo) bool {
	if dir.Mode()&os.ModeSticky != 0 {
		return true
	}
	uid, ok := hopFileOwner(dir)
	return ok && (uid == os.Geteuid() || uid == 0) && dir.Mode().Perm()&0o022 == 0
}

var errHopLineTooLong = errors.New("hop-resume: line too long")

// readHopLine returns the next line without its newline. A line longer than
// the reader's buffer is consumed and reported, never returned in part.
func readHopLine(r *bufio.Reader) ([]byte, error) {
	line, err := r.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) {
		for errors.Is(err, bufio.ErrBufferFull) {
			_, err = r.ReadSlice('\n')
		}
		if err != nil {
			return nil, err
		}
		return nil, errHopLineTooLong
	}
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), line[:len(line)-1]...), nil
}

// hopLineWriter writes one JSON value per line, whole.
type hopLineWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *hopLineWriter) write(value any) error {
	line, err := json.Marshal(value)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, err = l.w.Write(append(line, '\n'))
	return err
}

// --- the shell that opened the hop ---

// hopRelayLink is the opening shell's end of a relay's ssh session.
type hopRelayLink struct {
	fromFar io.Reader
	toFar   io.Writer
	stop    func()
}

// hopRelayArgv is the ssh call that runs the relay. -T keeps it a byte stream
// under `RequestTTY force`. ControlMaster=no comes first so it wins: the relay
// rides the hop's ControlMaster when one is up and otherwise connects on its
// own, but never competes with the interactive ssh to create one, which would
// print a warning into the user's terminal.
func hopRelayArgv(hop hopLine, id string) []string {
	argv := append([]string{"-T", "-o", "ControlMaster=no"}, hop.opts...)
	return append(argv, hop.target, remote.CampCommandLine("hop-resume relay "+id))
}

// startHopRelay launches the relay and returns at once. A relay that cannot
// start, including on a far camp that predates relays, is a link that reads as
// closed: the hop is unaffected and the forwarded socket remains.
func startHopRelay(ctx context.Context, hop hopLine, id string) hopRelayLink {
	closed := hopRelayLink{fromFar: strings.NewReader(""), toFar: io.Discard, stop: func() {}}
	cmd := exec.CommandContext(ctx, "ssh", hopRelayArgv(hop, id)...)
	toFar, err := cmd.StdinPipe()
	if err != nil {
		return closed
	}
	fromFar, err := cmd.StdoutPipe()
	if err != nil {
		_ = toFar.Close()
		return closed
	}
	if err := cmd.Start(); err != nil {
		_ = toFar.Close()
		_ = fromFar.Close()
		return closed
	}
	// Killing the local client is enough: the far relay sees its input end
	// and removes its socket. Nothing waits on the far side.
	var once sync.Once
	return hopRelayLink{fromFar: fromFar, toFar: toFar, stop: func() {
		once.Do(func() {
			_ = toFar.Close()
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		})
	}}
}

// serveHopRelay answers the requests a relay carries and returns the follow-up
// line of the switch the far shell confirmed. It ends when the relay does.
func serveHopRelay(ctx context.Context, fromFar io.Reader, toFar io.Writer, hop *hopSwitch, prepare func(context.Context, hopResumeRequest) (string, error)) (string, error) {
	frames := make(chan hopRelayFrame)
	ended := make(chan error, 1)
	go func() {
		reader := bufio.NewReaderSize(fromFar, hopRelayMaxLine)
		for {
			line, err := readHopLine(reader)
			if errors.Is(err, errHopLineTooLong) {
				continue
			}
			if err != nil {
				ended <- err
				return
			}
			// A login shell may print before camp starts; only frames count.
			var frame hopRelayFrame
			if json.Unmarshal(line, &frame) != nil || frame.Version != hopRelayVersion {
				continue
			}
			select {
			case frames <- frame:
			case <-ctx.Done():
				ended <- ctx.Err()
				return
			}
		}
	}()

	type outcome struct {
		follow string
		err    error
	}
	out := hopLineWriter{w: toFar}
	var (
		seq      int // the exchange in flight, 0 when idle
		stop     = func() {}
		prepared chan outcome
		follow   string
		acked    bool
	)
	defer func() { stop() }()
	// abandon ends the exchange in flight without a switch. Its preparation,
	// if still running, is cancelled and its result never read.
	abandon := func() {
		if seq == 0 {
			return
		}
		stop()
		hop.release()
		seq, prepared, follow, acked = 0, nil, "", false
	}
	respond := func(to int, err error) error {
		response := hopResumeResponse{Version: hopRelayVersion, Accepted: err == nil}
		if err != nil {
			response.Error = err.Error()
		}
		raw, marshalErr := json.Marshal(response)
		if marshalErr != nil {
			return marshalErr
		}
		return out.write(hopRelayFrame{Version: hopRelayVersion, Seq: to, Response: raw})
	}
	for {
		select {
		case <-ctx.Done():
			abandon()
			return "", ctx.Err()
		case err := <-ended:
			abandon()
			return "", err
		case result := <-prepared:
			prepared = nil
			if err := respond(seq, result.err); err != nil {
				abandon()
				return "", err
			}
			if result.err != nil {
				abandon()
				continue
			}
			follow, acked = result.follow, true
		case frame := <-frames:
			switch {
			case frame.Request != nil:
				// The relay moves on only once an exchange is over.
				abandon()
				request, err := hopRelayRequest(frame.Request)
				if err == nil && !hop.reserve() {
					err = errHopAlreadySwitching
				}
				if err != nil {
					if err := respond(frame.Seq, err); err != nil {
						return "", err
					}
					continue
				}
				seq = frame.Seq
				prepareCtx, cancelPrepare := context.WithTimeout(ctx, hopResumeTimeout-time.Second)
				stop = cancelPrepare
				done := make(chan outcome, 1)
				prepared = done
				go func() {
					follow, err := prepare(prepareCtx, request)
					done <- outcome{follow, err}
				}()
			case frame.Gone && frame.Seq == seq:
				abandon()
			case frame.Confirm && frame.Seq == seq && acked:
				return follow, nil
			}
		}
	}
}

func hopRelayRequest(raw json.RawMessage) (hopResumeRequest, error) {
	var envelope hopResumeEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return hopResumeRequest{}, camperrors.Wrap(err, "hop-resume: request")
	}
	if envelope.Version != hopRelayVersion {
		return hopResumeRequest{}, camperrors.New("hop-resume: unsupported request version; update camp on both machines and hop again")
	}
	request := envelope.Request
	selector, err := sanitizeResumeSelector(request.Selector)
	if err != nil {
		return hopResumeRequest{}, err
	}
	if selector == "" {
		return hopResumeRequest{}, camperrors.New("hop-resume: empty selector")
	}
	request.Selector = selector
	return request, nil
}

// --- the far side ---

// runHopRelay listens for this hop's far shell and carries its exchanges over
// in and out until in ends, which is how it learns the hop is over.
func runHopRelay(ctx context.Context, id string, in io.Reader, out io.Writer) error {
	path, err := hopRelaySocketPath(id)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return camperrors.Wrap(err, "hop-resume: relay directory")
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return camperrors.Wrap(err, "hop-resume: relay directory")
	}
	if info, err := os.Lstat(dir); err != nil || !info.IsDir() || !ownedByThisUser(info) {
		return camperrors.New("hop-resume: " + dir + " is not this user's own directory")
	}
	pruneHopRelays(dir)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return camperrors.Wrap(err, "hop-resume: relay listen")
	}
	defer func() {
		_ = ln.Close()
		_ = os.Remove(path)
	}()
	if err := os.Chmod(path, 0o600); err != nil {
		return camperrors.Wrap(err, "hop-resume: relay socket")
	}
	return relayHopExchanges(ctx, ln, in, out)
}

// pruneHopRelays removes sockets left by relays that were killed before they
// could clean up. A socket nothing listens on refuses the connection.
func pruneHopRelays(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.Type()&fs.ModeSocket == 0 {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		conn, err := net.DialTimeout("unix", path, 200*time.Millisecond)
		if err != nil {
			_ = os.Remove(path)
			continue
		}
		_ = conn.Close()
	}
}

func relayHopExchanges(ctx context.Context, ln net.Listener, in io.Reader, out io.Writer) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stopListening := context.AfterFunc(ctx, func() { _ = ln.Close() })
	defer stopListening()

	answers := make(chan hopRelayFrame)
	go func() {
		// The opening shell closing this stream is the end of the hop.
		defer cancel()
		reader := bufio.NewReaderSize(in, hopRelayMaxLine)
		for {
			line, err := readHopLine(reader)
			if errors.Is(err, errHopLineTooLong) {
				continue
			}
			if err != nil {
				return
			}
			var frame hopRelayFrame
			if json.Unmarshal(line, &frame) != nil || frame.Version != hopRelayVersion {
				continue
			}
			select {
			case answers <- frame:
			case <-ctx.Done():
				return
			}
		}
	}()

	toOpener := hopLineWriter{w: out}
	for seq := 1; ; seq++ {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return camperrors.Wrap(err, "hop-resume: relay accept")
		}
		err = relayHopExchange(ctx, conn, seq, &toOpener, answers)
		_ = conn.Close()
		if err != nil {
			return err
		}
	}
}

// relayHopExchange carries one far-shell connection. It returns an error only
// when the opening shell can no longer be written to.
func relayHopExchange(ctx context.Context, conn net.Conn, seq int, toOpener *hopLineWriter, answers <-chan hopRelayFrame) error {
	reader := bufio.NewReaderSize(conn, hopRelayMaxLine)
	_ = conn.SetReadDeadline(time.Now().Add(hopRelayRequestWait))
	request, err := readHopLine(reader)
	if err != nil || !json.Valid(request) {
		// Nothing to carry: a probe, or not a request.
		return nil
	}
	_ = conn.SetReadDeadline(time.Time{})
	if err := toOpener.write(hopRelayFrame{Version: hopRelayVersion, Seq: seq, Request: request}); err != nil {
		return err
	}
	gone := func() error {
		return toOpener.write(hopRelayFrame{Version: hopRelayVersion, Seq: seq, Gone: true})
	}
	// The far shell's next line is its confirmation. Anything it does before
	// it has been answered, hanging up included, ends the exchange.
	next := make(chan []byte, 1)
	go func() {
		line, err := readHopLine(reader)
		if err != nil {
			line = nil
		}
		next <- line
	}()
	answered := time.NewTimer(hopResumeTimeout)
	defer answered.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-next:
			return gone()
		case <-answered.C:
			return gone()
		case answer := <-answers:
			if answer.Seq != seq || answer.Response == nil {
				continue
			}
			if _, err := conn.Write(append(append([]byte(nil), answer.Response...), '\n')); err != nil {
				return gone()
			}
			var confirm hopRelayConfirm
			select {
			case line := <-next:
				if line != nil && json.Unmarshal(line, &confirm) == nil && confirm.Version == hopRelayVersion && confirm.Confirm {
					return toOpener.write(hopRelayFrame{Version: hopRelayVersion, Seq: seq, Confirm: true})
				}
			case <-time.After(hopRelayConfirmWait):
			case <-ctx.Done():
				return nil
			}
			return gone()
		}
	}
}

// sendOverHopRelay is the far shell's side of an exchange: request, response,
// and, for an accepted switch, the confirmation that lets the opening shell
// queue it. Only after that may this shell exit.
func sendOverHopRelay(conn net.Conn, request hopResumeRequest) error {
	out := hopLineWriter{w: conn}
	if err := out.write(hopResumeEnvelope{Version: hopRelayVersion, Request: request}); err != nil {
		return camperrors.Wrap(err, "hop-resume: tell the shell that opened this hop")
	}
	line, err := readHopLine(bufio.NewReaderSize(conn, hopRelayMaxLine))
	if err != nil {
		return camperrors.Wrap(err, "hop-resume: parent did not acknowledge the route; staying in this shell")
	}
	var response hopResumeResponse
	if err := json.Unmarshal(line, &response); err != nil {
		return camperrors.Wrap(err, "hop-resume: parent did not acknowledge the route; staying in this shell")
	}
	if response.Version != hopRelayVersion {
		return camperrors.New("hop-resume: unsupported parent acknowledgement; staying in this shell")
	}
	if response.Error != "" {
		return camperrors.New(response.Error)
	}
	if !response.Accepted {
		return camperrors.New("hop-resume: parent rejected the route; staying in this shell")
	}
	if err := out.write(hopRelayConfirm{Version: hopRelayVersion, Confirm: true}); err != nil {
		return camperrors.Wrap(err, "hop-resume: could not confirm the switch; staying in this shell")
	}
	return nil
}
