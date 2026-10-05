package fresh

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/creack/pty"
	"golang.org/x/term"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

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
	// pty.Open's ioctl calls leave the master in blocking mode. Restore
	// nonblocking reads so Close can wake io.Copy when a surviving descendant
	// still holds the slave after the shell exits or is cancelled.
	if err := setFollowUpPTYNonblocking(master); err != nil {
		_ = slave.Close()
		return camperrors.WrapJoin(errFollowUpTTYUnavailable, err, "")
	}

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
		// Closing the master unblocks this copy when a descendant still holds
		// the slave. The command's exit status is the result that matters.
		_, _ = io.Copy(output, master)
	})
	waitErr := cmd.Wait()
	closeMaster()
	wg.Wait()
	return waitErr
}
