package dungeon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	wkaudit "github.com/Obedience-Corp/camp/internal/workitem/audit"
)

func writeWorkitemMarker(t *testing.T, dir, id, ref, wiType, title string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := "version: v1alpha6\nkind: workitem\nid: " + id + "\ntype: " + wiType + "\ntitle: " + title + "\nref: " + ref + "\n"
	if err := os.WriteFile(filepath.Join(dir, ".workitem"), []byte(marker), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readWorkitemLedgerEvents(t *testing.T, campaignRoot string) []wkaudit.Event {
	t.Helper()
	path := filepath.Join(campaignRoot, ".campaign", "workitems", wkaudit.AuditFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read ledger: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	events := make([]wkaudit.Event, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		var e wkaudit.Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("unmarshal ledger line %q: %v", line, err)
		}
		events = append(events, e)
	}
	return events
}

func TestRecordWorkitemMove_SkipsItemsWithoutMarker(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "workflow", "bug", "plain-note")
	dest := filepath.Join(root, "workflow", "bug", "dungeon", "archived", "plain-note")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}

	recordWorkitemMove(context.Background(), root, source, dest)

	if events := readWorkitemLedgerEvents(t, root); len(events) != 0 {
		t.Fatalf("expected no ledger event for a non-workitem move, got %+v", events)
	}
	if _, ok := workitemLedgerPathIfExists(root); ok {
		t.Fatal("ledger file must not be created when nothing is appended")
	}
}

func TestRecordWorkitemMove_AppendsMoveEventForTrackedWorkitem(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "workflow", "bug", "flaky-test")
	dest := filepath.Join(root, "workflow", "bug", "dungeon", "archived", "2026-07-15", "flaky-test")
	writeWorkitemMarker(t, dest, "bug-flaky-test-2026-07-10", "WI-abc123", "bug", "Flaky test")

	recordWorkitemMove(context.Background(), root, source, dest)

	events := readWorkitemLedgerEvents(t, root)
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 ledger event, got %d: %+v", len(events), events)
	}
	got := events[0]
	if got.Event != wkaudit.EventMove {
		t.Fatalf("Event = %q, want %q", got.Event, wkaudit.EventMove)
	}
	if got.ID != "bug-flaky-test-2026-07-10" || got.Ref != "WI-abc123" || got.Type != "bug" || got.Title != "Flaky test" {
		t.Fatalf("unexpected event identity: %+v", got)
	}
	if got.From != "workflow/bug/flaky-test" {
		t.Fatalf("From = %q, want workflow/bug/flaky-test", got.From)
	}
	if got.To != "workflow/bug/dungeon/archived/2026-07-15/flaky-test" {
		t.Fatalf("To = %q, want workflow/bug/dungeon/archived/2026-07-15/flaky-test", got.To)
	}

	if path, ok := workitemLedgerPathIfExists(root); !ok || path == "" {
		t.Fatal("ledger file should exist and be reported stageable after a workitem move")
	}
}

func TestWorkitemLedgerPathIfExists_FalseWhenAbsent(t *testing.T) {
	root := t.TempDir()
	if _, ok := workitemLedgerPathIfExists(root); ok {
		t.Fatal("expected false for a campaign root with no ledger file yet")
	}
}
