package intent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRename_UpdatesTitleAndFilenameKeepsID(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	intentsDir := filepath.Join(tmp, "intents")
	svc := NewIntentService(tmp, intentsDir)

	created, err := svc.CreateDirect(ctx, CreateOptions{
		Title:     "original title here",
		Type:      TypeIdea,
		Timestamp: time.Date(2026, 1, 19, 15, 34, 12, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect: %v", err)
	}
	originalID := created.ID

	renamed, err := svc.Rename(ctx, originalID, "a much better title")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}

	if renamed.ID != originalID {
		t.Errorf("id changed on rename: %q -> %q", originalID, renamed.ID)
	}
	if renamed.Title != "a much better title" {
		t.Errorf("Title = %q, want %q", renamed.Title, "a much better title")
	}
	base := filepath.Base(renamed.Path)
	if !strings.HasPrefix(base, "a-much-better-title-") {
		t.Errorf("filename = %q, want slug of new title", base)
	}
	if !strings.HasSuffix(base, "20260119-153412.md") {
		t.Errorf("filename = %q, want stable timestamp suffix preserved", base)
	}
	if _, err := os.Stat(created.Path); !os.IsNotExist(err) {
		t.Errorf("old file still present at %q", created.Path)
	}

	// Lookup by the stable id still resolves after rename.
	got, err := svc.Get(ctx, originalID)
	if err != nil {
		t.Fatalf("Get after rename: %v", err)
	}
	if got.Title != "a much better title" {
		t.Errorf("resolved title = %q", got.Title)
	}
}

func TestRename_SurvivesStatusMove(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	intentsDir := filepath.Join(tmp, "intents")
	svc := NewIntentService(tmp, intentsDir)

	created, err := svc.CreateDirect(ctx, CreateOptions{
		Title:     "fix the thing",
		Type:      TypeBug,
		Timestamp: time.Date(2026, 1, 19, 15, 34, 12, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect: %v", err)
	}

	renamed, err := svc.Rename(ctx, created.ID, "a much clearer title")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	renamedBase := filepath.Base(renamed.Path)
	if !strings.HasPrefix(renamedBase, "a-much-clearer-title-") {
		t.Fatalf("rename did not produce expected slug: %q", renamedBase)
	}

	// A normal triage move must NOT revert the renamed slug to <id>.md.
	moved, err := svc.Move(ctx, created.ID, StatusReady)
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if got := filepath.Base(moved.Path); got != renamedBase {
		t.Errorf("move reverted renamed basename: got %q, want %q", got, renamedBase)
	}
	if filepath.Dir(moved.Path) != filepath.Join(intentsDir, "ready") {
		t.Errorf("moved dir = %q, want ready", filepath.Dir(moved.Path))
	}
	// Still resolvable by id after the move, with the renamed title.
	got, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get after move: %v", err)
	}
	if got.Title != "a much clearer title" {
		t.Errorf("title = %q after move", got.Title)
	}

	// The same holds for an audited status change via UpdateDirect.
	active := StatusActive
	if _, _, err := svc.UpdateDirect(ctx, created.ID, UpdateOptions{Status: &active}); err != nil {
		t.Fatalf("UpdateDirect status change: %v", err)
	}
	afterUpdate, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get after UpdateDirect: %v", err)
	}
	if filepath.Base(afterUpdate.Path) != renamedBase {
		t.Errorf("UpdateDirect reverted renamed basename: got %q, want %q", filepath.Base(afterUpdate.Path), renamedBase)
	}
}

func TestRename_CollisionProducesDistinctFilename(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	intentsDir := filepath.Join(tmp, "intents")
	svc := NewIntentService(tmp, intentsDir)

	// Two intents sharing the same timestamp suffix would collide if renamed to
	// the same slug. Write one occupying the target filename directly.
	suffix := "20260119-153412"
	id := "first-title-" + suffix
	writeRenamedIntent(t, intentsDir, "inbox", id, id, "first title")
	// Occupy the slug the rename will target.
	occupied := filepath.Join(intentsDir, "inbox", "shared-slug-"+suffix+".md")
	if err := os.WriteFile(occupied, []byte("---\nid: other-"+suffix+"\ntitle: other\nstatus: inbox\ncreated_at: 2026-01-01T00:00:00Z\n---\n"), 0644); err != nil {
		t.Fatalf("write occupied: %v", err)
	}

	renamed, err := svc.Rename(ctx, id, "shared slug")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	base := filepath.Base(renamed.Path)
	if base == "shared-slug-"+suffix+".md" {
		t.Errorf("rename collided onto occupied file: %q", base)
	}
	if !strings.HasPrefix(base, "shared-slug-"+suffix) {
		t.Errorf("filename = %q, want disambiguated shared-slug base", base)
	}
	if _, err := os.Stat(occupied); err != nil {
		t.Errorf("occupied file should be untouched: %v", err)
	}
}

func TestRename_EmptyTitleRejected(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	intentsDir := filepath.Join(tmp, "intents")
	svc := NewIntentService(tmp, intentsDir)

	created, err := svc.CreateDirect(ctx, CreateOptions{
		Title:     "keep me",
		Timestamp: time.Date(2026, 1, 19, 15, 34, 12, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect: %v", err)
	}
	if _, err := svc.Rename(ctx, created.ID, "   "); err == nil {
		t.Error("Rename with empty title should error")
	}
}
