package explorer

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Obedience-Corp/camp/internal/intent"
)

func TestRenameAction_RenamesViaServiceAndReselects(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	intentsDir := filepath.Join(tmp, "intents")
	svc := intent.NewIntentService(tmp, intentsDir)

	created, err := svc.CreateDirect(ctx, intent.CreateOptions{
		Title:     "old title",
		Type:      intent.TypeIdea,
		Timestamp: time.Date(2026, 1, 19, 15, 34, 12, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect: %v", err)
	}

	m := NewModel(ctx, svc, nil, intentsDir, "", "", "", nil)
	m.ready = true
	m.groups = groupIntentsByStatus([]*intent.Intent{created}, false)
	// Select the created intent (Inbox group, first item).
	m.cursorGroup = 0
	m.cursorItem = 0

	m.startRename()
	if m.focus != focusRename {
		t.Fatalf("focus = %v, want focusRename", m.focus)
	}
	if m.renameInput.Value() != "old title" {
		t.Fatalf("rename input not prefilled: %q", m.renameInput.Value())
	}

	// Replace the title and confirm.
	m.renameInput.SetValue("brand new title")
	updated, cmd := m.updateRename(tea.KeyMsg{Type: tea.KeyEnter})
	mp := updated.(*Model)
	if cmd == nil {
		t.Fatal("rename produced no command")
	}
	msg := cmd()
	fin, ok := msg.(renameFinishedMsg)
	if !ok || fin.err != nil {
		t.Fatalf("rename failed: %#v", msg)
	}

	// The service-level rename took effect.
	got, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get after rename: %v", err)
	}
	if got.Title != "brand new title" {
		t.Errorf("title = %q, want renamed", got.Title)
	}
	if !strings.HasPrefix(filepath.Base(got.Path), "brand-new-title-") {
		t.Errorf("filename not regenerated: %q", filepath.Base(got.Path))
	}

	// Simulate the reload + reselect path.
	mp.pendingReselectID = fin.renamedID
	reloaded, _ := mp.Update(intentsLoadedMsg{intents: []*intent.Intent{got}})
	rm := reloaded.(Model)
	if sel := rm.SelectedIntent(); sel == nil || sel.ID != created.ID {
		t.Errorf("renamed intent not reselected after reload")
	}
}
