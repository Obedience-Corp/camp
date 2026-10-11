package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
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
	"syscall"
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
// An acknowledged switch is provisional until that confirmation arrives. While
// the relay link is up the relay always ends an exchange by reporting the
// confirmation or the hang-up, however slow the link, so the opening shell
// waits for one of them. Only when the link itself is lost does the hop settle
// the doubt: a far shell that got its acknowledgement exits at once, so the
// switch counts if the hop ends within hopAckGrace of the loss, and is
// otherwise taken back and the hop freed for another. That grace is a
// judgement between two rare outcomes, both needing the relay link alone to
// fail at the instant of a switch: too long, and a far shell that never got
// its acknowledgement but exits soon after for another reason is followed;
// too short, and a far shell that did, but exits slowly, is not.
const (
	hopRelayVersion     = 2
	hopRelayMaxLine     = 8192
	hopRelayMaxRequest  = 4096 // leaves a frame room to carry any request
	hopRelayRequestWait = 5 * time.Second
	hopRelayConfirmWait = 5 * time.Second

	// hopRelayStartDelay lets the hop's own ssh claim its session first. An
	// ssh server caps sessions per connection (MaxSessions), and a relay that
	// took the last one would push the interactive ssh onto a second
	// connection with warnings in the user's terminal.
	hopRelayStartDelay = time.Second

	// hopRelayPruneAge keeps pruning away from a relay that is still between
	// creating its socket and listening on it.
	hopRelayPruneAge = 10 * time.Second

	// A relay whose link is lost while the hop lives is started again, a few
	// times, so one dropped connection does not end onward switching.
	hopRelayRestarts     = 3
	hopRelayRestartDelay = 2 * time.Second

	// hopRelayAwait is how long a far shell waits for a relay that is not up
	// yet: it starts a moment after the hop does, through a login shell.
	hopRelayAwait = 4 * time.Second
)

// hopAckGrace is how long an acknowledged switch waits for the hop to end once
// its relay link is lost. A variable so tests need not wait it out.
var hopAckGrace = 5 * time.Second

// hopAckBackstop outlasts every bound the relay itself keeps, for a relay that
// never reports how an acknowledged exchange ended. A variable for the same
// reason.
var hopAckBackstop = hopResumeTimeout + hopRelayConfirmWait + 5*time.Second

// errHopRelayLost marks a relay that was listening and whose link then ended.
var errHopRelayLost = camperrors.New("hop-resume: relay link lost")

// hopPrepareSpacing is the least time between an exchange that ended without a
// switch and the next preparation. A far process that asks and hangs up in a
// loop would otherwise start `camp switch` on this machine as fast as it could
// ask. A variable so tests need not wait it out.
var hopPrepareSpacing = time.Second

var errHopAlreadySwitching = camperrors.New("hop-resume: this hop is already switching; exit this shell to complete it")

var hopResumeRelayCmd = &cobra.Command{
	Use:    "relay <id> [forwarded-socket]",
	Hidden: true,
	Args:   cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		forwarded := ""
		if len(args) == 2 {
			forwarded = args[1]
		}
		return runHopRelay(cmd.Context(), args[0], forwarded, cmd.InOrStdin(), cmd.OutOrStdout())
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
	// Ready is the relay's first frame: it is listening for the far shell.
	Ready bool `json:"ready,omitempty"`
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
	path := filepath.Join(home, ".obey", "hop-resume", id+".sock")
	if len(path) >= hopSocketPathMax {
		// Said here so the far shell hears the reason, not just that no
		// relay is running.
		return "", camperrors.New("hop-resume: " + filepath.Dir(path) + " is too long a path to hold a relay's socket, so a hop to this user cannot switch onward by relay")
	}
	return path, nil
}

// hopRelayHostPath is the note a relay leaves beside its socket saying which
// machine it runs on. A home directory can be mounted on several, and whether
// a socket is dead can only be judged on the machine whose relay created it.
func hopRelayHostPath(socketPath string) string {
	return strings.TrimSuffix(socketPath, ".sock") + ".host"
}

