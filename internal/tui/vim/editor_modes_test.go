package vim

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestEditor_VisualMode(t *testing.T) {
	e := NewEditor("hello world")

	// Enter visual mode
	e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	if e.Mode() != ModeVisual {
		t.Errorf("Mode() = %v, want ModeVisual", e.Mode())
	}

	// Move right to extend selection
	e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})

	// Delete selection
	e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if e.Content() != "lo world" {
		t.Errorf("Content() = %q, want \"lo world\"", e.Content())
	}
	if e.Mode() != ModeNormal {
		t.Errorf("Mode() = %v after delete, want ModeNormal", e.Mode())
	}
}

func TestEditor_MatchBracketVisual(t *testing.T) {
	e := NewEditor("(abc)")

	// Enter visual mode on (
	e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})

	// % to extend selection to )
	e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'%'}})

	// Delete the visual selection
	e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if e.Content() != "" {
		t.Errorf("v%%d: Content() = %q, want \"\"", e.Content())
	}
}

func TestEditor_CommandMode(t *testing.T) {
	e := NewEditor("hello")

	// Enter command mode
	e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	if e.Mode() != ModeCommand {
		t.Errorf("Mode() = %v, want ModeCommand", e.Mode())
	}

	// Type command
	e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if e.CommandBuffer() != "wq" {
		t.Errorf("CommandBuffer() = %q, want \"wq\"", e.CommandBuffer())
	}

	// Execute
	cmd, _ := e.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != "wq" {
		t.Errorf("Command result = %q, want \"wq\"", cmd)
	}
}

func TestEditor_EnterInNormalModeSaves(t *testing.T) {
	e := NewEditor("hello world")

	// Press Enter in normal mode - should return "wq" command
	cmd, _ := e.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != "wq" {
		t.Errorf("Enter in normal mode: cmd = %q, want \"wq\"", cmd)
	}
}
