package explorer

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Obedience-Corp/camp/internal/intent"
)

func TestGroupNotes_SplitsActiveAndArchived(t *testing.T) {
	notes := []*intent.Intent{
		{ID: "a", Title: "active note", Status: intent.StatusNote},
		{ID: "b", Title: "archived note", Status: intent.StatusNoteArchived},
	}
	groups := groupNotes(notes)
	if len(groups) != 2 {
		t.Fatalf("groupNotes returned %d groups, want 2", len(groups))
	}
	if groups[0].Name != "Notes" || len(groups[0].Intents) != 1 {
		t.Errorf("Notes group = %+v, want 1 active note", groups[0])
	}
	if groups[1].Name != "Archived" || len(groups[1].Intents) != 1 {
		t.Errorf("Archived group = %+v, want 1 archived note", groups[1])
	}
}

func TestGroupExplorerItemsByStatus_PutsNotesFirst(t *testing.T) {
	items := []*intent.Intent{
		{ID: "i", Title: "inbox", Status: intent.StatusInbox},
		{ID: "n", Title: "note", Status: intent.StatusNote},
	}

	groups := groupExplorerItemsByStatus(items, false)
	if len(groups) == 0 {
		t.Fatal("groupExplorerItemsByStatus returned no groups")
	}
	if groups[0].Name != "Notes" || groups[0].Status != intent.StatusNote {
		t.Fatalf("first group = %+v, want Notes group", groups[0])
	}
	if len(groups[0].Intents) != 1 || groups[0].Intents[0].ID != "n" {
		t.Fatalf("Notes group intents = %+v, want note n", groups[0].Intents)
	}
}

func TestToggleNotesMode_FlipsAndResetsCursor(t *testing.T) {
	ctx := context.Background()
	m := NewModel(ctx, nil, nil, "/tmp/intents", "/tmp/campaign", "test-id", "", nil)
	m.cursorGroup = 3
	m.cursorItem = 2

	m.toggleNotesMode()
	if !m.notesMode {
		t.Error("toggleNotesMode did not enable notes mode")
	}
	if m.cursorGroup != 0 || m.cursorItem != -1 {
		t.Errorf("cursor not reset: group=%d item=%d", m.cursorGroup, m.cursorItem)
	}

	m.toggleNotesMode()
	if m.notesMode {
		t.Error("second toggle did not disable notes mode")
	}
}

func TestConvert_TUIFlow_NoteBecomesIntent(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	intentsDir := filepath.Join(tmp, "intents")
	svc := intent.NewIntentService(tmp, intentsDir)

	note, err := svc.CreateNote(ctx, intent.CreateOptions{
		Title:     "actionable note",
		Timestamp: time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}

	m := NewModel(ctx, svc, nil, intentsDir, "", "", "", nil)
	m.ready = true
	m.groups = groupExplorerItemsByStatus([]*intent.Intent{note}, false)
	m.cursorGroup = 0
	m.cursorItem = 0

	// Open the convert picker, pick Feature (index 1), and confirm.
	m.startConvert()
	if m.focus != focusConvertType {
		t.Fatalf("focus = %v, want focusConvertType", m.focus)
	}
	updated, _ := m.updateConvert(tea.KeyMsg{Type: tea.KeyDown})
	mp := updated.(*Model)
	convertModel, cmd := mp.updateConvert(tea.KeyMsg{Type: tea.KeyEnter})
	_ = convertModel
	if cmd == nil {
		t.Fatal("convert produced no command")
	}

	// Execute the async convert command.
	msg := cmd()
	if fin, ok := msg.(moveFinishedMsg); !ok {
		t.Fatalf("expected moveFinishedMsg, got %T", msg)
	} else if fin.err != nil {
		t.Fatalf("convert failed: %v", fin.err)
	}

	// The note is gone from the note store and now an intent in inbox.
	if _, err := svc.GetNote(ctx, note.ID); !errors.Is(err, intent.ErrNotFound) {
		t.Errorf("note still in note store, err = %v", err)
	}
	got, err := svc.Get(ctx, note.ID)
	if err != nil {
		t.Fatalf("converted intent not found: %v", err)
	}
	if got.Status != intent.StatusInbox {
		t.Errorf("Status = %q, want inbox", got.Status)
	}
	if got.Type != intent.TypeFeature {
		t.Errorf("Type = %q, want feature", got.Type)
	}
}

func TestUpdateNormal_COnSelectedNoteStartsConvert(t *testing.T) {
	ctx := context.Background()
	note := &intent.Intent{ID: "n", Title: "note", Status: intent.StatusNote}
	m := NewModel(ctx, nil, nil, "/tmp/intents", "", "", "", nil)
	m.ready = true
	m.groups = groupExplorerItemsByStatus([]*intent.Intent{note}, false)
	m.cursorGroup = 0
	m.cursorItem = 0

	updated, _ := m.updateNormal(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	got := updated.(Model)
	if got.focus != focusConvertType {
		t.Fatalf("focus = %v, want focusConvertType", got.focus)
	}
	if got.noteToConvert == nil || got.noteToConvert.ID != "n" {
		t.Fatalf("noteToConvert = %+v, want note n", got.noteToConvert)
	}
}

func TestStartAddTUI_FromNotesGroupUsesNoteMode(t *testing.T) {
	ctx := context.Background()
	m := NewModel(ctx, nil, nil, "/tmp/intents", "", "", "", nil)
	m.ready = true
	m.groups = groupExplorerItemsByStatus(nil, false)
	m.cursorGroup = 0
	m.cursorItem = -1

	m.startAddTUI()
	if !m.addNoteMode {
		t.Fatal("startAddTUI from Notes group should enable note mode")
	}
	if m.addModel == nil {
		t.Fatal("startAddTUI did not create an add model")
	}
}