func hopHostTag() string {
	host, _ := os.Hostname()
	sum := sha256.Sum256([]byte(host))
	return hex.EncodeToString(sum[:8])
}

// trustedHopSocket is the one rule for every socket a hopped shell is pointed
// at. The shell can outlive its hop and keeps the names its environment gave
// it, and whoever answers at one of them can end the shell with an
// acknowledgement. So a name is used only while it is a socket this user owns,
// in a directory where nobody else can put something of theirs in its place.
// An ssh server that binds the forwarded socket as the login user passes; a
// socket bound as anyone else, root included, does not.
func trustedHopSocket(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	dir, err := os.Stat(filepath.Dir(path))
	if err != nil {
		return err
	}
	reason := ""
	switch {
	case info.Mode()&os.ModeSocket == 0:
		reason = "it is not a socket"
	case !ownedByThisUser(info):
		reason = "it belongs to another user, as when the ssh server bound it as root or this shell changed user inside the hop"
	case !keepsOwnEntries(dir):
		reason = "others can replace what is in its directory"
		if os.Geteuid() == 0 {
			reason = "root does not trust a shared directory, where an ssh server may bind sockets as root for any user"
		}
	default:
		return nil
	}
	return camperrors.New(path + " is not a socket only this user could have put there: " + reason)
}

// dialHopSocket connects to a socket a hop named, under the whole rule: the
// path must be trusted, and so must the process that answers.
func dialHopSocket(ctx context.Context, path string) (net.Conn, error) {
	if err := trustedHopSocket(path); err != nil {
		return nil, err
	}
	dialer := net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, err
	}
	if err := trustedHopPeer(conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func ownedByThisUser(info os.FileInfo) bool {
	uid, ok := hopFileOwner(info)
	return ok && uid == os.Geteuid()
}

// keepsOwnEntries reports whether what this user puts in dir stays theirs.
// A directory only its owner can write qualifies when that owner is this user
// or root. A shared directory qualifies only when it is sticky, so nobody can
// replace an entry they do not own, and never for root: a root-binding ssh
// server creates root-owned sockets there on behalf of any user, so to root
// "a socket I own in /tmp" proves nothing.
func keepsOwnEntries(dir os.FileInfo) bool {
	uid, ok := hopFileOwner(dir)
	if !ok || (uid != os.Geteuid() && uid != 0) {
		return false
	}
	if dir.Mode().Perm()&0o022 == 0 {
		return true
	}
	return dir.Mode()&os.ModeSticky != 0 && os.Geteuid() != 0
}

// trustedHopPeer closes the gap between checking a path and connecting to it:
// the process that answered must run as this user too. Where the platform
// cannot say, the path rule stands alone.
func trustedHopPeer(conn net.Conn) error {
	if uid, ok := hopPeerUID(conn); ok && uid != os.Geteuid() {
		return camperrors.New("the listener there runs as another user")
	}
	return nil
}

// printableHopText keeps text that arrived from the other side of a hop from
// driving this terminal: control characters become spaces.
func printableHopText(text string) string {
	return strings.Map(func(r rune) rune {
		if hopControlRune(r) {
			return ' '
		}
		return r
	}, text)
}

func hopControlRune(r rune) bool { return r < 0x20 || (r >= 0x7f && r < 0xa0) }

// printableHopRequest refuses a request whose names carry control characters.
// They end up in this shell's own messages when a destination is not found.
func printableHopRequest(request hopResumeRequest) error {
	for _, text := range []string{request.Selector, request.Host, request.Scope.Org, request.Scope.Status} {
		if strings.ContainsFunc(text, hopControlRune) {
			return camperrors.New("hop-resume: request carries control characters")
		}
	}
	return nil
}

var errHopLineTooLong = camperrors.New("hop-resume: line too long")

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
	line, err := marshalHopLine(value)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, err = l.w.Write(line)
	return err
}

