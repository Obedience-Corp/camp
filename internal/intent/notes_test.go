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

func TestMoveIntentToNote_ClearsLifecycleMetadata(t *testing.T) {
	svc, ctx := newNotesTestService(t)

	created, err := svc.CreateDirect(ctx, CreateOptions{
		Title:     "turn this intent into a note",
		Type:      TypeFeature,
		Concept:   "projects/camp",
		Body:      "important details stay in the body",
		Timestamp: time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect: %v", err)
	}
	oldPath := created.Path

	note, err := svc.MoveIntentToNote(ctx, created.ID)
	if err != nil {
		t.Fatalf("MoveIntentToNote: %v", err)
	}
	if note.Status != StatusNote {
		t.Errorf("Status = %q, want %q", note.Status, StatusNote)
	}
	if note.Type != "" {
		t.Errorf("Type = %q, want empty for note", note.Type)
	}
	if note.Concept != "" {
		t.Errorf("Concept = %q, want empty for note", note.Concept)
	}
	if !strings.Contains(note.Content, "important details stay in the body") {
		t.Errorf("Content = %q, want preserved body details", note.Content)
	}
	if filepath.Dir(note.Path) != filepath.Join(svc.intentsDir, "notes") {
		t.Errorf("note dir = %q, want notes/", filepath.Dir(note.Path))
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("old intent still present at %q", oldPath)
	}
	if _, err := svc.Get(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get(converted note) err = %v, want ErrNotFound", err)
	}
	if _, err := svc.GetNote(ctx, created.ID); err != nil {
		t.Errorf("GetNote(converted note): %v", err)
	}
	raw, err := os.ReadFile(note.Path)
	if err != nil {
		t.Fatalf("ReadFile(note): %v", err)
	}
	if strings.Contains(string(raw), "type:") || strings.Contains(string(raw), "concept:") {
		t.Fatalf("note frontmatter retained lifecycle metadata:\n%s", raw)
	}
}

func TestMoveNoteToStatus_UsesSelectedStatus(t *testing.T) {
	svc, ctx := newNotesTestService(t)

	note, err := svc.CreateNote(ctx, CreateOptions{
		Title:     "ready note",
		Timestamp: time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	oldPath := note.Path

	got, err := svc.MoveNoteToStatus(ctx, note.ID, StatusReady, TypeResearch)
	if err != nil {
		t.Fatalf("MoveNoteToStatus: %v", err)
	}
	if got.Status != StatusReady {
		t.Errorf("Status = %q, want %q", got.Status, StatusReady)
	}
	if got.Type != TypeResearch {
		t.Errorf("Type = %q, want %q", got.Type, TypeResearch)
	}
	if filepath.Dir(got.Path) != filepath.Join(svc.intentsDir, "ready") {
		t.Errorf("converted dir = %q, want ready/", filepath.Dir(got.Path))
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("note still present in notes/ at %q", oldPath)
	}
	if _, err := svc.Get(ctx, note.ID); err != nil {
		t.Errorf("converted note not resolvable as intent: %v", err)
	}
	if _, err := svc.GetNote(ctx, note.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetNote(converted intent) err = %v, want ErrNotFound", err)
	}
}

func TestConvert_DoesNotOverwriteExistingIntent(t *testing.T) {
	svc, ctx := newNotesTestService(t)
	ts := time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC)

	// A note and an intent created from the same title at the same timestamp
	// share an id (slug + timestamp).
	note, err := svc.CreateNote(ctx, CreateOptions{Title: "duplicate identity", Timestamp: ts})
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	existing, err := svc.CreateDirect(ctx, CreateOptions{Title: "duplicate identity", Type: TypeFeature, Timestamp: ts})
	if err != nil {
		t.Fatalf("CreateDirect: %v", err)
	}
	if note.ID != existing.ID {
		t.Fatalf("test precondition: ids should collide, got %q vs %q", note.ID, existing.ID)
	}

	// Converting the note must NOT overwrite the existing inbox intent.
	if _, err := svc.Convert(ctx, note.ID, TypeIdea); !errors.Is(err, ErrFileExists) {
		t.Fatalf("Convert on id collision err = %v, want ErrFileExists", err)
	}

	// The existing intent file is intact (still TypeFeature, not clobbered).
	raw, err := os.ReadFile(existing.Path)
	if err != nil {
		t.Fatalf("read existing intent: %v", err)
	}
	if !strings.Contains(string(raw), "type: feature") {
		t.Errorf("existing intent was overwritten by convert:\n%s", raw)
	}
	// The note still exists in the note store.
	if _, err := svc.GetNote(ctx, note.ID); err != nil {
		t.Errorf("note should still exist after rejected convert: %v", err)
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

func TestMoveIntentToNote_RejectsPartialID(t *testing.T) {
	svc, ctx := newNotesTestService(t)

	created, err := svc.CreateDirect(ctx, CreateOptions{
		Title:     "fix the widget renderer",
		Type:      TypeBug,
		Timestamp: time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect: %v", err)
	}

	if _, err := svc.MoveIntentToNote(ctx, "widget"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("MoveIntentToNote(partial id) err = %v, want ErrNotFound", err)
	}

	got, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get after rejected move: %v", err)
	}
	if got.Status != StatusInbox {
		t.Errorf("status = %q, want inbox; the intent must not move on a fuzzy guess", got.Status)
	}
}

func TestRename_WorksForNotes(t *testing.T) {
	svc, ctx := newNotesTestService(t)

	note, err := svc.CreateNote(ctx, CreateOptions{
		Title:     "socket path note",
		Timestamp: time.Date(2026, 1, 19, 15, 34, 12, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}

	renamed, err := svc.Rename(ctx, note.ID, "daemon socket path reminder")
	if err != nil {
		t.Fatalf("Rename note: %v", err)
	}
	if renamed.ID != note.ID {
		t.Errorf("id changed on rename: %q -> %q", note.ID, renamed.ID)
	}
	if renamed.Status != StatusNote {
		t.Errorf("status = %q, want %q; rename must keep it a note", renamed.Status, StatusNote)
	}
	base := filepath.Base(renamed.Path)
	if !strings.HasPrefix(base, "daemon-socket-path-reminder-") {
		t.Errorf("filename = %q, want slug of new title", base)
	}
	if filepath.Dir(renamed.Path) != filepath.Join(svc.intentsDir, "notes") {
		t.Errorf("renamed note dir = %q, want notes/", filepath.Dir(renamed.Path))
	}

	got, err := svc.GetNote(ctx, note.ID)
	if err != nil {
		t.Fatalf("GetNote after rename: %v", err)
	}
	if got.Title != "daemon socket path reminder" {
		t.Errorf("resolved title = %q", got.Title)
	}
	if _, err := svc.Get(ctx, note.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get(renamed note) err = %v, want ErrNotFound; notes stay out of intent resolution", err)
	}
}
