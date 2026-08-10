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
)

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

func writeNativeClipboard(s string) error {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.Command("pbcopy")
	case "windows":
		c = exec.Command("cmd", "/c", "clip")
	default:
		c = exec.Command("xclip", "-selection", "clipboard")
	}
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
