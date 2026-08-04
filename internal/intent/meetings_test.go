package intent

import (
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
	if err := os.WriteFile(filepath.Join(bundle, "transcript.md"), []byte("spk1: hi\nspk2: hello\n"), 0o644); err != nil {
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
