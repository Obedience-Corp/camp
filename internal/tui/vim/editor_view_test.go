package vim

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestEditor_View(t *testing.T) {
	e := NewEditor("hello\nworld")
	e.SetSize(80, 10)

	cfg := DefaultViewConfig()
	view := e.View(cfg)

	if view == "" {
		t.Error("View() should not be empty")
	}
	if !strings.Contains(view, "hello") {
		t.Error("View() should contain 'hello'")
	}
	if !strings.Contains(view, "world") {
		t.Error("View() should contain 'world'")
	}
}

func TestEditor_ScrollOffset(t *testing.T) {
	// Create editor with many lines
	content := ""
	for i := 0; i < 20; i++ {
		if i > 0 {
			content += "\n"
		}
		content += "line" + string(rune('0'+i%10))
	}
	e := NewEditor(content)
	e.SetSize(80, 5)

	// Move to bottom
	e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	e.EnsureCursorVisible()

	// View should render visible lines
	cfg := DefaultViewConfig()
	view := e.View(cfg)
	if view == "" {
		t.Error("View() should not be empty after scrolling")
	}
}
