package intent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestImportMeeting_CreatesSidecarAndUpdatesInPlace(t *testing.T) {
	svc, ctx := newNotesTestService(t)

	bundle := filepath.Join(t.TempDir(), "boardy-spc-meeting-20260729-133259.meeting")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "summary.md"), []byte("## Summary\n\nHello meeting.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	transcript := "# Meeting: boardy spc meeting\n" +
		"# Started: 2026-07-29T13:33:00-06:00\n" +
		"# STT: sherpa (offline)\n" +
		"[00:00:01] first utterance\n" +
		"[00:13:45] final utterance\n"
	if err := os.WriteFile(filepath.Join(bundle, "meeting.md"), []byte(transcript), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(bundle, ".samantha"), 0o755); err != nil {
		t.Fatal(err)
	}
	events := "{\"type\":\"speaker_analysis\",\"status\":\"complete\",\"speaker_id\":\"spk1\"}\n" +
		"{\"type\":\"utterance\",\"speaker_id\":\"spk2\"}\n"
	if err := os.WriteFile(filepath.Join(bundle, ".samantha", "events.jsonl"), []byte(events), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := svc.ImportMeeting(ctx, ImportMeetingOptions{
		BundlePath: bundle,
		Author:     "agent",
		Timestamp:  time.Date(2026, 7, 29, 13, 32, 59, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ImportMeeting: %v", err)
	}
	if res.Note.Status != StatusNoteMeetings {
		t.Fatalf("Status = %q, want notes/meetings", res.Note.Status)
	}
	if _, err := os.Stat(res.TranscriptPath); err != nil {
		t.Fatalf("transcript missing: %v", err)
	}
	raw, _ := os.ReadFile(res.Note.Path)
	if strings.Contains(string(raw), "spk1: hi") {
		t.Fatal("transcript must not be inlined into the note body")
	}
	if !strings.Contains(string(raw), ".transcripts/") {
		t.Fatal("note body should reference transcript sidecar")
	}
	if res.Note.Meeting == nil {
		t.Fatal("imported note has no structured meeting metadata")
	}
	if got := res.Note.Meeting.DurationSeconds; got != 825 {
		t.Fatalf("duration_seconds = %d, want 825", got)
	}
	if got := res.Note.Meeting.Utterances; got != 2 {
		t.Fatalf("utterances = %d, want 2", got)
	}
	if got := res.Note.Meeting.Speakers; got != 2 {
		t.Fatalf("speakers = %d, want 2", got)
	}
	if got := res.Note.Meeting.STT; got != "sherpa (offline)" {
		t.Fatalf("stt = %q, want sherpa (offline)", got)
	}
	if got := res.Note.Meeting.SpeakerAnalysis; got != "complete" {
		t.Fatalf("speaker_analysis = %q, want complete", got)
	}
	reparsed, err := ParseIntent(raw)
	if err != nil {
		t.Fatalf("ParseIntent meeting round trip: %v", err)
	}
	if reparsed.Meeting == nil || reparsed.Meeting.DurationSeconds != 825 {
		t.Fatalf("meeting metadata did not round trip: %#v", reparsed.Meeting)
	}

	// Re-import updates in place.
	res2, err := svc.ImportMeeting(ctx, ImportMeetingOptions{
		BundlePath: bundle,
		Summary:    "## Summary\n\nUpdated summary.\n",
	})
	if err != nil {
		t.Fatalf("re-import: %v", err)
	}
	if !res2.UpdatedExisting {
		t.Fatal("expected UpdatedExisting on re-import")
	}
	if res2.Note.ID != res.Note.ID {
		t.Fatalf("id changed on re-import: %q vs %q", res2.Note.ID, res.Note.ID)
	}
	// Only one meetings note file.
	meetingsDir := filepath.Join(svc.intentsDir, "notes", "meetings")
	entries, err := os.ReadDir(meetingsDir)
	if err != nil {
		t.Fatal(err)
	}
	mdCount := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			mdCount++
		}
	}
	if mdCount != 1 {
		t.Fatalf("meetings md files = %d, want 1 (no duplicate)", mdCount)
	}
}

func TestImportMeeting_PlainBundleNameUpdatesInPlace(t *testing.T) {
	svc, ctx := newNotesTestService(t)
	bundle := filepath.Join(t.TempDir(), "standup.meeting")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "meeting.md"), []byte("# Meeting: standup\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	first, err := svc.ImportMeeting(ctx, ImportMeetingOptions{
		BundlePath: bundle,
		Summary:    "first summary",
		Timestamp:  time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("first import: %v", err)
	}
	if !intentIDPattern.MatchString(first.Note.ID) {
		t.Fatalf("plain bundle ID %q does not match intent ID format", first.Note.ID)
	}

	second, err := svc.ImportMeeting(ctx, ImportMeetingOptions{
		BundlePath: bundle,
		Summary:    "updated summary",
	})
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if !second.UpdatedExisting {
		t.Fatal("plain bundle re-import should update the existing note")
	}
	if second.Note.ID != first.Note.ID {
		t.Fatalf("plain bundle ID changed on re-import: %q vs %q", second.Note.ID, first.Note.ID)
	}
}

