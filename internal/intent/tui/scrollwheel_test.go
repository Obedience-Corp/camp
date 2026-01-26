package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestScrollWheel_Selected(t *testing.T) {
	items := []string{"one", "two", "three", "four", "five"}
	sw := NewScrollWheel(items)

	if sw.Selected() != 0 {
		t.Errorf("Expected initial selection 0, got %d", sw.Selected())
	}

	if sw.SelectedValue() != "one" {
		t.Errorf("Expected selected value 'one', got %q", sw.SelectedValue())
	}

	sw.SetSelected(2)
	if sw.Selected() != 2 {
		t.Errorf("Expected selection 2, got %d", sw.Selected())
	}
	if sw.SelectedValue() != "three" {
		t.Errorf("Expected selected value 'three', got %q", sw.SelectedValue())
	}
}

func TestScrollWheel_Navigation(t *testing.T) {
	items := []string{"one", "two", "three"}
	sw := NewScrollWheel(items)
	sw.Focus()

	// Move down
	sw, _ = sw.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if sw.Selected() != 1 {
		t.Errorf("Expected selection 1 after down, got %d", sw.Selected())
	}

	// Move down again
	sw, _ = sw.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if sw.Selected() != 2 {
		t.Errorf("Expected selection 2 after down, got %d", sw.Selected())
	}

	// Try to move past end
	sw, _ = sw.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if sw.Selected() != 2 {
		t.Errorf("Expected selection to stay at 2, got %d", sw.Selected())
	}

	// Move up
	sw, _ = sw.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	if sw.Selected() != 1 {
		t.Errorf("Expected selection 1 after up, got %d", sw.Selected())
	}
}

func TestScrollWheel_VisibleRange(t *testing.T) {
	// Test with fewer than maxVisible items
	items := []string{"one", "two", "three"}
	sw := NewScrollWheel(items)

	start, end := sw.visibleRange()
	if start != 0 || end != 3 {
		t.Errorf("Expected range [0, 3], got [%d, %d]", start, end)
	}

	// Test with more than maxVisible items
	items = []string{"a", "b", "c", "d", "e", "f", "g"}
	sw = NewScrollWheel(items)
	sw.SetSelected(3) // Select 'd'

	start, end = sw.visibleRange()
	if end-start != maxVisibleItems {
		t.Errorf("Expected %d visible items, got %d", maxVisibleItems, end-start)
	}

	// Selected item should be in visible range
	if sw.selected < start || sw.selected >= end {
		t.Errorf("Selected item %d not in visible range [%d, %d)", sw.selected, start, end)
	}
}

func TestScrollWheel_Focus(t *testing.T) {
	sw := NewScrollWheel([]string{"a", "b"})

	if sw.Focused() {
		t.Error("Expected unfocused initially")
	}

	sw.Focus()
	if !sw.Focused() {
		t.Error("Expected focused after Focus()")
	}

	sw.Blur()
	if sw.Focused() {
		t.Error("Expected unfocused after Blur()")
	}
}

func TestScrollWheel_NoNavWithoutFocus(t *testing.T) {
	sw := NewScrollWheel([]string{"a", "b", "c"})
	// Don't focus

	sw, _ = sw.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if sw.Selected() != 0 {
		t.Errorf("Expected no navigation without focus, selection was %d", sw.Selected())
	}
}

func TestScrollWheel_EmptyItems(t *testing.T) {
	sw := NewScrollWheel([]string{})

	if sw.SelectedValue() != "" {
		t.Error("Expected empty string for empty items")
	}

	if sw.View() != "" {
		t.Error("Expected empty view for empty items")
	}
}

func TestScrollWheel_SetItems(t *testing.T) {
	sw := NewScrollWheel([]string{"a", "b", "c"})
	sw.SetSelected(2)

	// Set fewer items - selected should adjust
	sw.SetItems([]string{"x", "y"})
	if sw.Selected() != 1 {
		t.Errorf("Expected selection adjusted to 1, got %d", sw.Selected())
	}
}
