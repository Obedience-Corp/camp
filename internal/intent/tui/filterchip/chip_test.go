package filterchip

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestChip_SelectedValue(t *testing.T) {
	options := []string{"All", "Option1", "Option2"}
	chip := NewChip("Test", options)

	if got := chip.SelectedValue(); got != "All" {
		t.Errorf("SelectedValue() = %q, want %q", got, "All")
	}

	chip.SetSelected(1)
	if got := chip.SelectedValue(); got != "Option1" {
		t.Errorf("SelectedValue() = %q, want %q", got, "Option1")
	}
}

func TestChip_Focus(t *testing.T) {
	chip := NewChip("Test", []string{"All"})

	if chip.Focused {
		t.Error("Chip should not be focused initially")
	}

	chip.Focus()
	if !chip.Focused {
		t.Error("Chip should be focused after Focus()")
	}

	chip.Blur()
	if chip.Focused {
		t.Error("Chip should not be focused after Blur()")
	}
}

func TestChip_Toggle(t *testing.T) {
	chip := NewChip("Test", []string{"All", "Option1"})
	chip.Focus()

	if chip.Open {
		t.Error("Chip dropdown should not be open initially")
	}

	chip.Toggle()
	if !chip.Open {
		t.Error("Chip dropdown should be open after Toggle()")
	}

	chip.Toggle()
	if chip.Open {
		t.Error("Chip dropdown should be closed after second Toggle()")
	}
}

func TestChip_Update_OpenDropdown(t *testing.T) {
	chip := NewChip("Test", []string{"All", "Option1", "Option2"})
	chip.Focus()

	// Press enter to open dropdown
	msg := tea.KeyMsg{Type: tea.KeyEnter}
	chip, _ = chip.Update(msg)

	if !chip.Open {
		t.Error("Dropdown should be open after pressing Enter")
	}
}

func TestChip_Update_NavigateDropdown(t *testing.T) {
	chip := NewChip("Test", []string{"All", "Option1", "Option2"})
	chip.Focus()
	chip.Toggle() // Open dropdown

	// Navigate down
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	chip, _ = chip.Update(msg)

	if chip.highlighted != 1 {
		t.Errorf("highlighted = %d, want 1 after j", chip.highlighted)
	}

	// Navigate up
	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}}
	chip, _ = chip.Update(msg)

	if chip.highlighted != 0 {
		t.Errorf("highlighted = %d, want 0 after k", chip.highlighted)
	}
}

func TestChip_IsActive(t *testing.T) {
	chip := NewChip("Test", []string{"All", "Option1"})

	if chip.IsActive() {
		t.Error("Chip should not be active with first option selected")
	}

	chip.SetSelected(1)
	if !chip.IsActive() {
		t.Error("Chip should be active with non-first option selected")
	}
}

func TestBar_Focus(t *testing.T) {
	chip1 := NewChip("Type", []string{"All"})
	chip2 := NewChip("Status", []string{"All"})
	bar := NewBar(chip1, chip2)

	if bar.IsFocused() {
		t.Error("Bar should not be focused initially")
	}

	bar.Focus()
	if !bar.IsFocused() {
		t.Error("Bar should be focused after Focus()")
	}

	if bar.FocusedChip != 0 {
		t.Errorf("FocusedChip = %d, want 0", bar.FocusedChip)
	}

	if !bar.Chips[0].Focused {
		t.Error("First chip should be focused")
	}
}

func TestBar_FocusNext(t *testing.T) {
	chip1 := NewChip("Type", []string{"All"})
	chip2 := NewChip("Status", []string{"All"})
	bar := NewBar(chip1, chip2)
	bar.Focus()

	bar.FocusNext()
	if bar.FocusedChip != 1 {
		t.Errorf("FocusedChip = %d, want 1", bar.FocusedChip)
	}

	// Wrap around
	bar.FocusNext()
	if bar.FocusedChip != 0 {
		t.Errorf("FocusedChip = %d, want 0 after wrap", bar.FocusedChip)
	}
}

func TestBar_HasActiveFilters(t *testing.T) {
	chip1 := NewChip("Type", []string{"All", "Option1"})
	chip2 := NewChip("Status", []string{"All", "Active"})
	bar := NewBar(chip1, chip2)

	if bar.HasActiveFilters() {
		t.Error("Bar should not have active filters initially")
	}

	bar.Chips[0].SetSelected(1)
	if !bar.HasActiveFilters() {
		t.Error("Bar should have active filters after selection")
	}
}

func TestBar_ClearAll(t *testing.T) {
	chip1 := NewChip("Type", []string{"All", "Option1"})
	chip2 := NewChip("Status", []string{"All", "Active"})
	bar := NewBar(chip1, chip2)

	bar.Chips[0].SetSelected(1)
	bar.Chips[1].SetSelected(1)

	bar.ClearAll()

	if bar.Chips[0].Selected != 0 {
		t.Errorf("Chip 0 Selected = %d, want 0", bar.Chips[0].Selected)
	}
	if bar.Chips[1].Selected != 0 {
		t.Errorf("Chip 1 Selected = %d, want 0", bar.Chips[1].Selected)
	}
}
