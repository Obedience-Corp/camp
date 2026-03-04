package explorer

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNewModel(t *testing.T) {
	ctx := context.Background()
	m := NewModel(ctx, nil, nil, "/tmp/intents", "/tmp/campaign", "test-id", "", nil)

	// Verify filter bar is initialized with correct chips
	if len(m.filterBar.Chips) != 2 {
		t.Errorf("Expected 2 filter chips, got %d", len(m.filterBar.Chips))
	}

	if m.filterBar.Chips[0].Label != "Type" {
		t.Errorf("Expected first chip label 'Type', got %q", m.filterBar.Chips[0].Label)
	}

	if m.filterBar.Chips[1].Label != "Status" {
		t.Errorf("Expected second chip label 'Status', got %q", m.filterBar.Chips[1].Label)
	}

	// Verify initial focus mode
	if m.focus != focusList {
		t.Errorf("Expected initial focus mode focusList, got %d", m.focus)
	}
}

func TestModel_TabToFilterBar(t *testing.T) {
	ctx := context.Background()
	m := NewModel(ctx, nil, nil, "/tmp/intents", "/tmp/campaign", "test-id", "", nil)

	// Set ready state (normally done by WindowSizeMsg)
	m.ready = true
	m.width = 100
	m.height = 30

	// Press Tab to move to filter bar
	msg := tea.KeyMsg{Type: tea.KeyTab}
	updated, _ := m.Update(msg)
	m = updated.(Model)

	if m.focus != focusFilterBar {
		t.Errorf("Expected focus mode focusFilterBar after Tab, got %d", m.focus)
	}

	if !m.filterBar.IsFocused() {
		t.Error("Expected filter bar to be focused")
	}
}

func TestModel_EscapeFromFilterBar(t *testing.T) {
	ctx := context.Background()
	m := NewModel(ctx, nil, nil, "/tmp/intents", "/tmp/campaign", "test-id", "", nil)

	// Set ready state and focus filter bar
	m.ready = true
	m.width = 100
	m.height = 30
	m.focus = focusFilterBar
	m.filterBar.Focus()

	// Press Escape to return to list
	msg := tea.KeyMsg{Type: tea.KeyEsc}
	updated, _ := m.Update(msg)
	m = updated.(Model)

	if m.focus != focusList {
		t.Errorf("Expected focus mode focusList after Escape, got %d", m.focus)
	}

	if m.filterBar.IsFocused() {
		t.Error("Expected filter bar to not be focused after Escape")
	}
}

func TestModel_FilterBarView(t *testing.T) {
	ctx := context.Background()
	m := NewModel(ctx, nil, nil, "/tmp/intents", "/tmp/campaign", "test-id", "", nil)

	// Set ready state
	m.ready = true
	m.width = 100
	m.height = 30

	// Render the view - should not panic
	view := m.View()

	// Check that filter bar is rendered (contains chip labels)
	if view == "" {
		t.Error("View should not be empty")
	}
}

func TestModel_FilterBarChipSelection(t *testing.T) {
	ctx := context.Background()
	m := NewModel(ctx, nil, nil, "/tmp/intents", "/tmp/campaign", "test-id", "", nil)

	// Set ready state
	m.ready = true
	m.width = 100
	m.height = 30

	// Focus filter bar
	m.focus = focusFilterBar
	m.filterBar.Focus()

	// Open dropdown with Enter
	msg := tea.KeyMsg{Type: tea.KeyEnter}
	updated, _ := m.Update(msg)
	m = updated.(Model)

	if !m.filterBar.Chips[0].Open {
		t.Error("Expected first chip dropdown to be open after Enter")
	}

	// Navigate down
	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	updated, _ = m.Update(msg)
	m = updated.(Model)

	// Select with Enter
	msg = tea.KeyMsg{Type: tea.KeyEnter}
	updated, _ = m.Update(msg)
	m = updated.(Model)

	if m.filterBar.Chips[0].Open {
		t.Error("Expected dropdown to be closed after selection")
	}

	if m.filterBar.Chips[0].Selected != 1 {
		t.Errorf("Expected selected index 1, got %d", m.filterBar.Chips[0].Selected)
	}
}
