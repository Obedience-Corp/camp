package explorer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Obedience-Corp/camp/internal/intent"
)

func TestExtractItemsFromMeetingBody(t *testing.T) {
	body := `# Meeting

## Action items

- [ ] Rewrite the unfair-advantage answer
- [x] Already done item
- not a checkbox
- [ ] Rewrite the unfair-advantage answer
`
	items := extractItemsFromMeetingBody(body)
	if len(items) != 2 {
		t.Fatalf("items = %#v, want 2 unique checkbox lines", items)
	}
	if items[0].Title != "Rewrite the unfair-advantage answer" {
		t.Fatalf("first title = %q", items[0].Title)
	}
}

func TestMeetingKeys_TOpensTranscriptWhenMeetingSelected(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	intentsDir := filepath.Join(tmp, "intents")
	svc := intent.NewIntentService(tmp, intentsDir)

	bundle := filepath.Join(t.TempDir(), "extract.meeting")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "meeting.md"), []byte("# Meeting: extract\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	imported, err := svc.ImportMeeting(ctx, intent.ImportMeetingOptions{
		BundlePath: bundle,
		Summary:    "## Action items\n\n- [ ] Ship meeting keys\n",
		Timestamp:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ImportMeeting: %v", err)
	}

	m := NewModel(ctx, svc, nil, intentsDir, tmp, "id", "", nil)
	m.ready = true
	m.groups = groupExplorerItemsByStatus([]*intent.Intent{imported.Note}, nil, false, map[string]bool{
		"notes": true, "notes/meetings": true,
	}, false)
	if !selectNoteInGroups(&m, imported.Note.ID) {
		// selectNoteInGroups may not expand meetings; place manually
		for gi, g := range m.groups {
			for ii, it := range g.Intents {
				if it.ID == imported.Note.ID {
					m.cursorGroup = gi
					m.cursorItem = ii
					m.groups[gi].Expanded = true
				}
			}
		}
	}
	// Ensure Meeting metadata is present on selected intent
	sel := m.SelectedIntent()
	if sel == nil {
		t.Fatal("no selected intent")
	}
	// Reload from disk so Meeting is populated
	reloaded, err := svc.GetNote(ctx, imported.Note.ID)
	if err != nil {
		t.Fatal(err)
	}
	m.groups[m.cursorGroup].Intents[m.cursorItem] = reloaded
	if !isMeetingNote(reloaded) {
		t.Fatalf("expected meeting note, got status=%s meeting=%v", reloaded.Status, reloaded.Meeting != nil)
	}

	// t should prefer transcript over type filter
	updated, cmd := m.updateNormal(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	_ = updated
	if cmd == nil {
		t.Fatal("expected open-transcript command for meeting note")
	}
}

func TestMeetingKeys_XExtractsActionItems(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	intentsDir := filepath.Join(tmp, "intents")
	svc := intent.NewIntentService(tmp, intentsDir)

	bundle := filepath.Join(t.TempDir(), "extract-x.meeting")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "meeting.md"), []byte("# Meeting\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	imported, err := svc.ImportMeeting(ctx, intent.ImportMeetingOptions{
		BundlePath: bundle,
		Summary:    "## Action items\n\n- [ ] Do the thing\n",
		Timestamp:  time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ImportMeeting: %v", err)
	}
	note, err := svc.GetNote(ctx, imported.Note.ID)
	if err != nil {
		t.Fatal(err)
	}

	m := NewModel(ctx, svc, nil, intentsDir, tmp, "id", "", nil)
	m.ready = true
	cmd := m.runMeetingExtract(note, extractItemsFromMeetingBody(note.Content))
	msg := cmd()
	fin, ok := msg.(folderFinishedMsg)
	if !ok || fin.err != nil {
		t.Fatalf("extract result = %#v", msg)
	}
	if fin.message != "Extracted 1 intent(s) from meeting" {
		t.Fatalf("extract message = %q", fin.message)
	}

	reloaded, err := svc.GetNote(ctx, imported.Note.ID)
	if err != nil {
		t.Fatalf("GetNote after extract: %v", err)
	}
	if reloaded.Meeting == nil || len(reloaded.Meeting.ExtractedIntents) != 1 {
		t.Fatalf("extracted intent ids = %#v, want one", reloaded.Meeting)
	}
	extracted, err := svc.Get(ctx, reloaded.Meeting.ExtractedIntents[0])
	if err != nil {
		t.Fatalf("Get extracted intent: %v", err)
	}
	if extracted.Title != "Do the thing" || extracted.Status != intent.StatusInbox {
		t.Fatalf("extracted intent = %#v", extracted)
	}

	// Idempotent second run
	msg2 := m.runMeetingExtract(note, extractItemsFromMeetingBody(note.Content))()
	fin2, ok := msg2.(folderFinishedMsg)
	if !ok || fin2.err != nil {
		t.Fatalf("second extract = %#v", msg2)
	}
	if !strings.HasPrefix(fin2.message, "Extracted 0 new intents") {
		t.Fatalf("second extract message = %q", fin2.message)
	}
	reloadedAgain, err := svc.GetNote(ctx, imported.Note.ID)
	if err != nil {
		t.Fatalf("GetNote after second extract: %v", err)
	}
	if got := len(reloadedAgain.Meeting.ExtractedIntents); got != 1 {
		t.Fatalf("extracted intent ids after retry = %d, want 1", got)
	}
}

func TestMeetingKeys_AMissingAudioFailsClosed(t *testing.T) {
	m := Model{}
	selected := &intent.Intent{Meeting: &intent.MeetingMetadata{
		Bundle: "/path/that/does/not/exist",
		Audio:  "audio.wav",
	}}

	_, cmd := m.handleMeetingAudio(selected)
	if cmd != nil {
		t.Fatal("missing audio returned an open command")
	}
	if m.statusMessage != "Meeting audio is unavailable: audio.wav" {
		t.Fatalf("status message = %q", m.statusMessage)
	}
}

func TestMeetingKeys_ARejectsAudioFromAnotherHost(t *testing.T) {
	m := Model{}
	selected := &intent.Intent{Meeting: &intent.MeetingMetadata{
		Bundle:     "/tmp/meeting-bundle",
		BundleHost: "definitely-not-this-host.invalid",
		Audio:      "audio.wav",
	}}

	_, cmd := m.handleMeetingAudio(selected)
	if cmd != nil {
		t.Fatal("remote-host audio returned an open command")
	}
	if m.statusMessage != "Meeting audio is available on definitely-not-this-host.invalid" {
		t.Fatalf("status message = %q", m.statusMessage)
	}
}