// marshalHopLine encodes value as one line. HTML escaping is off: it can make
// a line several times longer than what it carries, past the length a reader
// accepts.
func marshalHopLine(value any) ([]byte, error) {
	var line bytes.Buffer
	enc := json.NewEncoder(&line)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return nil, err
	}
	return line.Bytes(), nil
}

// --- the shell that opened the hop ---

// hopRelayLink is the opening shell's end of a relay's ssh session.
type hopRelayLink struct {
	fromFar io.Reader
	toFar   io.Writer
	// stop ends the relay's ssh and reports how it had ended, if it had.
	stop func() error
}

// hopRelayArgv is the ssh call that runs the relay. Its own options come first
// so they win over the hop's and the user's config:
//   - -T keeps it a byte stream under `RequestTTY force`.
//   - ControlMaster=no: it rides the hop's ControlMaster when one is up and
//     otherwise connects on its own, but never competes with the interactive
//     ssh to create one, which would print a warning into the user's terminal.
//   - ClearAllForwardings, -a, -x: it carries nothing but its stdio, so it must
//     not bind the forwards the user configured for the interactive session.
//
// The relay is told where the forwarded socket is so it can step aside where
// that socket already serves the far shell.
func hopRelayArgv(hop hopLine, id, forwardedSock string) []string {
	argv := append([]string{"-T", "-a", "-x", "-o", "ControlMaster=no", "-o", "ClearAllForwardings=yes"}, hop.opts...)
	return append(argv, hop.target, remote.CampCommandLine("hop-resume relay "+id+" "+remote.ShellQuote(forwardedSock)))
}

// startHopRelay launches the relay and returns at once. A relay that cannot
// start, including on a far camp that predates relays, is a link that reads as
// closed: the hop is unaffected and the forwarded socket remains.
func startHopRelay(ctx context.Context, hop hopLine, id, forwardedSock string) hopRelayLink {
	closed := hopRelayLink{fromFar: strings.NewReader(""), toFar: io.Discard, stop: func() error { return nil }}
	cmd := exec.CommandContext(ctx, "ssh", hopRelayArgv(hop, id, forwardedSock)...)
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
	// Killing the local client is enough and nothing waits on the far side:
	// the far relay sees its input end and removes its socket, and if the
	// server kills it first, the next relay to start there prunes the socket.
	var once sync.Once
	var ended error
	return hopRelayLink{fromFar: fromFar, toFar: toFar, stop: func() error {
		once.Do(func() {
			_ = toFar.Close()
			_ = cmd.Process.Kill()
			ended = cmd.Wait()
		})
		return ended
	}}
}

