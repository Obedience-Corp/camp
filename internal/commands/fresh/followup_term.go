package fresh

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// followUpTailCapture bounds output captured from surviving descendants after
// the command exits. The window starts after the streaming writer finishes and
// never extends when more bytes arrive. Closed terminals drain immediately.
const followUpTailCapture = 100 * time.Millisecond

// errFollowUpTTYUnavailable means the command has not started and the caller
// should run it on a pipe instead.
var errFollowUpTTYUnavailable = errors.New("follow-up terminal unavailable")

// followUpUsesTerminal reports whether the command should see a terminal.
// Reduced motion still does: the spinner is already a single settled row,
// and the command's own color is independent of that. A dumb terminal does
// not, because the child would emit sequences that terminal cannot show.
func followUpUsesTerminal(f *os.File) bool {
	if f == nil || strings.EqualFold(os.Getenv("TERM"), "dumb") {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// runFollowUpOnTerminal attaches cmd's stdout and stderr to a pty the size of
// parent and copies the pty onto output. Stdin stays the null device, matching
// the pipe path, so a command that reads input gets EOF instead of a prompt
// nobody is answering.
//
// The slave is raw. Otherwise the pty turns each \n into \r\n and the user's
// cooked terminal turns that \n into \r\n again.
func runFollowUpOnTerminal(cmd *exec.Cmd, parent *os.File, output io.Writer) error {
	master, slave, err := pty.Open()
	if err != nil {
		return camperrors.WrapJoin(errFollowUpTTYUnavailable, err, "")
	}
	closeMaster := sync.OnceFunc(func() { _ = master.Close() })
	defer closeMaster()

	if ws, err := pty.GetsizeFull(parent); err == nil && ws != nil && ws.Cols > 0 && ws.Rows > 0 {
		_ = pty.Setsize(slave, ws)
	}
	if _, err := term.MakeRaw(int(slave.Fd())); err != nil {
		_ = slave.Close()
		return camperrors.WrapJoin(errFollowUpTTYUnavailable, err, "")
	}
	// Construct a fresh wrapper after enabling nonblocking mode so Go polling
	// can wake the reader when a surviving descendant still holds the slave.
	replacement, err := newFollowUpPTYMaster(master)
	if err != nil {
		_ = slave.Close()
		return camperrors.WrapJoin(errFollowUpTTYUnavailable, err, "")
	}
	if err := master.Close(); err != nil {
		_ = replacement.Close()
		_ = slave.Close()
		return camperrors.WrapJoin(errFollowUpTTYUnavailable, err, "")
	}
	master = replacement

	cmd.Stdout = slave
	cmd.Stderr = slave
	if err := cmd.Start(); err != nil {
		_ = slave.Close()
		return err
	}
	// The child holds the slave. Dropping this copy is what lets the master
	// read finish when the child exits.
	_ = slave.Close()

	var wg sync.WaitGroup
	wg.Go(func() {
		// A read deadline interrupts this copy after the command exits even
		// when a descendant retains the slave. Preserve the command exit status.
		_, _ = io.Copy(output, master)
	})
	waitErr := cmd.Wait()
	// Interrupt a pending read without discarding the bytes already queued.
	_ = master.SetReadDeadline(time.Now())
	wg.Wait()
	// Capture separately from forwarding: a slow writer must not consume the
	// finite window for draining queued child output. Descendant output after
	// this fixed window is deliberately excluded.
	_ = master.SetReadDeadline(time.Now().Add(followUpTailCapture))
	var tail bytes.Buffer
	_, _ = io.Copy(&tail, master)
	_, _ = tail.WriteTo(output)
	closeMaster()
	return waitErr
}
