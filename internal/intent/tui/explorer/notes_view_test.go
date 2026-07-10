package explorer

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/intent/tui"
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

func TestMove_TUIFlow_IntentBecomesNote(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	intentsDir := filepath.Join(tmp, "intents")
	svc := intent.NewIntentService(tmp, intentsDir)

	created, err := svc.CreateDirect(ctx, intent.CreateOptions{
		Title:     "not actionable anymore",
		Type:      intent.TypeFeature,
		Timestamp: time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect: %v", err)
	}

	m := NewModel(ctx, svc, nil, intentsDir, "", "", "", nil)
	m.ready = true
	m.intentToMove = created
	m.focus = focusMove
	m.moveStatusIdx = moveStatusIndex(t, m.currentMoveStatusOptions(), intent.StatusNote)

	updated, cmd := m.updateMove(tea.KeyMsg{Type: tea.KeyEnter})
	_ = updated
	if cmd == nil {
		t.Fatal("move to notes produced no command")
	}
	msg := cmd()
	if fin, ok := msg.(moveFinishedMsg); !ok {
		t.Fatalf("expected moveFinishedMsg, got %T", msg)
	} else if fin.err != nil {
		t.Fatalf("move to notes failed: %v", fin.err)
	}

	if _, err := svc.Get(ctx, created.ID); !errors.Is(err, intent.ErrNotFound) {
		t.Errorf("intent still resolves after move to note, err = %v", err)
	}
	note, err := svc.GetNote(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetNote after move: %v", err)
	}
	if note.Status != intent.StatusNote {
		t.Errorf("Status = %q, want notes", note.Status)
	}
	if note.Type != "" {
		t.Errorf("Type = %q, want empty for note", note.Type)
	}
}

func TestMove_TUIFlow_NoteBecomesReadyIntent(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	intentsDir := filepath.Join(tmp, "intents")
	svc := intent.NewIntentService(tmp, intentsDir)

	note, err := svc.CreateNote(ctx, intent.CreateOptions{
		Title:     "ready from notes",
		Timestamp: time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}

	m := NewModel(ctx, svc, nil, intentsDir, "", "", "", nil)
	m.ready = true
	m.intentToMove = note
	m.focus = focusMove
	m.moveStatusIdx = moveStatusIndex(t, m.currentMoveStatusOptions(), intent.StatusReady)

	updated, cmd := m.updateMove(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(*Model)
	if cmd != nil {
		t.Fatal("note move should open type picker before producing command")
	}
	if got.focus != focusConvertType {
		t.Fatalf("focus = %v, want focusConvertType", got.focus)
	}
	if got.convertTargetStatus != intent.StatusReady {
		t.Fatalf("convertTargetStatus = %q, want ready", got.convertTargetStatus)
	}

	updated, _ = got.updateConvert(tea.KeyMsg{Type: tea.KeyDown}) // Feature
	got = updated.(*Model)
	updated, cmd = got.updateConvert(tea.KeyMsg{Type: tea.KeyEnter})
	_ = updated
	if cmd == nil {
		t.Fatal("confirming note move produced no command")
	}
	msg := cmd()
	if fin, ok := msg.(moveFinishedMsg); !ok {
		t.Fatalf("expected moveFinishedMsg, got %T", msg)
	} else if fin.err != nil {
		t.Fatalf("move from note failed: %v", fin.err)
	}

	converted, err := svc.Get(ctx, note.ID)
	if err != nil {
		t.Fatalf("Get converted intent: %v", err)
	}
	if converted.Status != intent.StatusReady {
		t.Errorf("Status = %q, want ready", converted.Status)
	}
	if converted.Type != intent.TypeFeature {
		t.Errorf("Type = %q, want feature", converted.Type)
	}
}

func TestArchive_TUIFlow_NoteMovesToArchived(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	intentsDir := filepath.Join(tmp, "intents")
	svc := intent.NewIntentService(tmp, intentsDir)

	note, err := svc.CreateNote(ctx, intent.CreateOptions{
		Title:     "note to dungeon",
		Timestamp: time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}

	m := NewModel(ctx, svc, nil, intentsDir, "", "", "", nil)
	m.ready = true
	m.notesMode = true
	m.groups = groupNotes([]*intent.Intent{note})
	m.cursorGroup = 0
	m.cursorItem = 0

	// Dispatch the Archive action the note action menu now offers.
	_, cmd := m.handleActionMenuSelection(tui.ActionMenuSelectedMsg{Action: "archive"})
	if cmd == nil {
		t.Fatal("archive action produced no command")
	}
	msg := cmd()
	if fin, ok := msg.(archiveFinishedMsg); !ok {
		t.Fatalf("expected archiveFinishedMsg, got %T", msg)
	} else if fin.err != nil {
		t.Fatalf("archive failed: %v", fin.err)
	}

	archived, err := svc.GetNote(ctx, note.ID)
	if err != nil {
		t.Fatalf("GetNote after archive: %v", err)
	}
	if archived.Status != intent.StatusNoteArchived {
		t.Errorf("Status = %q, want %q", archived.Status, intent.StatusNoteArchived)
	}
}

func TestRestore_TUIFlow_ArchivedNoteBecomesActive(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	intentsDir := filepath.Join(tmp, "intents")
	svc := intent.NewIntentService(tmp, intentsDir)

	note, err := svc.CreateNote(ctx, intent.CreateOptions{
		Title:     "archived note to restore",
		Timestamp: time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	archived, err := svc.ArchiveNote(ctx, note.ID)
	if err != nil {
		t.Fatalf("ArchiveNote: %v", err)
	}

	m := NewModel(ctx, svc, nil, intentsDir, "", "", "", nil)
	m.ready = true
	m.notesMode = true
	m.groups = groupNotes([]*intent.Intent{archived})
	m.cursorGroup = 1 // Archived group
	m.cursorItem = 0

	_, cmd := m.handleActionMenuSelection(tui.ActionMenuSelectedMsg{Action: "restore"})
	if cmd == nil {
		t.Fatal("restore action produced no command")
	}
	msg := cmd()
	if fin, ok := msg.(moveFinishedMsg); !ok {
		t.Fatalf("expected moveFinishedMsg, got %T", msg)
	} else if fin.err != nil {
		t.Fatalf("restore failed: %v", fin.err)
	}

	restored, err := svc.GetNote(ctx, note.ID)
	if err != nil {
		t.Fatalf("GetNote after restore: %v", err)
	}
	if restored.Status != intent.StatusNote {
		t.Errorf("Status = %q, want %q", restored.Status, intent.StatusNote)
	}
}

// TestRestore_TUIFlow_NonArchivedNoOp pins that dispatching "restore" on a note
// that is not archived is an inert no-op: Go switch cases do not fall through,
// so it never reaches the "delete" case. The action menu already disables
// Restore for active notes; this guards the dispatch layer directly.
func TestRestore_TUIFlow_NonArchivedNoOp(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	intentsDir := filepath.Join(tmp, "intents")
	svc := intent.NewIntentService(tmp, intentsDir)

	note, err := svc.CreateNote(ctx, intent.CreateOptions{
		Title:     "active note, restore should no-op",
		Timestamp: time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}

	m := NewModel(ctx, svc, nil, intentsDir, "", "", "", nil)
	m.ready = true
	m.notesMode = true
	m.groups = groupNotes([]*intent.Intent{note})
	m.cursorGroup = 0
	m.cursorItem = 0

	updated, cmd := m.handleActionMenuSelection(tui.ActionMenuSelectedMsg{Action: "restore"})
	if cmd != nil {
		t.Fatalf("restore on active note produced a command: %T", cmd())
	}
	got := updated.(Model)
	if got.focus == focusConfirm {
		t.Error("restore on active note fell through into delete confirmation")
	}
	if got.pendingAction == "delete" {
		t.Errorf("pendingAction = %q, restore must not reach the delete path", got.pendingAction)
	}
	if got.statusMessage == "" {
		t.Error("restore on a non-archived note should surface a status message, not silently no-op")
	}

	// The note still exists and is unchanged.
	if _, err := svc.GetNote(ctx, note.ID); err != nil {
		t.Fatalf("note missing after restore no-op: %v", err)
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

func TestUpdateNormal_MOnSelectedNoteStartsMove(t *testing.T) {
	ctx := context.Background()
	note := &intent.Intent{ID: "n", Title: "note", Status: intent.StatusNote}
	m := NewModel(ctx, nil, nil, "/tmp/intents", "", "", "", nil)
	m.ready = true
	m.groups = groupExplorerItemsByStatus([]*intent.Intent{note}, false)
	m.cursorGroup = 0
	m.cursorItem = 0

	updated, _ := m.updateNormal(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	got := updated.(Model)
	if got.focus != focusMove {
		t.Fatalf("focus = %v, want focusMove", got.focus)
	}
	if got.intentToMove == nil || got.intentToMove.ID != "n" {
		t.Fatalf("intentToMove = %+v, want note n", got.intentToMove)
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

func moveStatusIndex(t *testing.T, options []moveStatusOption, status intent.Status) int {
	t.Helper()
	for i, opt := range options {
		if opt.status == status {
			return i
		}
	}
	t.Fatalf("status %q not found in move options %+v", status, options)
	return -1
}
