package dungeon

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestStatusPickerModel_EscCancels(t *testing.T) {
	model, err := newStatusPickerModel("note.md", []StatusDir{
		{Name: "archived", ItemCount: 2},
	})
	if err != nil {
		t.Fatalf("newStatusPickerModel() error = %v", err)
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	next, ok := updated.(statusPickerModel)
	if !ok {
		t.Fatalf("expected statusPickerModel, got %T", updated)
	}
	if !next.done || !next.cancelled {
		t.Fatalf("expected esc to cancel picker, got done=%v cancelled=%v", next.done, next.cancelled)
	}
}

func TestStatusPickerModel_CtrlCAborts(t *testing.T) {
	model, err := newStatusPickerModel("note.md", []StatusDir{
		{Name: "archived", ItemCount: 2},
	})
	if err != nil {
		t.Fatalf("newStatusPickerModel() error = %v", err)
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	next, ok := updated.(statusPickerModel)
	if !ok {
		t.Fatalf("expected statusPickerModel, got %T", updated)
	}
	if !next.done || !next.aborted {
		t.Fatalf("expected ctrl+c to abort picker, got done=%v aborted=%v", next.done, next.aborted)
	}
}

func TestStatusPickerModel_EnterSelectsCurrentStatus(t *testing.T) {
	model, err := newStatusPickerModel("note.md", []StatusDir{
		{Name: "archived", ItemCount: 2},
		{Name: "completed", ItemCount: 1},
	})
	if err != nil {
		t.Fatalf("newStatusPickerModel() error = %v", err)
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	next, ok := updated.(statusPickerModel)
	if !ok {
		t.Fatalf("expected statusPickerModel, got %T", updated)
	}
	updated, _ = next.Update(tea.KeyMsg{Type: tea.KeyEnter})
	next, ok = updated.(statusPickerModel)
	if !ok {
		t.Fatalf("expected statusPickerModel, got %T", updated)
	}
	if !next.done {
		t.Fatal("expected enter to complete the picker")
	}
	if next.value != "completed" {
		t.Fatalf("value = %q, want %q", next.value, "completed")
	}
}
