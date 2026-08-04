package explorer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Obedience-Corp/camp/internal/intent"
)

func TestFolderCreateRenameDelete_OnDisk(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	intentsDir := filepath.Join(tmp, "intents")
	svc := intent.NewIntentService(tmp, intentsDir)

	m := NewModel(ctx, svc, nil, intentsDir, tmp, "id", "", nil)
	m.ready = true
	m.groups = groupExplorerItemsByStatus(nil, nil, false, nil, false)
	// Cursor on Notes header
	for i, g := range m.groups {
		if g.Status == intent.StatusNote {
			m.cursorGroup = i
			m.cursorItem = -1
			break
		}
	}

	m.startFolderCreate()
	if m.focus != focusFolderInput {
		t.Fatalf("focus = %v, want focusFolderInput", m.focus)
	}
	m.folderInput.SetValue("reading/papers")
	updated, cmd := m.updateFolderInput(tea.KeyMsg{Type: tea.KeyEnter})
	_ = updated
	if cmd == nil {
		t.Fatal("create produced no command")
	}
	msg := cmd()
	fin, ok := msg.(folderFinishedMsg)
	if !ok || fin.err != nil {
		t.Fatalf("create result = %#v", msg)
	}
	gitkeep := filepath.Join(intentsDir, "notes", "reading", "papers", ".gitkeep")
	if _, err := os.Stat(gitkeep); err != nil {
		t.Fatalf(".gitkeep missing: %v", err)
	}

	// Rename papers -> books
	var m2 Model
	switch u := updated.(type) {
	case Model:
		m2 = u
	case *Model:
		m2 = *u
	default:
		t.Fatalf("unexpected model type %T", updated)
	}
	m2.service = svc
	// Point at the papers folder after rebuild-like grouping
	m2.groups = groupExplorerItemsByStatus(nil, nil, false, map[string]bool{"notes": true, "notes/reading": true}, false)
	// Create folder again not needed - exists on disk; synthesize from service folders by placing a note
	// Force cursor onto notes/reading/papers by building with fold and empty notes still shows folders from synthesize
	// synthesize only adds folders present in notes; so create a hand-placed empty status by calling startFolderRename after setting group manually
	m2.groups = append(m2.groups, IntentGroup{
		Name: "Papers", Status: intent.Status("notes/reading/papers"), Depth: 2,
	})
	m2.cursorGroup = len(m2.groups) - 1
	m2.cursorItem = -1
	m2.startFolderRename()
	m2.folderInput.SetValue("books")
	_, cmd = m2.updateFolderInput(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("rename produced no command")
	}
	msg = cmd()
	if fin, ok := msg.(folderFinishedMsg); !ok || fin.err != nil {
		t.Fatalf("rename result = %#v", msg)
	}
	if _, err := os.Stat(filepath.Join(intentsDir, "notes", "reading", "books", ".gitkeep")); err != nil {
		t.Fatalf("renamed folder missing: %v", err)
	}

	// Delete empty books
	m3 := NewModel(ctx, svc, nil, intentsDir, tmp, "id", "", nil)
	m3.ready = true
	m3.groups = []IntentGroup{{Name: "Books", Status: intent.Status("notes/reading/books"), Depth: 2}}
	m3.cursorGroup = 0
	m3.cursorItem = -1
	cmd = m3.deleteSelectedFolder()
	if cmd == nil {
		t.Fatal("delete produced no command")
	}
	msg = cmd()
	if fin, ok := msg.(folderFinishedMsg); !ok || fin.err != nil {
		t.Fatalf("delete result = %#v", msg)
	}
	if _, err := os.Stat(filepath.Join(intentsDir, "notes", "reading", "books")); !os.IsNotExist(err) {
		t.Fatalf("folder still present after delete")
	}
}

func TestMoveNoteToFolder_FromPicker(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	intentsDir := filepath.Join(tmp, "intents")
	svc := intent.NewIntentService(tmp, intentsDir)

	if _, err := svc.CreateNoteFolder(ctx, "reading"); err != nil {
		t.Fatalf("CreateNoteFolder: %v", err)
	}
	note, err := svc.CreateNote(ctx, intent.CreateOptions{
		Title:     "to move",
		Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}

	m := NewModel(ctx, svc, nil, intentsDir, tmp, "id", "", nil)
	m.ready = true
	m.startNoteFolderMove(note)
	if m.focus != focusNoteFolderMove {
		t.Fatalf("focus = %v", m.focus)
	}
	for _, opt := range m.noteFolderOptions {
		if opt.status == intent.StatusNoteMeetings || opt.status == intent.StatusNoteArchived {
			t.Fatalf("reserved destination offered by move picker: %+v", opt)
		}
	}
	// Find reading option
	idx := -1
	for i, opt := range m.noteFolderOptions {
		if opt.rel == "reading" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("reading option missing: %+v", m.noteFolderOptions)
	}
	m.noteFolderIdx = idx
	_, cmd := m.updateNoteFolderMove(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("move produced no command")
	}
	msg := cmd()
	if fin, ok := msg.(folderFinishedMsg); !ok || fin.err != nil {
		t.Fatalf("move result = %#v", msg)
	}
	got, err := svc.GetNote(ctx, note.ID)
	if err != nil {
		t.Fatalf("GetNote: %v", err)
	}
	if got.Status != intent.Status("notes/reading") {
		t.Fatalf("Status = %q, want notes/reading", got.Status)
	}
}
