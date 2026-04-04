package explorer

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestModel_TabTogglesPreviewFocusWhenVisible(t *testing.T) {
	m := makeTestModel(1, 0)
	m.showPreview = true
	m.recalculateLayout()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)

	if !m.previewFocused {
		t.Fatal("expected preview to take focus when visible")
	}
	if m.focus != focusList {
		t.Fatalf("expected focus to remain on the main view, got %d", m.focus)
	}
	if m.filterBar.IsFocused() {
		t.Fatal("filter bar should not take focus when preview is visible")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)

	if m.previewFocused {
		t.Fatal("expected preview focus to toggle back to the list")
	}
	if m.focus != focusList {
		t.Fatalf("expected focus to remain on the main view after returning from preview, got %d", m.focus)
	}
}

func TestModel_FilterShortcutsOpenTargetChip(t *testing.T) {
	m := makeTestModel(1, 0)
	m.showPreview = true
	m.recalculateLayout()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	m = updated.(Model)

	if m.focus != focusFilterBar {
		t.Fatalf("expected type shortcut to focus the filter bar, got %d", m.focus)
	}
	if m.filterBar.FocusedChip != 0 {
		t.Fatalf("expected type shortcut to focus chip 0, got %d", m.filterBar.FocusedChip)
	}
	if !m.filterBar.Chips[0].Open {
		t.Fatal("expected type shortcut to open the type dropdown")
	}
	if m.previewFocused {
		t.Fatal("expected type shortcut to clear preview focus")
	}

	m = makeTestModel(1, 0)
	m.showPreview = true
	m.recalculateLayout()

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updated.(Model)

	if m.focus != focusFilterBar {
		t.Fatalf("expected status shortcut to focus the filter bar, got %d", m.focus)
	}
	if m.filterBar.FocusedChip != 1 {
		t.Fatalf("expected status shortcut to focus chip 1, got %d", m.filterBar.FocusedChip)
	}
	if !m.filterBar.Chips[1].Open {
		t.Fatal("expected status shortcut to open the status dropdown")
	}
}

func TestModel_HidingPreviewClearsPreviewFocus(t *testing.T) {
	m := makeTestModel(1, 0)
	m.showPreview = true
	m.previewFocused = true
	m.recalculateLayout()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	m = updated.(Model)

	if m.showPreview {
		t.Fatal("expected preview to be hidden after pressing v")
	}
	if m.previewFocused {
		t.Fatal("expected preview focus to clear when the preview is hidden")
	}
}

func TestModel_ForceHiddenPreviewClearsPreviewFocus(t *testing.T) {
	m := makeTestModel(1, 0)
	m.showPreview = true
	m.previewFocused = true
	m.recalculateLayout()

	m.width = 70
	m.recalculateLayout()

	if !m.previewForceHidden {
		t.Fatal("expected preview to be force-hidden in a narrow layout")
	}
	if m.previewFocused {
		t.Fatal("expected preview focus to clear when the layout force-hides the preview")
	}
}
