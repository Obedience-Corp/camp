package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func key(s string) tea.KeyMsg {
	switch s {
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func TestTagOverlay_ToggleAndConfirm(t *testing.T) {
	o := NewTagOverlay([]string{"personal", "reference", "question"}, nil)

	// Toggle the first tag on.
	o, done := o.Update(key("space"))
	if done {
		t.Fatal("space should not finish the overlay")
	}
	// Move to the third tag and toggle it on.
	o, _ = o.Update(key("down"))
	o, _ = o.Update(key("down"))
	o, _ = o.Update(key("space"))

	o, done = o.Update(key("enter"))
	if !done {
		t.Fatal("enter should finish the overlay")
	}
	if o.Cancelled() {
		t.Fatal("enter should not cancel")
	}

	got := o.Result()
	if len(got) != 2 || got[0] != "personal" || got[1] != "question" {
		t.Fatalf("Result = %v, want [personal question]", got)
	}
}

func TestTagOverlay_CustomTag(t *testing.T) {
	o := NewTagOverlay([]string{"personal"}, nil)

	// Enter input mode and type a custom tag.
	o, _ = o.Update(key("i"))
	for _, r := range "urgent" {
		o, _ = o.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	o, _ = o.Update(key("enter")) // add custom tag
	o, done := o.Update(key("enter"))
	if !done {
		t.Fatal("second enter should confirm")
	}

	got := o.Result()
	found := false
	for _, tag := range got {
		if tag == "urgent" {
			found = true
		}
	}
	if !found {
		t.Fatalf("custom tag not in result: %v", got)
	}
}

func TestTagOverlay_PreselectsCurrent(t *testing.T) {
	o := NewTagOverlay([]string{"personal", "reference"}, []string{"reference"})
	o, _ = o.Update(key("enter"))
	got := o.Result()
	if len(got) != 1 || got[0] != "reference" {
		t.Fatalf("Result = %v, want [reference]", got)
	}
}

func TestTagOverlay_Cancel(t *testing.T) {
	o := NewTagOverlay([]string{"personal"}, nil)
	o, _ = o.Update(key("space"))
	o, done := o.Update(key("esc"))
	if !done || !o.Cancelled() {
		t.Fatalf("esc should finish and cancel: done=%v cancelled=%v", done, o.Cancelled())
	}
}