// serveHopRelay answers the requests a relay carries, for as long as the hop
// and the relay last, and returns the follow-up line of the switch that
// stands when they end: one the far shell confirmed, or one acknowledged and
// still unsettled when the hop ended. It returns errHopRelayLost when a relay
// that was listening lost its link with no switch standing.
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
			if json.Unmarshal(line, &frame) != nil || frame.Version == 0 {
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
		ready       bool // the relay reported that it is listening
		seq         int  // the exchange in flight, 0 when idle
		stop        = func() {}
		prepared    chan outcome
		winding     chan outcome // a cancelled preparation that has not returned yet
		notBefore   time.Time    // the next preparation may start no sooner
		waiting     int          // the request held until it may be prepared, 0 when none
		held        hopResumeRequest
		spaced      <-chan time.Time // the held request may be prepared now
		overdue     <-chan time.Time // it waited too long on a cancelled preparation
		follow      string
		acked       bool             // acknowledged: provisional until confirmed
		confirmed   bool             // the far shell confirmed: final
		unconfirmed <-chan time.Time // the provisional switch has waited too long
	)
	defer func() { stop() }()
	// abandon ends the exchange in flight without a switch. Its preparation,
	// if still running, is cancelled, and the next is held off a moment.
	abandon := func() {
		if seq == 0 || confirmed {
			return
		}
		stop()
		hop.release()
		if prepared != nil {
			winding = prepared
		}
		notBefore = time.Now().Add(hopPrepareSpacing)
		seq, prepared, follow, acked, unconfirmed = 0, nil, "", false, nil
	}
	respond := func(to int, err error) error {
		response := hopResumeResponse{Version: hopRelayVersion, Accepted: err == nil}
		if err != nil {
			response.Error = err.Error()
		}
		raw, marshalErr := marshalHopLine(response)
		if marshalErr != nil {
			return marshalErr
		}
		return out.write(hopRelayFrame{Version: hopRelayVersion, Seq: to, Response: bytes.TrimSpace(raw)})
	}
	// broken ends serving when the relay can no longer be written to. A switch
	// the far shell confirmed stands all the same. Otherwise a relay that was
	// listening has lost its link, as when reading from it ends.
	broken := func(err error) (string, error) {
		if confirmed {
			return follow, nil
		}
		abandon()
		if ready {
			err = errHopRelayLost
		}
		return "", err
	}
	// drop forgets the held request.
	drop := func() { waiting, spaced, overdue = 0, nil, nil }
	// begin prepares the held request once nothing stands in its way: one
	// preparation at a time, and none too soon after an exchange that did not
	// switch. Until then the request waits here, where a hang-up can still
	// reach it, rather than in a preparation nobody is waiting for.
	begin := func() error {
		if waiting == 0 || winding != nil {
			return nil
		}
		if wait := time.Until(notBefore); wait > 0 {
			if spaced == nil {
				spaced = time.After(wait)
			}
			return nil
		}
		to, request := waiting, held
		drop()
		if !hop.reserve() {
			return respond(to, errHopAlreadySwitching)
		}
		seq = to
		prepareCtx, cancelPrepare := context.WithTimeout(ctx, hopResumeTimeout-time.Second)
		stop = cancelPrepare
		done := make(chan outcome, 1)
		prepared = done
		go func() {
			follow, err := prepare(prepareCtx, request)
			done <- outcome{follow, err}
		}()
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			// The hop is over. A far shell that was acknowledged moments
			// ago and is gone now acted on it.
			if acked {
				return follow, nil
			}
			abandon()
			return "", ctx.Err()
		case err := <-ended:
			if confirmed {
				return follow, nil
			}
			if !acked {
				abandon()
				if ready {
					err = errHopRelayLost
				}
				return "", err
			}
			// The link went before the confirmation. Whether the far shell
			// acted shows in the hop: wait for it to end, or for the grace
			// to run out.
			ended, frames = nil, nil
			unconfirmed = time.After(hopAckGrace)
		case <-unconfirmed:
			abandon()
			if frames == nil {
				return "", errHopRelayLost
			}
		case <-winding:
			winding, overdue = nil, nil
			if err := begin(); err != nil {
				return broken(err)
			}
		case <-spaced:
			spaced = nil
			if err := begin(); err != nil {
				return broken(err)
			}
		case <-overdue:
			to := waiting
			drop()
			if err := respond(to, camperrors.New("hop-resume: the previous request is still being cancelled; try again")); err != nil {
				return broken(err)
			}
		case result := <-prepared:
			prepared = nil
			if err := respond(seq, result.err); err != nil {
				return broken(err)
			}
			if result.err != nil {
				abandon()
				continue
			}
			follow, acked, unconfirmed = result.follow, true, time.After(hopAckBackstop)
		case frame := <-frames:
			switch {
			case frame.Ready && frame.Version == hopRelayVersion:
				ready = true
			case frame.Seq <= 0:
				// Not an exchange a relay would number.
			case frame.Version != hopRelayVersion:
				// Say so, or the far shell waits out its deadline.
				if frame.Request != nil {
					if err := respond(frame.Seq, errHopRelayVersion); err != nil {
						return broken(err)
					}
				}
			case frame.Request != nil:
				// The relay moves on only once an exchange is over.
				abandon()
				drop()
				request, err := hopRelayRequest(frame.Request)
				if err != nil {
					if err := respond(frame.Seq, err); err != nil {
						return broken(err)
					}
					continue
				}
				waiting, held = frame.Seq, request
				if winding != nil {
					overdue = time.After(hopRelayRequestWait)
				}
				if err := begin(); err != nil {
					return broken(err)
				}
			case frame.Gone && frame.Seq == waiting:
				drop()
			case frame.Gone && frame.Seq == seq:
				abandon()
			case frame.Confirm && frame.Seq == seq && acked:
				confirmed, unconfirmed = true, nil
			}
		}
	}
}

