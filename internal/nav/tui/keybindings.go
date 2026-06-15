package tui

import (
	"os"

	"golang.org/x/term"
)

// HelpText displays keybinding hints at the bottom of the picker.
const HelpText = "[↑↓/Tab: navigate] [Enter: select] [Esc: cancel] [Type to filter]"

// DefaultKeybindings documents the default go-fuzzyfinder keybindings.
var DefaultKeybindings = []Keybinding{
	{Key: "↑ / Ctrl+P", Action: "Move selection up"},
	{Key: "↓ / Ctrl+N / Tab", Action: "Move selection down"},
	{Key: "Enter", Action: "Select current item"},
	{Key: "Esc / Ctrl+C", Action: "Cancel picker"},
	{Key: "Backspace", Action: "Delete character from query"},
	{Key: "Ctrl+W", Action: "Delete word from query"},
	{Key: "Ctrl+U", Action: "Clear query"},
	{Key: "Any printable", Action: "Append to query"},
}

// Keybinding represents a keyboard shortcut and its action.
type Keybinding struct {
	// Key is the key or key combination.
	Key string
	// Action describes what the key does.
	Action string
}

// IsTerminal checks if stdin is an interactive terminal.
func IsTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}
