package ui

import (
	"os"

	"golang.org/x/term"
)

// IsTerminal reports whether both stdin and stdout are attached to terminals.
func IsTerminal() bool {
	return isTerminalFile(os.Stdin) && isTerminalFile(os.Stdout)
}

func isTerminalFile(f *os.File) bool {
	return f != nil && term.IsTerminal(int(f.Fd()))
}
