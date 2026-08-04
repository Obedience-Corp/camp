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


// selectNoteInGroups finds the first group holding noteID and sets the cursor there.
func selectNoteInGroups(m *Model, noteID string) bool {
	for gi, g := range m.groups {
		for ii, it := range g.Intents {
			if it.ID == noteID {
				// Expand ancestors so the item is visible.
				for pi, p := range m.groups {
					for _, ci := range p.Children {
						if ci == gi {
							m.groups[pi].Expanded = true
						}
					}
				}
				m.groups[gi].Expanded = true
				m.cursorGroup = gi
				m.cursorItem = ii
				return true
			}
		}
	}
	return false
}

func TestGroupNotes_SplitsActiveAndArchived(t *testing.T) {
	notes := []*intent.Intent{
		{ID: "a", Title: "active note", Status: intent.StatusNote},
		{ID: "b", Title: "archived note", Status: intent.StatusNoteArchived},
	}
	groups := groupNotes(notes, nil, false)
	if len(groups) < 2 {
		t.Fatalf("groupNotes returned %d groups, want at least root+reserved", len(groups))
	}
	var root, archived *IntentGroup
	for i := range groups {
		switch groups[i].Status {
		case intent.StatusNote:
			root = &groups[i]
		case intent.StatusNoteArchived:
			archived = &groups[i]
		}
	}
	if root == nil || len(root.Intents) != 1 {
		t.Errorf("Notes root = %+v, want 1 active note", root)
	}
	if archived == nil || len(archived.Intents) != 1 {
		t.Errorf("Archived group = %+v, want 1 archived note", archived)
	}
}

func TestGroupExplorerItemsByStatus_PutsNotesAfterActive(t *testing.T) {
	items := []*intent.Intent{
		{ID: "i", Title: "inbox", Status: intent.StatusInbox},
		{ID: "n", Title: "note", Status: intent.StatusNote},
	}

	groups := groupExplorerItemsByStatus(items, false, nil, false)
	if len(groups) < 5 {
		t.Fatalf("groupExplorerItemsByStatus returned %d groups, want at least Inbox/Ready/Active/Notes/Dungeon", len(groups))
	}
	if groups[0].Name != "Inbox" || groups[1].Name != "Ready" || groups[2].Name != "Active" {
		t.Fatalf("top groups = %q/%q/%q, want Inbox/Ready/Active", groups[0].Name, groups[1].Name, groups[2].Name)
	}
	if groups[3].Name != "Notes" || groups[3].Status != intent.StatusNote {
		t.Fatalf("notes group = %+v, want Notes at index 3", groups[3])
	}
	if groups[3].Expanded {
		t.Fatal("Notes should be collapsed by default")
	}
	if len(groups[3].Intents) != 1 || groups[3].Intents[0].ID != "n" {
		t.Fatalf("Notes group intents = %+v, want note n", groups[3].Intents)
	}
	// Nested note must not be dropped.
	nested := append(items, &intent.Intent{ID: "nested", Title: "nested", Status: intent.Status("notes/reading/papers")})
	groups2 := groupExplorerItemsByStatus(nested, false, map[string]bool{
		"notes": true, "notes/reading": true, "notes/reading/papers": true,
	}, false)
	found := false
	for _, g := range groups2 {
		for _, it := range g.Intents {
			if it.ID == "nested" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("nested note under notes/reading/papers was dropped")
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
	m.groups = groupExplorerItemsByStatus([]*intent.Intent{note}, false, map[string]bool{"notes": true}, false)
	if !selectNoteInGroups(&m, note.ID) {
		t.Fatal("note not found in groups")
	}

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
	m.groups = groupNotes([]*intent.Intent{note}, nil, false)
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
	m.groups = groupNotes([]*intent.Intent{archived}, map[string]bool{
		"notes": true, "notes/archived": true,
	}, false)
	if !selectNoteInGroups(&m, archived.ID) {
		t.Fatal("archived note not found in groups")
	}

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
	m.groups = groupNotes([]*intent.Intent{note}, map[string]bool{"notes": true}, false)
	if !selectNoteInGroups(&m, note.ID) {
		t.Fatal("note not found in groups")
	}

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
	m.groups = groupExplorerItemsByStatus([]*intent.Intent{note}, false, map[string]bool{"notes": true}, false)
	if !selectNoteInGroups(&m, note.ID) {
		t.Fatal("note not found in groups")
	}

	updated, _ := m.updateNormal(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	got := updated.(Model)
	if got.focus != focusConvertType {
		t.Fatalf("focus = %v, want focusConvertType", got.focus)
	}
	if got.noteToConvert == nil || got.noteToConvert.ID != "n" {
		t.Fatalf("noteToConvert = %+v, want note n", got.noteToConvert)
	}
}

func TestUpdateNormal_MOnSelectedNoteStartsFolderMove(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	intentsDir := filepath.Join(tmp, "intents")
	svc := intent.NewIntentService(tmp, intentsDir)
	note, err := svc.CreateNote(ctx, intent.CreateOptions{
		Title:     "movable note",
		Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	if _, err := svc.CreateNoteFolder(ctx, "reading"); err != nil {
		t.Fatalf("CreateNoteFolder: %v", err)
	}

	m := NewModel(ctx, svc, nil, intentsDir, tmp, "id", "", nil)
	m.ready = true
	m.groups = groupExplorerItemsByStatus([]*intent.Intent{note}, false, map[string]bool{"notes": true}, false)
	if !selectNoteInGroups(&m, note.ID) {
		t.Fatal("note not found in groups")
	}

	updated, _ := m.updateNormal(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	got := updated.(Model)
	if got.focus != focusNoteFolderMove {
		t.Fatalf("focus = %v, want focusNoteFolderMove", got.focus)
	}
	if got.noteToMoveFolder == nil || got.noteToMoveFolder.ID != note.ID {
		t.Fatalf("noteToMoveFolder = %+v, want note", got.noteToMoveFolder)
	}
	if len(got.noteFolderOptions) == 0 {
		t.Fatal("expected folder picker options")
	}
}

func TestUpdateNormal_MOnLifecycleIntentStillStatusPicker(t *testing.T) {
	ctx := context.Background()
	item := &intent.Intent{ID: "i", Title: "inbox", Status: intent.StatusInbox}
	m := NewModel(ctx, nil, nil, "/tmp/intents", "", "", "", nil)
	m.ready = true
	m.groups = groupExplorerItemsByStatus([]*intent.Intent{item}, false, nil, false)
	// Select the inbox item.
	for gi, g := range m.groups {
		for ii, it := range g.Intents {
			if it.ID == "i" {
				m.groups[gi].Expanded = true
				m.cursorGroup = gi
				m.cursorItem = ii
			}
		}
	}

	updated, _ := m.updateNormal(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	got := updated.(Model)
	if got.focus != focusMove {
		t.Fatalf("focus = %v, want focusMove for lifecycle intent", got.focus)
	}
}

func TestStartAddTUI_FromNotesGroupUsesNoteMode(t *testing.T) {
	ctx := context.Background()
	m := NewModel(ctx, nil, nil, "/tmp/intents", "", "", "", nil)
	m.ready = true
	m.groups = groupExplorerItemsByStatus(nil, false, nil, false)
	// Notes sits after Inbox/Ready/Active.
	notesIdx := -1
	for i, g := range m.groups {
		if g.Status == intent.StatusNote {
			notesIdx = i
			break
		}
	}
	if notesIdx < 0 {
		t.Fatal("Notes group missing")
	}
	m.cursorGroup = notesIdx
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
