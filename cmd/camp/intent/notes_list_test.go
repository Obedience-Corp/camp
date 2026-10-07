package intent

import (
	"bytes"
	"path/filepath"
	"slices"
	"testing"
	"time"

	intentcore "github.com/Obedience-Corp/camp/internal/intent"
)

const notesListTestRoot = "/camp-notes-list-test"

func noteEntryFixture(id, folder string, created time.Time) intentcore.NoteEntry {
	dir := filepath.Join(notesListTestRoot, ".campaign", "intents", "notes", filepath.FromSlash(folder))
	return intentcore.NoteEntry{
		Note: &intentcore.Intent{
			ID:        id,
			Title:     "title " + id,
			Status:    intentcore.StatusNote,
			Author:    "agent",
			CreatedAt: created,
			Path:      filepath.Join(dir, id+".md"),
		},
		Folder: folder,
	}
}

func noteEntryIDs(entries []intentcore.NoteEntry) []string {
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.Note.ID)
	}
	return ids
}

func TestSortNoteEntriesNewestFirst(t *testing.T) {
	base := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	entries := []intentcore.NoteEntry{
		noteEntryFixture("oldest", "", base),
		noteEntryFixture("tie-b", "reading", base.Add(time.Hour)),
		noteEntryFixture("newest", "meetings", base.Add(2*time.Hour)),
		noteEntryFixture("tie-a", "", base.Add(time.Hour)),
	}
	entries[0].Note.UpdatedAt = base.Add(10 * time.Hour)

	sortNoteEntriesNewestFirst(entries)

	want := []string{"newest", "tie-a", "tie-b", "oldest"}
	if got := noteEntryIDs(entries); !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v (created_at desc, id asc on ties, updated_at ignored)", got, want)
	}
}

func TestFilterNoteEntriesByFolderMatchesExactly(t *testing.T) {
	now := time.Now()
	all := func() []intentcore.NoteEntry {
		return []intentcore.NoteEntry{
			noteEntryFixture("root", "", now),
			noteEntryFixture("reading", "reading", now),
			noteEntryFixture("papers", "reading/papers", now),
			noteEntryFixture("meeting", "meetings", now),
		}
	}

	tests := []struct {
		folder string
		want   []string
	}{
		{folder: "", want: []string{"root"}},
		{folder: "reading", want: []string{"reading"}},
		{folder: "reading/papers", want: []string{"papers"}},
		{folder: "meetings", want: []string{"meeting"}},
		{folder: "archived", want: []string{}},
	}
	for _, tt := range tests {
		t.Run("folder="+tt.folder, func(t *testing.T) {
			got := noteEntryIDs(filterNoteEntriesByFolder(all(), tt.folder))
			if !slices.Equal(got, tt.want) {
				t.Fatalf("filter %q = %v, want %v", tt.folder, got, tt.want)
			}
		})
	}
}

func TestSplitNotesStatus(t *testing.T) {
	tests := []struct {
		name      string
		in        []string
		wantNotes bool
		wantRest  []string
	}{
		{name: "none", in: nil, wantNotes: false, wantRest: []string{}},
		{name: "notes only", in: []string{"notes"}, wantNotes: true, wantRest: []string{}},
		{name: "case and singular", in: []string{" NOTE ", "inbox"}, wantNotes: true, wantRest: []string{"inbox"}},
		{name: "lifecycle untouched", in: []string{"ready", "dungeon/done"}, wantNotes: false, wantRest: []string{"ready", "dungeon/done"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotNotes, gotRest := splitNotesStatus(tt.in)
			if gotNotes != tt.wantNotes || !slices.Equal(gotRest, tt.wantRest) {
				t.Fatalf("splitNotesStatus(%v) = (%v, %v), want (%v, %v)", tt.in, gotNotes, gotRest, tt.wantNotes, tt.wantRest)
			}
		})
	}
}

