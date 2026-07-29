//go:build dev

package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func sampleChoices() []WorkitemChoice {
	return []WorkitemChoice{
		{Path: "festivals/planning/sync-clone-SC0001", Title: "Sync clone transport", Ref: "SC0001", Type: "festival"},
		{Path: "workflow/design/billing-revamp", Title: "Billing revamp", Ref: "WI-abc123", Type: "design"},
		{Path: ".campaign/intents/active/telemetry", Title: "Telemetry intent", Ref: "WI-def456", Type: "intent"},
	}
}

func typePicker(p workitemPicker, text string) workitemPicker {
	for _, r := range text {
		p, _ = p.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return p
}

func TestWorkitemPicker_NoChoicesIsInactive(t *testing.T) {
	p := newWorkitemPicker(nil)
	if p.active() {
		t.Fatal("picker with no choices must be inactive")
	}
}

func TestWorkitemPicker_NoMatchKeepsSkipRow(t *testing.T) {
	p := newWorkitemPicker(sampleChoices())
	p = typePicker(p, "zzznomatch")
	if len(p.visible) != 0 {
		t.Fatalf("expected no matches, got %d", len(p.visible))
	}
	if p.cursor != 0 {
		t.Fatalf("cursor must rest on skip row with no matches, got %d", p.cursor)
	}

	p, _ = p.update(tea.KeyMsg{Type: tea.KeyEnter})
	if !p.done {
		t.Fatal("enter should finish the picker")
	}
	if p.selected != "" {
		t.Fatalf("no-match enter must skip, got %q", p.selected)
	}
}

func TestWorkitemPicker_FilterNarrowsAndSelectsTopMatch(t *testing.T) {
	p := newWorkitemPicker(sampleChoices())
	p = typePicker(p, "billing")

	if len(p.visible) != 1 {
		t.Fatalf("expected 1 match for 'billing', got %d", len(p.visible))
	}
	if p.cursor != 1 {
		t.Fatalf("non-empty query with matches should land cursor on first match, got %d", p.cursor)
	}

	p, _ = p.update(tea.KeyMsg{Type: tea.KeyEnter})
	if !p.done || p.selected != "workflow/design/billing-revamp" {
		t.Fatalf("selected = %q (done=%v), want billing path", p.selected, p.done)
	}
}

func TestWorkitemPicker_FiltersByFestivalID(t *testing.T) {
	p := newWorkitemPicker(sampleChoices())
	p = typePicker(p, "SC0001")
	if len(p.visible) != 1 {
		t.Fatalf("expected 1 match for festival id, got %d", len(p.visible))
	}
	p, _ = p.update(tea.KeyMsg{Type: tea.KeyEnter})
	if p.selected != "festivals/planning/sync-clone-SC0001" {
		t.Fatalf("selected = %q, want festival path", p.selected)
	}
}

func TestWorkitemPicker_EmptyQueryDefaultsToSkip(t *testing.T) {
	p := newWorkitemPicker(sampleChoices())
	if p.cursor != 0 {
		t.Fatalf("empty query must default cursor to skip row, got %d", p.cursor)
	}
	if len(p.visible) != 3 {
		t.Fatalf("empty query shows all choices, got %d", len(p.visible))
	}

	p, _ = p.update(tea.KeyMsg{Type: tea.KeyEnter})
	if p.selected != "" {
		t.Fatalf("enter on default skip row must skip, got %q", p.selected)
	}
}

func TestWorkitemPicker_NavigationClampsToBounds(t *testing.T) {
	p := newWorkitemPicker(sampleChoices())
	// Up from the skip row stays at 0.
	p, _ = p.update(tea.KeyMsg{Type: tea.KeyUp})
	if p.cursor != 0 {
		t.Fatalf("up from skip row must clamp to 0, got %d", p.cursor)
	}
	// Down past the last row clamps at len(visible).
	for i := 0; i < 10; i++ {
		p, _ = p.update(tea.KeyMsg{Type: tea.KeyDown})
	}
	if p.cursor != len(p.visible) {
		t.Fatalf("cursor must clamp to %d, got %d", len(p.visible), p.cursor)
	}
}

func TestWorkitemPicker_ViewShowsSkipRowAndHelp(t *testing.T) {
	p := newWorkitemPicker(sampleChoices())
	view := p.view()
	if !strings.Contains(view, noneRowLabel) {
		t.Error("view must render the skip row")
	}
	if !strings.Contains(view, "Esc: skip") {
		t.Error("view must document the skip action")
	}
}

func TestWindowBounds(t *testing.T) {
	tests := []struct {
		name              string
		cursor, n, size   int
		wantStart, wantEnd int
	}{
		{"fits", 0, 3, 8, 0, 3},
		{"top", 1, 20, 8, 0, 8},
		{"middle", 10, 20, 8, 5, 13},
		{"bottom", 20, 20, 8, 12, 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end := windowBounds(tt.cursor, tt.n, tt.size)
			if start != tt.wantStart || end != tt.wantEnd {
				t.Fatalf("windowBounds(%d,%d,%d) = (%d,%d), want (%d,%d)",
					tt.cursor, tt.n, tt.size, start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}
