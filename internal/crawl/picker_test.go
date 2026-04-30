package crawl

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPickerModel_EscCancels(t *testing.T) {
	model, err := newPickerModel(Item{ID: "note.md", Title: "note.md"}, []Option{
		{Label: "archived", Action: ActionMove, Target: "archived", Count: 2},
	})
	if err != nil {
		t.Fatalf("newPickerModel() error = %v", err)
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	next, ok := updated.(pickerModel)
	if !ok {
		t.Fatalf("expected pickerModel, got %T", updated)
	}
	if !next.done || !next.cancelled {
		t.Fatalf("expected esc to cancel picker, got done=%v cancelled=%v", next.done, next.cancelled)
	}
}

func TestPickerModel_CtrlCAborts(t *testing.T) {
	model, err := newPickerModel(Item{ID: "note.md", Title: "note.md"}, []Option{
		{Label: "archived", Action: ActionMove, Target: "archived", Count: 2},
	})
	if err != nil {
		t.Fatalf("newPickerModel() error = %v", err)
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	next, ok := updated.(pickerModel)
	if !ok {
		t.Fatalf("expected pickerModel, got %T", updated)
	}
	if !next.done || !next.aborted {
		t.Fatalf("expected ctrl+c to abort picker, got done=%v aborted=%v", next.done, next.aborted)
	}
}

func TestPickerModel_EnterSelectsCurrentOption(t *testing.T) {
	model, err := newPickerModel(Item{ID: "note.md", Title: "note.md"}, []Option{
		{Label: "archived", Action: ActionMove, Target: "archived", Count: 2},
		{Label: "completed", Action: ActionMove, Target: "completed", Count: 1},
	})
	if err != nil {
		t.Fatalf("newPickerModel() error = %v", err)
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	next, ok := updated.(pickerModel)
	if !ok {
		t.Fatalf("expected pickerModel, got %T", updated)
	}
	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyEnter})
	next, ok = updated.(pickerModel)
	if !ok {
		t.Fatalf("expected pickerModel, got %T", updated)
	}
	if !next.done {
		t.Fatal("expected enter to complete the picker")
	}
	if next.value.Target != "completed" {
		t.Fatalf("value.Target = %q, want %q", next.value.Target, "completed")
	}
}

func TestPickerModel_EmptyOptionsRejected(t *testing.T) {
	if _, err := newPickerModel(Item{ID: "note.md"}, nil); err == nil {
		t.Fatal("expected error for empty options")
	}
}

func TestVisiblePickerRange_FewerThanWindow(t *testing.T) {
	start, end := visiblePickerRange(3, 1)
	if start != 0 || end != 3 {
		t.Errorf("got (%d,%d), want (0,3)", start, end)
	}
}

func TestVisiblePickerRange_WindowedNearEnd(t *testing.T) {
	start, end := visiblePickerRange(20, 19)
	if end != 20 {
		t.Errorf("end = %d, want 20", end)
	}
	if end-start != destinationPickerVisibleEntries {
		t.Errorf("window size = %d, want %d", end-start, destinationPickerVisibleEntries)
	}
}

func TestRenderOptionLabel_WithCount(t *testing.T) {
	got := renderOptionLabel(Option{Label: "archived", Count: 5})
	want := "archived (5 items)"
	if got != want {
		t.Errorf("renderOptionLabel = %q, want %q", got, want)
	}
}

func TestRenderOptionLabel_WithoutCount(t *testing.T) {
	got := renderOptionLabel(Option{Label: "archived"})
	if got != "archived" {
		t.Errorf("renderOptionLabel = %q, want %q", got, "archived")
	}
}