func TestStableMeetingIDDistinguishesBasenamesWithSameSlug(t *testing.T) {
	left := stableMeetingID("stand_up")
	right := stableMeetingID("stand-up")
	if left == right {
		t.Fatalf("distinct bundle basenames collapsed to the same ID %q", left)
	}
	if !intentIDPattern.MatchString(left) || !intentIDPattern.MatchString(right) {
		t.Fatalf("derived IDs are invalid: %q, %q", left, right)
	}
}

func TestImportMeeting_AdoptPreservesLifecycleIntentID(t *testing.T) {
	svc, ctx := newNotesTestService(t)
	existing, err := svc.CreateDirect(ctx, CreateOptions{
		Title:     "misfiled meeting",
		Type:      TypeIdea,
		Timestamp: time.Date(2026, 7, 29, 13, 32, 59, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect: %v", err)
	}
	bundle := filepath.Join(t.TempDir(), "different-bundle-name.meeting")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "meeting.md"), []byte("# Meeting: adopted\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := svc.ImportMeeting(ctx, ImportMeetingOptions{
		BundlePath:    bundle,
		Summary:       "adopted summary",
		AdoptIntentID: existing.ID,
	})
	if err != nil {
		t.Fatalf("ImportMeeting adopt: %v", err)
	}
	if result.Note.ID != existing.ID {
		t.Fatalf("adopted note ID = %q, want preserved %q", result.Note.ID, existing.ID)
	}
	if _, err := svc.Get(ctx, existing.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("adopted lifecycle intent still resolves: %v", err)
	}
	if _, err := svc.GetNote(ctx, existing.ID); err != nil {
		t.Fatalf("adopted note does not resolve by preserved ID: %v", err)
	}
}

func TestImportMeeting_RequiresMeetingMarkdown(t *testing.T) {
	svc, ctx := newNotesTestService(t)
	bundle := filepath.Join(t.TempDir(), "incomplete.meeting")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := svc.ImportMeeting(ctx, ImportMeetingOptions{BundlePath: bundle})
	if err == nil || !strings.Contains(err.Error(), "missing meeting.md") {
		t.Fatalf("ImportMeeting error = %v, want missing meeting.md", err)
	}
}

func TestExtractMeetingIntents_PersistsBacklinksAndIsIdempotent(t *testing.T) {
	svc, ctx := newNotesTestService(t)
	bundle := filepath.Join(t.TempDir(), "extract.meeting")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "meeting.md"), []byte("# Meeting: extract\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	imported, err := svc.ImportMeeting(ctx, ImportMeetingOptions{BundlePath: bundle})
	if err != nil {
		t.Fatalf("ImportMeeting: %v", err)
	}

	items := []ExtractItem{{Title: "Follow up"}, {Title: "Build follow-up", Type: TypeFeature}}
	created, err := svc.ExtractMeetingIntents(ctx, imported.Note.ID, items)
	if err != nil {
		t.Fatalf("ExtractMeetingIntents: %v", err)
	}
	if len(created) != 2 {
		t.Fatalf("created = %d, want 2", len(created))
	}
	if created[0].Type != TypeChore {
		t.Fatalf("default extracted type = %q, want chore", created[0].Type)
	}
	if !strings.Contains(created[0].Content, "meeting_ref: "+imported.Note.ID) {
		t.Fatalf("created intent missing meeting backlink:\n%s", created[0].Content)
	}

	note, err := svc.GetNote(ctx, imported.Note.ID)
	if err != nil {
		t.Fatalf("GetNote: %v", err)
	}
	if note.Meeting == nil || len(note.Meeting.ExtractedIntents) != 2 {
		t.Fatalf("extracted_intents = %#v, want 2 ids", note.Meeting)
	}
	createdAgain, err := svc.ExtractMeetingIntents(ctx, imported.Note.ID, items)
	if err != nil {
		t.Fatalf("ExtractMeetingIntents retry: %v", err)
	}
	if len(createdAgain) != 0 {
		t.Fatalf("retry created %d duplicate intents", len(createdAgain))
	}
}