var errHopRelayVersion = camperrors.New("hop-resume: unsupported request version; update camp on both machines and hop again")

func hopRelayRequest(raw json.RawMessage) (hopResumeRequest, error) {
	var envelope hopResumeEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return hopResumeRequest{}, camperrors.Wrap(err, "hop-resume: request")
	}
	if envelope.Version != hopRelayVersion {
		return hopResumeRequest{}, errHopRelayVersion
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
//
// Where the forwarded socket already serves this user, the relay steps aside
// at once: the far shell keeps the route it has always had, and the relay does
// not hold one of the connection's sessions for the life of the hop. The
// decision is made once. If that socket stops serving later, as when another
// user on a machine with a root-binding ssh server has it bind over the path,
// this hop cannot switch onward and the far shell is told so; staying to
// watch would cost every hop a session to guard against that.
func runHopRelay(ctx context.Context, id, forwardedSock string, in io.Reader, out io.Writer) error {
	path, err := hopRelaySocketPath(id)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	// Before the forwarded-socket dial, which can take seconds. The relay's id
	// is already on the process list, and a directory an earlier tool left
	// open to others must not stay that way while someone can still create
	// entries in it. Failure is reported below, after a relay that can step
	// aside has done so.
	tightenHopRelayDir(dir)
	if forwardedSock != "" {
		// The same test the far shell will apply when it dials.
		if conn, err := dialHopSocket(ctx, forwardedSock); err == nil {
			_ = conn.Close()
			return nil
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return camperrors.Wrap(err, "hop-resume: relay directory")
	}
	// Checked before anything is changed there, and without following a link.
	if info, err := os.Lstat(dir); err != nil || !info.IsDir() || !ownedByThisUser(info) {
		return camperrors.New("hop-resume: " + dir + " is not this user's own directory")
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return camperrors.Wrap(err, "hop-resume: relay directory")
	}
	pruneHopRelays(dir)
	// Only this hop's opening shell starts a relay with this id. Whatever is
	// already at this path is a predecessor it gave up on, or something
	// planted while the directory was open to others. A socket there may
	// still be running, its link dead but its input not yet closed, and
	// would answer nobody. A file or a link would make Listen fail and this
	// hop would have no relay. Take the path over rather than defer to it.
	if _, err := os.Lstat(path); err == nil {
		_ = os.Remove(path)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return camperrors.Wrap(err, "hop-resume: relay listen")
	}
	// A relay that was taken over must not remove its successor's socket.
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	bound, err := os.Lstat(path)
	if err != nil {
		_ = ln.Close()
		return camperrors.Wrap(err, "hop-resume: relay socket")
	}
	defer func() {
		_ = ln.Close()
		if now, err := os.Lstat(path); err == nil && os.SameFile(bound, now) {
			_ = os.Remove(path)
			_ = os.Remove(hopRelayHostPath(path))
		}
	}()
	if err := os.Chmod(path, 0o600); err != nil {
		return camperrors.Wrap(err, "hop-resume: relay socket")
	}
	// Without its note the socket still serves; it is only never pruned.
	// A symlink or fifo is not a note, and is not opened.
	_ = writeHopRelayHost(hopRelayHostPath(path))
	return relayHopExchanges(ctx, ln, in, out)
}

// tightenHopRelayDir closes dir to other users when this user owns it.
// It does not report failure; runHopRelay reports the same conditions once
// it knows the relay is not stepping aside.
func tightenHopRelayDir(dir string) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || !ownedByThisUser(info) {
		return
	}
	_ = os.Chmod(dir, 0o700)
}

// errHopRelayNote is a path that is not a regular-file note. A symlink names
// some other file, and opening a fifo, directory, or device can block.
var errHopRelayNote = errors.New("hop-resume: relay note is not a regular file")

// openHopRelayHost opens a relay note only when it is a regular file.
// The check comes before the open. The open is also nonblocking, so a note
// replaced by a fifo between the two cannot stall prune or the write.
func openHopRelayHost(path string, flag int) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) || flag&os.O_CREATE == 0 {
			return nil, err
		}
	} else if !info.Mode().IsRegular() {
		return nil, errHopRelayNote
	}
	f, err := os.OpenFile(path, flag|hopNoFollowFlag()|hopNonblockFlag(), 0o600)
	if err != nil {
		return nil, err
	}
	opened, statErr := f.Stat()
	if statErr != nil || !opened.Mode().IsRegular() {
		_ = f.Close()
		if statErr != nil {
			return nil, statErr
		}
		return nil, errHopRelayNote
	}
	return f, nil
}

