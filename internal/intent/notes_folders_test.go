package intent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNoteStatuses_StructuralDefaults(t *testing.T) {
	got := NoteStatuses()
	want := []Status{StatusNote, StatusNoteArchived, StatusNoteMeetings}
	if len(got) != len(want) {
		t.Fatalf("NoteStatuses() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("NoteStatuses()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNoteFolders_RootReservedAndUserOrder(t *testing.T) {
	svc, ctx := newNotesTestService(t)

	// User tree + nested + dot-prefixed (must be skipped).
	mustMkdir(t, filepath.Join(svc.intentsDir, "notes", "reading", "papers"))
	mustMkdir(t, filepath.Join(svc.intentsDir, "notes", "zzz"))
	mustMkdir(t, filepath.Join(svc.intentsDir, "notes", ".transcripts"))
	// Place one note under nested papers.
	writeHandPlacedNote(t, filepath.Join(svc.intentsDir, "notes", "reading", "papers"),
		"nested-paper-20260101-000001", "nested paper note")

	folders, err := svc.NoteFolders(ctx)
	if err != nil {
		t.Fatalf("NoteFolders: %v", err)
	}

	// Expect: root, meetings, archived, reading, reading/papers, zzz
	wantStatuses := []Status{
		StatusNote,
		StatusNoteMeetings,
		StatusNoteArchived,
		Status("notes/reading"),
		Status("notes/reading/papers"),
		Status("notes/zzz"),
	}
	if len(folders) != len(wantStatuses) {
		t.Fatalf("NoteFolders returned %d folders %#v, want %d %#v",
			len(folders), folderStatuses(folders), len(wantStatuses), wantStatuses)
	}
	for i, want := range wantStatuses {
		if folders[i].Status != want {
			t.Errorf("folders[%d].Status = %q, want %q", i, folders[i].Status, want)
		}
	}
	// Dot-prefixed must never appear.
	for _, f := range folders {
		if f.Status == Status("notes/.transcripts") {
			t.Fatal("NoteFolders must skip .transcripts")
		}
	}
	// Nested count is on the leaf folder only.
	if folders[4].Count != 1 {
		t.Errorf("notes/reading/papers Count = %d, want 1", folders[4].Count)
	}
	if folders[3].Count != 0 {
		t.Errorf("notes/reading Count = %d, want 0 (note is nested)", folders[3].Count)
	}
	if !folders[1].Reserved || !folders[2].Reserved {
		t.Error("meetings and archived should be Reserved")
	}
	if folders[3].Reserved || folders[4].Reserved {
		t.Error("user folders must not be Reserved")
	}
}

func TestNoteFolders_ReservedPresentWhenMissingOnDisk(t *testing.T) {
	svc, ctx := newNotesTestService(t)
	// Create only the notes root via CreateNote.
	if _, err := svc.CreateNote(ctx, CreateOptions{
		Title:     "root note only",
		Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("CreateNote: %v", err)
	}

	folders, err := svc.NoteFolders(ctx)
	if err != nil {
		t.Fatalf("NoteFolders: %v", err)
	}
	if len(folders) < 3 {
		t.Fatalf("expected at least root+2 reserved, got %d", len(folders))
	}
	if folders[1].Status != StatusNoteMeetings || folders[2].Status != StatusNoteArchived {
		t.Fatalf("reserved order = %q, %q; want meetings, archived",
			folders[1].Status, folders[2].Status)
	}
}

func TestListNotes_IncludesNestedUserFolders(t *testing.T) {
	svc, ctx := newNotesTestService(t)

	rootNote, err := svc.CreateNote(ctx, CreateOptions{
		Title:     "root note",
		Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}

	papersDir := filepath.Join(svc.intentsDir, "notes", "reading", "papers")
	mustMkdir(t, papersDir)
	writeHandPlacedNote(t, papersDir, "nested-paper-20260101-000002", "papers note")

	// Active list should include nested note, not require includeArchived.
	active, err := svc.ListNotes(ctx, false)
	if err != nil {
		t.Fatalf("ListNotes(active): %v", err)
	}
	if !containsNoteID(active, rootNote.ID) {
		t.Fatalf("ListNotes missing root note %q among %v", rootNote.ID, noteIDs(active))
	}
	if !containsNoteID(active, "nested-paper-20260101-000002") {
		t.Fatalf("ListNotes missing nested note among %v", noteIDs(active))
	}

	// Resolve by id must find the nested note.
	got, err := svc.GetNote(ctx, "nested-paper-20260101-000002")
	if err != nil {
		t.Fatalf("GetNote(nested): %v", err)
	}
	if got.Title != "papers note" {
		t.Errorf("GetNote title = %q, want %q", got.Title, "papers note")
	}
}

func TestListNotes_ExcludesArchivedUnlessRequested(t *testing.T) {
	svc, ctx := newNotesTestService(t)

	note, err := svc.CreateNote(ctx, CreateOptions{
		Title:     "to archive",
		Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	if _, err := svc.ArchiveNote(ctx, note.ID); err != nil {
		t.Fatalf("ArchiveNote: %v", err)
	}

	active, err := svc.ListNotes(ctx, false)
	if err != nil {
		t.Fatalf("ListNotes(active): %v", err)
	}
	if containsNoteID(active, note.ID) {
		t.Fatal("active ListNotes must not include archived note")
	}

	all, err := svc.ListNotes(ctx, true)
	if err != nil {
		t.Fatalf("ListNotes(all): %v", err)
	}
	if !containsNoteID(all, note.ID) {
		t.Fatal("includeArchived ListNotes must include archived note")
	}
}

func TestIsValidStatus_AcceptsNestedNoteFolders(t *testing.T) {
	if !isValidStatus(Status("notes/reading/papers")) {
		t.Error("nested note status should be valid")
	}
	if isValidStatus(Status("not-a-real-status")) {
		t.Error("unknown status should be invalid")
	}
}

func TestNormalizeNoteFolderRel_RejectsInvalid(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
		want    string
	}{
		{in: "", want: ""},
		{in: "reading/papers", want: "reading/papers"},
		{in: "Reading/Papers", want: "reading/papers"},
		{in: "notes/reading", want: "reading"},
		{in: "../escape", wantErr: true},
		{in: "/abs", wantErr: true},
		{in: "archived", wantErr: true},
		{in: "meetings", wantErr: true},
		{in: ".hidden", wantErr: true},
		{in: "Bad Name!", wantErr: true},
		{in: "reading/archived", want: "reading/archived"}, // reserved only at root
	}
	for _, tc := range cases {
		got, err := normalizeNoteFolderRel(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("normalizeNoteFolderRel(%q) err=nil, want error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("normalizeNoteFolderRel(%q) unexpected err: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("normalizeNoteFolderRel(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCreateRenameDeleteNoteFolder(t *testing.T) {
	svc, ctx := newNotesTestService(t)

	created, err := svc.CreateNoteFolder(ctx, "reading/papers")
	if err != nil {
		t.Fatalf("CreateNoteFolder: %v", err)
	}
	if created.Status != Status("notes/reading/papers") {
		t.Fatalf("Status = %q, want notes/reading/papers", created.Status)
	}
	gitkeep := filepath.Join(svc.intentsDir, "notes", "reading", "papers", ".gitkeep")
	if _, err := os.Stat(gitkeep); err != nil {
		t.Fatalf(".gitkeep missing after create: %v", err)
	}

	// Collision
	if _, err := svc.CreateNoteFolder(ctx, "reading/papers"); err == nil {
		t.Fatal("CreateNoteFolder should reject existing folder")
	}

	// Reserved
	if _, err := svc.CreateNoteFolder(ctx, "meetings"); err == nil {
		t.Fatal("CreateNoteFolder should reject reserved name")
	}
	if _, err := svc.CreateNoteFolder(ctx, "../x"); err == nil {
		t.Fatal("CreateNoteFolder should reject traversal")
	}

	if err := svc.RenameNoteFolder(ctx, "reading/papers", "reading/books"); err != nil {
		t.Fatalf("RenameNoteFolder: %v", err)
	}
	if _, err := os.Stat(filepath.Join(svc.intentsDir, "notes", "reading", "books", ".gitkeep")); err != nil {
		t.Fatalf("renamed folder missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(svc.intentsDir, "notes", "reading", "papers")); !os.IsNotExist(err) {
		t.Fatalf("old folder still present after rename")
	}

	if err := svc.DeleteNoteFolder(ctx, "reading/books"); err != nil {
		t.Fatalf("DeleteNoteFolder empty: %v", err)
	}

	// Non-empty refuse
	if _, err := svc.CreateNoteFolder(ctx, "scratch"); err != nil {
		t.Fatalf("CreateNoteFolder scratch: %v", err)
	}
	writeHandPlacedNote(t, filepath.Join(svc.intentsDir, "notes", "scratch"),
		"scratch-note-20260101-000001", "scratch note")
	if err := svc.DeleteNoteFolder(ctx, "scratch"); err == nil {
		t.Fatal("DeleteNoteFolder should refuse non-empty folder")
	}
}

func TestMoveNoteToFolder_AndCreateNoteWithFolder(t *testing.T) {
	svc, ctx := newNotesTestService(t)

	if _, err := svc.CreateNoteFolder(ctx, "reading/papers"); err != nil {
		t.Fatalf("CreateNoteFolder: %v", err)
	}

	// CreateNote with Folder lands under the folder.
	note, err := svc.CreateNote(ctx, CreateOptions{
		Title:     "folder create note",
		Folder:    "reading/papers",
		Timestamp: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateNote with Folder: %v", err)
	}
	wantDir := filepath.Join(svc.intentsDir, "notes", "reading", "papers")
	if filepath.Dir(note.Path) != wantDir {
		t.Fatalf("note dir = %q, want %q", filepath.Dir(note.Path), wantDir)
	}
	if note.Status != Status("notes/reading/papers") {
		t.Fatalf("Status = %q, want notes/reading/papers", note.Status)
	}

	// Move back to root.
	moved, err := svc.MoveNoteToFolder(ctx, note.ID, "")
	if err != nil {
		t.Fatalf("MoveNoteToFolder root: %v", err)
	}
	if moved.Status != StatusNote {
		t.Fatalf("Status after move to root = %q, want notes", moved.Status)
	}
	if filepath.Dir(moved.Path) != filepath.Join(svc.intentsDir, "notes") {
		t.Fatalf("path after move = %q", moved.Path)
	}

	// Move into folder again.
	moved2, err := svc.MoveNoteToFolder(ctx, note.ID, "reading/papers")
	if err != nil {
		t.Fatalf("MoveNoteToFolder papers: %v", err)
	}
	if moved2.Status != Status("notes/reading/papers") {
		t.Fatalf("Status = %q", moved2.Status)
	}

	// Missing destination folder.
	if _, err := svc.MoveNoteToFolder(ctx, note.ID, "does-not-exist"); err == nil {
		t.Fatal("MoveNoteToFolder should fail for missing folder")
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
}

func writeHandPlacedNote(t *testing.T, dir, id, title string) {
	t.Helper()
	status := statusFromNotesDir(dir)
	body := "---\n" +
		"id: " + id + "\n" +
		"title: " + title + "\n" +
		"status: " + status + "\n" +
		"created_at: 2026-01-01T00:00:00Z\n" +
		"---\n\n" +
		"# " + title + "\n"
	path := filepath.Join(dir, id+".md")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write note %s: %v", path, err)
	}
}

// statusFromNotesDir builds the Status string from a path under .../notes[/...].
func statusFromNotesDir(dir string) string {
	cleaned := filepath.ToSlash(filepath.Clean(dir))
	parts := strings.Split(cleaned, "/")
	for i, p := range parts {
		if p == "notes" {
			return strings.Join(parts[i:], "/")
		}
	}
	return "notes"
}

func folderStatuses(folders []NoteFolder) []Status {
	out := make([]Status, len(folders))
	for i, f := range folders {
		out[i] = f.Status
	}
	return out
}

func containsNoteID(notes []*Intent, id string) bool {
	for _, n := range notes {
		if n.ID == id {
			return true
		}
	}
	return false
}

func noteIDs(notes []*Intent) []string {
	out := make([]string, len(notes))
	for i, n := range notes {
		out[i] = n.ID
	}
	return out
}