func TestOutputIntentListPayloadNoteItems(t *testing.T) {
	created := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	root := noteEntryFixture("root-note", "", created)
	root.Note.Tags = []string{"ops"}
	nested := noteEntryFixture("paper-note", "reading/papers", created.Add(time.Hour))
	nested.Note.Status = intentcore.Status("notes/reading/papers")
	nested.Note.UpdatedAt = created.Add(2 * time.Hour)
	idea := &intentcore.Intent{
		ID:        "idea-1",
		Title:     "an idea",
		Type:      intentcore.TypeIdea,
		Status:    intentcore.StatusInbox,
		CreatedAt: created,
		Path:      filepath.Join(notesListTestRoot, ".campaign", "intents", "inbox", "idea-1.md"),
	}

	notes, folders := splitNoteEntries([]intentcore.NoteEntry{nested, root})
	var buf bytes.Buffer
	if err := outputIntentListPayload(&buf, notesListTestRoot, append(notes, idea), folders); err != nil {
		t.Fatalf("outputIntentListPayload: %v", err)
	}

	payload := decodeIntentJSONPayload(t, buf.String())
	if got := payload["schema_version"]; got != IntentJSONVersion {
		t.Fatalf("schema_version = %v, want %q", got, IntentJSONVersion)
	}
	if got := payload["campaign_root"]; got != notesListTestRoot {
		t.Fatalf("campaign_root = %v, want %q", got, notesListTestRoot)
	}
	items := payload["items"].([]any)
	if len(items) != 3 {
		t.Fatalf("items = %d, want 3:\n%s", len(items), buf.String())
	}

	paper := items[0].(map[string]any)
	wantPaper := map[string]any{
		"id":         "paper-note",
		"title":      "title paper-note",
		"status":     "notes",
		"folder":     "reading/papers",
		"author":     "agent",
		"created_at": "2026-03-01T10:00:00Z",
		"updated_at": "2026-03-01T11:00:00Z",
		"path":       ".campaign/intents/notes/reading/papers/paper-note.md",
	}
	for key, want := range wantPaper {
		if got := paper[key]; got != want {
			t.Errorf("paper item %s = %v, want %v", key, got, want)
		}
	}

	rootItem := items[1].(map[string]any)
	folder, ok := rootItem["folder"]
	if !ok || folder != "" {
		t.Errorf("root note folder = %v (present %v), want empty string present", folder, ok)
	}
	if _, ok := rootItem["updated_at"]; ok {
		t.Errorf("root note without updated_at should omit the key: %#v", rootItem)
	}
	if tags, _ := rootItem["tags"].([]any); len(tags) != 1 || tags[0] != "ops" {
		t.Errorf("root note tags = %#v, want [ops]", rootItem["tags"])
	}

	ideaItem := items[2].(map[string]any)
	if _, ok := ideaItem["folder"]; ok {
		t.Errorf("lifecycle idea must not carry folder: %#v", ideaItem)
	}
	if got := ideaItem["status"]; got != "inbox" {
		t.Errorf("lifecycle idea status = %v, want inbox", got)
	}
}

func TestOutputIntentListPayloadEmptyNotesIsArray(t *testing.T) {
	notes, folders := splitNoteEntries(nil)
	var buf bytes.Buffer
	if err := outputIntentListPayload(&buf, notesListTestRoot, notes, folders); err != nil {
		t.Fatalf("outputIntentListPayload: %v", err)
	}
	items, ok := decodeIntentJSONPayload(t, buf.String())["items"].([]any)
	if !ok || len(items) != 0 {
		t.Fatalf("items = %#v, want []", items)
	}
}

func TestIntentNoteJSONRequiresText(t *testing.T) {
	_, stderr, err := executeIntentJSONTestCommand(t, newIntentNoteCommand(), "--json")
	if err == nil {
		t.Fatal("note --json without text should fail")
	}
	payload := decodeIntentJSONPayload(t, stderr)
	if got := payload["schema_version"]; got != IntentJSONVersion {
		t.Fatalf("error schema_version = %v, want %q", got, IntentJSONVersion)
	}
	envelope, ok := payload["error"].(map[string]any)
	if !ok {
		t.Fatalf("missing error envelope: %#v", payload)
	}
	if msg, _ := envelope["message"].(string); msg == "" {
		t.Fatalf("error envelope has no message: %#v", envelope)
	}
}