// writeHopRelayHost records which machine this socket belongs to. Anything
// other than a regular file is left untouched.
func writeHopRelayHost(path string) error {
	f, err := openHopRelayHost(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return err
	}
	_, err = f.Write([]byte(hopHostTag()))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// readHopRelayHost reads the note beside a relay socket, and only that file.
// A symlink, fifo, directory, or device is not a note. The note is a short
// host tag; anything longer is not one either, and is not read in full.
func readHopRelayHost(path string) ([]byte, error) {
	f, err := openHopRelayHost(path, os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(io.LimitReader(f, 64))
}

// pruneHopRelays removes sockets left by relays on this machine that were
// killed before they could clean up. A socket is judged only if the note
// beside it names this machine, and removed only if nothing listens on it: a
// connection refused. Any other outcome leaves it alone.
func pruneHopRelays(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	here := hopHostTag()
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || !strings.HasSuffix(entry.Name(), ".sock") || info.Mode()&fs.ModeSocket == 0 || time.Since(info.ModTime()) < hopRelayPruneAge {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if note, err := readHopRelayHost(hopRelayHostPath(path)); err != nil || string(note) != here {
			continue
		}
		conn, err := net.DialTimeout("unix", path, 200*time.Millisecond)
		if errors.Is(err, syscall.ECONNREFUSED) {
			_ = os.Remove(path)
			_ = os.Remove(hopRelayHostPath(path))
		}
		if err == nil {
			_ = conn.Close()
		}
	}
}

func relayHopExchanges(ctx context.Context, ln net.Listener, in io.Reader, out io.Writer) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stopListening := context.AfterFunc(ctx, func() { _ = ln.Close() })
	defer stopListening()

	var answers hopRelayAnswers
	go func() {
		// The opening shell closing this stream is the end of the hop, so
		// this reader must never wait on anything else.
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
			if json.Unmarshal(line, &frame) == nil && frame.Version == hopRelayVersion {
				answers.deliver(frame)
			}
		}
	}()

	toOpener := hopLineWriter{w: out}
	// A far login shell may have printed without ending its line; start ours
	// on a fresh one so the first frame is not glued to that.
	if _, err := io.WriteString(out, "\n"); err != nil {
		return camperrors.Wrap(err, "hop-resume: relay stream")
	}
	if err := toOpener.write(hopRelayFrame{Version: hopRelayVersion, Ready: true}); err != nil {
		return camperrors.Wrap(err, "hop-resume: relay stream")
	}
	for seq := 1; ; seq++ {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return camperrors.Wrap(err, "hop-resume: relay accept")
		}
		err = relayHopExchange(ctx, conn, seq, &toOpener, &answers)
		_ = conn.Close()
		if err != nil {
			return err
		}
	}
}

