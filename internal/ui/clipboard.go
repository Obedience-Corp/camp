package ui

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"

	osc52 "github.com/aymanbagabas/go-osc52/v2"
)

var (
	// clipboardOutput is the terminal Bubble Tea renders to. OSC 52 is a
	// non-printing control sequence, so writing it here asks the operator's
	// terminal to update its clipboard even when camp itself is across SSH.
	clipboardOutput = io.Writer(os.Stdout)

	// remoteClipboardSession is a seam for the environment check. Native
	// clipboard commands in an SSH session target the remote machine (and
	// xclip commonly has no display at all), not the computer in front of the
	// operator.
	remoteClipboardSession = func() bool {
		return os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != ""
	}

	nativeClipboardWrite = writeNativeClipboard

	// clipboardLookPath and clipboardRun are seams so the candidate ordering
	// can be tested on any host without installing a display server.
	clipboardLookPath = exec.LookPath
	clipboardRun      = runClipboardCommand
	clipboardGOOS     = runtime.GOOS
)

// errNoClipboardCommand reports that no platform clipboard command is
// installed. Callers fall back to OSC 52.
var errNoClipboardCommand = errors.New("no clipboard command available")

// WriteClipboard copies s to the operator's clipboard. Local sessions prefer
// the platform clipboard command; remote sessions use OSC 52 so the request
// travels through the terminal to the local computer. If a local clipboard
// command is unavailable, OSC 52 is also the fallback.
//
// Overridable in tests so callers do not touch the real clipboard.
var WriteClipboard = writeClipboard

func writeClipboard(s string) error {
	if remoteClipboardSession() {
		return writeTerminalClipboard(s)
	}

	nativeErr := nativeClipboardWrite(s)
	if nativeErr == nil {
		return nil
	}
	if terminalErr := writeTerminalClipboard(s); terminalErr != nil {
		return errors.Join(nativeErr, terminalErr)
	}
	return nil
}

// clipboardCommand is one candidate clipboard writer.
type clipboardCommand struct {
	name string
	args []string
}

func writeNativeClipboard(s string) error {
	candidates := clipboardCandidates(clipboardGOOS)

	var attemptErrs []error
	for _, candidate := range candidates {
		if _, err := clipboardLookPath(candidate.name); err != nil {
			continue
		}
		if err := clipboardRun(candidate, s); err != nil {
			attemptErrs = append(attemptErrs, err)
			continue
		}
		return nil
	}

	if len(attemptErrs) == 0 {
		return errNoClipboardCommand
	}
	return errors.Join(attemptErrs...)
}

// clipboardCandidates returns the clipboard commands to try, most likely to
// reach the operator's clipboard first.
//
// On Unix desktops the display-server environment variables order the list
// rather than filter it: a session under XWayland sets both, and only wl-copy
// reaches the compositor's clipboard, but an unset variable is not proof a
// tool will fail (subshells and login managers routinely drop them). So a
// missing variable demotes a candidate instead of removing it, and everything
// installed still gets a turn before the caller falls back to OSC 52.
func clipboardCandidates(goos string) []clipboardCommand {
	switch goos {
	case "darwin":
		return []clipboardCommand{{name: "pbcopy"}}
	case "windows":
		return []clipboardCommand{{name: "cmd", args: []string{"/c", "clip"}}}
	}

	wayland := clipboardCommand{name: "wl-copy"}
	x11 := []clipboardCommand{
		{name: "xclip", args: []string{"-selection", "clipboard"}},
		{name: "xsel", args: []string{"--clipboard", "--input"}},
	}

	switch {
	case os.Getenv("WAYLAND_DISPLAY") != "":
		return append([]clipboardCommand{wayland}, x11...)
	case os.Getenv("DISPLAY") != "":
		return append(x11, wayland)
	default:
		return append([]clipboardCommand{wayland}, x11...)
	}
}

func runClipboardCommand(candidate clipboardCommand, s string) error {
	c := exec.Command(candidate.name, candidate.args...)
	c.Stdin = strings.NewReader(s)
	return c.Run()
}

func writeTerminalClipboard(s string) error {
	sequence := osc52.New(s)
	// GNU screen needs the sequence wrapped for its outer terminal. tmux can
	// consume the plain sequence when set-clipboard is enabled, its normal
	// integration path, so STY is deliberately narrower than TERM=screen-*.
	if os.Getenv("STY") != "" {
		sequence = sequence.Screen()
	}
	_, err := sequence.WriteTo(clipboardOutput)
	return err
}
