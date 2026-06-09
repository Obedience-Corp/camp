package intent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newNotesTestService(t *testing.T) (*IntentService, context.Context) {
	t.Helper()
	tmpDir := t.TempDir()
	return NewIntentService(tmpDir, filepath.Join(tmpDir, "intents")), context.Background()
}

func TestCreateNote_RoutesToNotesDir(t *testing.T) {
	svc, ctx := newNotesTestService(t)

	note, err := svc.CreateNote(ctx, CreateOptions{
		Title:     "remember to check the daemon socket path",
		Type:      TypeFeature, // should be ignored for notes
		Concept:   "projects/camp",
		Timestamp: time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}

	if note.Status != StatusNote {
		t.Errorf("Status = %q, want %q", note.Status, StatusNote)
	}
	if note.Type != "" {
		t.Errorf("Type = %q, want empty for a note", note.Type)
	}
	wantDir := filepath.Join(svc.intentsDir, "notes")
	if filepath.Dir(note.Path) != wantDir {
		t.Errorf("note dir = %q, want %q", filepath.Dir(note.Path), wantDir)
	}
	if _, err := os.Stat(note.Path); err != nil {
		t.Errorf("note file not written: %v", err)
	}
	if errs := note.Validate(); len(errs) > 0 {
		t.Errorf("note failed validation: %v", errs)
	}
}

func TestCreateDirect_IntentRoutesToInbox(t *testing.T) {
	svc, ctx := newNotesTestService(t)

	intent, err := svc.CreateDirect(ctx, CreateOptions{
		Title:     "add a search command",
		Type:      TypeFeature,
		Timestamp: time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect: %v", err)
	}
	if intent.Status != StatusInbox {
		t.Errorf("Status = %q, want %q", intent.Status, StatusInbox)
	}
	if filepath.Dir(intent.Path) != filepath.Join(svc.intentsDir, "inbox") {
		t.Errorf("intent dir = %q, want inbox", filepath.Dir(intent.Path))
	}
}

func TestList_ExcludesNotes(t *testing.T) {
	svc, ctx := newNotesTestService(t)

	if _, err := svc.CreateDirect(ctx, CreateOptions{
		Title:     "real intent that should be listed",
		Type:      TypeFeature,
		Timestamp: time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("CreateDirect: %v", err)
	}
	note, err := svc.CreateNote(ctx, CreateOptions{
		Title:     "a stray note that must not appear in intent lists",
		Timestamp: time.Date(2026, 6, 9, 10, 1, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}

	intents, err := svc.List(ctx, nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(intents) != 1 {
		t.Fatalf("List returned %d intents, want 1 (notes must be excluded)", len(intents))
	}
	for _, i := range intents {
		if i.ID == note.ID {
			t.Errorf("note %q leaked into intent List", note.ID)
		}
	}
}

func TestGetAndFind_DoNotResolveNotes(t *testing.T) {
	svc, ctx := newNotesTestService(t)

	note, err := svc.CreateNote(ctx, CreateOptions{
		Title:     "note invisible to intent lookups",
		Timestamp: time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}

	if _, err := svc.Get(ctx, note.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get(note) err = %v, want ErrNotFound", err)
	}
	if _, err := svc.Find(ctx, note.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Find(note) err = %v, want ErrNotFound", err)
	}

	got, err := svc.GetNote(ctx, note.ID)
	if err != nil {
		t.Fatalf("GetNote: %v", err)
	}
	if got.ID != note.ID {
		t.Errorf("GetNote ID = %q, want %q", got.ID, note.ID)
	}
}

func TestArchiveNote_MovesToArchived(t *testing.T) {
	svc, ctx := newNotesTestService(t)

	note, err := svc.CreateNote(ctx, CreateOptions{
		Title:     "note to be archived",
		Timestamp: time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	oldPath := note.Path

	archived, err := svc.ArchiveNote(ctx, note.ID)
	if err != nil {
		t.Fatalf("ArchiveNote: %v", err)
	}
	if archived.Status != StatusNoteArchived {
		t.Errorf("Status = %q, want %q", archived.Status, StatusNoteArchived)
	}
	wantDir := filepath.Join(svc.intentsDir, "notes", "archived")
	if filepath.Dir(archived.Path) != wantDir {
		t.Errorf("archived dir = %q, want %q", filepath.Dir(archived.Path), wantDir)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("old note file still present at %q", oldPath)
	}

	active, err := svc.ListNotes(ctx, false)
	if err != nil {
		t.Fatalf("ListNotes(active): %v", err)
	}
	if len(active) != 0 {
		t.Errorf("active notes = %d, want 0 after archive", len(active))
	}
	all, err := svc.ListNotes(ctx, true)
	if err != nil {
		t.Fatalf("ListNotes(all): %v", err)
	}
	if len(all) != 1 {
		t.Errorf("all notes = %d, want 1 including archived", len(all))
	}
}

func TestConvert_NoteBecomesIntent(t *testing.T) {
	svc, ctx := newNotesTestService(t)

	note, err := svc.CreateNote(ctx, CreateOptions{
		Title:     "this note turned out to be actionable",
		Timestamp: time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	oldPath := note.Path

	got, err := svc.Convert(ctx, note.ID, TypeFeature)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if got.Status != StatusInbox {
		t.Errorf("Status = %q, want %q", got.Status, StatusInbox)
	}
	if got.Type != TypeFeature {
		t.Errorf("Type = %q, want %q", got.Type, TypeFeature)
	}
	if filepath.Dir(got.Path) != filepath.Join(svc.intentsDir, "inbox") {
		t.Errorf("converted dir = %q, want inbox", filepath.Dir(got.Path))
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("note still present in notes/ at %q", oldPath)
	}

	// It now resolves as an intent and appears in intent listings.
	if _, err := svc.Get(ctx, note.ID); err != nil {
		t.Errorf("converted intent not resolvable via Get: %v", err)
	}
	intents, err := svc.List(ctx, nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(intents) != 1 {
		t.Errorf("List returned %d intents, want 1 after convert", len(intents))
	}
	// And it is gone from the notes store.
	notes, err := svc.ListNotes(ctx, true)
	if err != nil {
		t.Fatalf("ListNotes: %v", err)
	}
	if len(notes) != 0 {
		t.Errorf("notes = %d, want 0 after convert", len(notes))
	}
}

func TestConvert_RejectsInvalidType(t *testing.T) {
	svc, ctx := newNotesTestService(t)

	note, err := svc.CreateNote(ctx, CreateOptions{
		Title:     "note with a bad convert type",
		Timestamp: time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}

	if _, err := svc.Convert(ctx, note.ID, Type("bogus")); err == nil {
		t.Error("Convert with invalid type should error")
	}
	// The note must remain a note after a rejected convert.
	if _, err := svc.GetNote(ctx, note.ID); err != nil {
		t.Errorf("note should still exist after rejected convert: %v", err)
	}
}

func TestNoteFrontmatter_HasNotesStatus(t *testing.T) {
	svc, ctx := newNotesTestService(t)

	note, err := svc.CreateNote(ctx, CreateOptions{
		Title:     "frontmatter status should be notes",
		Timestamp: time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	raw, err := os.ReadFile(note.Path)
	if err != nil {
		t.Fatalf("read note: %v", err)
	}
	if !strings.Contains(string(raw), "status: notes") {
		t.Errorf("note frontmatter missing 'status: notes':\n%s", raw)
	}
}