// hopRelayAnswers hands the opening shell's answer to the exchange it names
// and drops every other frame: an answer to an exchange the far shell already
// abandoned has nowhere to go.
type hopRelayAnswers struct {
	mu      sync.Mutex
	seq     int
	waiting chan hopRelayFrame
}

func (a *hopRelayAnswers) expect(seq int) <-chan hopRelayFrame {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.seq, a.waiting = seq, make(chan hopRelayFrame, 1)
	return a.waiting
}

func (a *hopRelayAnswers) over() {
	a.mu.Lock()
	a.seq, a.waiting = 0, nil
	a.mu.Unlock()
}

func (a *hopRelayAnswers) deliver(frame hopRelayFrame) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.waiting == nil || frame.Seq != a.seq || frame.Response == nil {
		return
	}
	select {
	case a.waiting <- frame:
	default:
	}
}

// relayHopExchange carries one far-shell connection. It returns an error only
// when the opening shell can no longer be written to.
func relayHopExchange(ctx context.Context, conn net.Conn, seq int, toOpener *hopLineWriter, answers *hopRelayAnswers) error {
	stopClose := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopClose()
	reader := bufio.NewReaderSize(conn, hopRelayMaxRequest)
	_ = conn.SetReadDeadline(time.Now().Add(hopRelayRequestWait))
	request, err := readHopLine(reader)
	if err != nil || !json.Valid(request) {
		// Nothing to carry: a probe, or not a request.
		return nil
	}
	_ = conn.SetReadDeadline(time.Time{})
	answer := answers.expect(seq)
	defer answers.over()
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
	unanswered := time.NewTimer(hopResumeTimeout)
	defer unanswered.Stop()
	var answered hopRelayFrame
	select {
	case <-ctx.Done():
		return nil
	case <-next:
		return gone()
	case answered = <-answer:
	case <-unanswered.C:
		// An answer that arrived with the deadline still counts.
		select {
		case answered = <-answer:
		default:
			return gone()
		}
	}
	if _, err := conn.Write(append(append([]byte(nil), answered.Response...), '\n')); err != nil {
		return gone()
	}
	unconfirmed := time.NewTimer(hopRelayConfirmWait)
	defer unconfirmed.Stop()
	line, ok := awaitHopLine(ctx, next, unconfirmed.C)
	if !ok {
		return nil
	}
	var confirm hopRelayConfirm
	if line != nil && json.Unmarshal(line, &confirm) == nil && confirm.Version == hopRelayVersion && confirm.Confirm {
		return toOpener.write(hopRelayFrame{Version: hopRelayVersion, Seq: seq, Confirm: true})
	}
	return gone()
}

// awaitHopLine waits for the far shell's next line until the deadline fires,
// nil if it sent none. A line that arrived with the deadline still counts: the
// far shell acts on having sent its confirmation, so it must not be reported
// as gone. ok is false when ctx ended first.
func awaitHopLine(ctx context.Context, next <-chan []byte, deadline <-chan time.Time) (line []byte, ok bool) {
	select {
	case line = <-next:
	case <-ctx.Done():
		return nil, false
	case <-deadline:
		select {
		case line = <-next:
		default:
		}
	}
	return line, true
}

// sendOverHopRelay is the far shell's side of an exchange: request, response,
// and, for an accepted switch, the confirmation that makes it final. Only
// after that may this shell exit.
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
		return camperrors.New(printableHopText(response.Error))
	}
	if !response.Accepted {
		return camperrors.New("hop-resume: parent rejected the route; staying in this shell")
	}
	if err := out.write(hopRelayConfirm{Version: hopRelayVersion, Confirm: true}); err != nil {
		return camperrors.Wrap(err, "hop-resume: could not confirm the switch; staying in this shell")
	}
	return nil
}
