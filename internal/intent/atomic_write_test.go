package intent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAtomicWrites_RenameRoundTripsWithoutTempLitter(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	intentsDir := filepath.Join(tmp, "intents")
	svc := NewIntentService(tmp, intentsDir)

	created, err := svc.CreateDirect(ctx, CreateOptions{
		Title:     "atomic rename target",
		Type:      TypeIdea,
		Timestamp: time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect: %v", err)
	}

	renamed, err := svc.Rename(ctx, created.ID, "atomic rename target updated")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if _, err := os.Stat(renamed.Path); err != nil {
		t.Errorf("renamed file missing: %v", err)
	}

	got, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get after rename: %v", err)
	}
	if got.Title != "atomic rename target updated" {
		t.Errorf("title = %q after rename", got.Title)
	}
	assertNoTempLitter(t, intentsDir)
}

func TestAtomicWrites_NoteMovesRoundTripWithoutTempLitter(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	intentsDir := filepath.Join(tmp, "intents")
	svc := NewIntentService(tmp, intentsDir)

	note, err := svc.CreateNote(ctx, CreateOptions{
		Title:     "a captured note",
		Timestamp: time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}

	promoted, err := svc.MoveNoteToStatus(ctx, note.ID, StatusInbox, TypeIdea)
	if err != nil {
		t.Fatalf("MoveNoteToStatus: %v", err)
	}
	if promoted.Status != StatusInbox {
		t.Errorf("status = %q after promote, want inbox", promoted.Status)
	}

	got, err := svc.Get(ctx, note.ID)
	if err != nil {
		t.Fatalf("Get after promote: %v", err)
	}
	if got.Title != "a captured note" {
		t.Errorf("title = %q after promote", got.Title)
	}
	assertNoTempLitter(t, intentsDir)
}

func assertNoTempLitter(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.Contains(d.Name(), ".tmp-") {
			t.Errorf("leftover temp file from atomic write: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}
