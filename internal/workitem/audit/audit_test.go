package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendEvent(t *testing.T) {
	root := t.TempDir()

	if err := AppendEvent(context.Background(), root, Event{
		Event:      EventPromote,
		ID:         "shiny",
		Type:       "feature",
		From:       "workflow/feature/shiny",
		To:         "festivals/planning/shiny-x",
		Target:     "festival",
		PromotedTo: "festivals/planning/shiny-x",
	}); err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, ".campaign", "workitems", AuditFile))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	var got Event
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if got.Event != EventPromote {
		t.Fatalf("Event = %q, want %q", got.Event, EventPromote)
	}
	if got.ID != "shiny" || got.Target != "festival" || got.PromotedTo != "festivals/planning/shiny-x" {
		t.Fatalf("unexpected event: %+v", got)
	}
	if got.Timestamp.IsZero() {
		t.Fatal("Timestamp should be set")
	}
}

func TestAppendEvent_NewEventTypes(t *testing.T) {
	cases := []struct {
		name  string
		event Event
	}{
		{
			name: "create",
			event: Event{
				Event: EventCreate,
				ID:    "design-example-2026-07-11",
				Ref:   "WI-abc123",
				Type:  "design",
				Title: "Example",
				To:    "workflow/design/example",
			},
		},
		{
			name: "adopt",
			event: Event{
				Event: EventAdopt,
				ID:    "design-legacy-2026-07-11",
				Ref:   "WI-def456",
				Type:  "design",
				Title: "Legacy",
				To:    "workflow/design/legacy",
			},
		},
		{
			name: "move",
			event: Event{
				Event: EventMove,
				ID:    "bug-triage-2026-07-11",
				Ref:   "WI-a1b2c3",
				Type:  "bug",
				Title: "Triage item",
				From:  "workflow/bug/triage-item",
				To:    "workflow/bug/dungeon/archived/2026-07-11/triage-item",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			if err := AppendEvent(context.Background(), root, c.event); err != nil {
				t.Fatalf("AppendEvent() error = %v", err)
			}

			data, err := os.ReadFile(filepath.Join(root, ".campaign", "workitems", AuditFile))
			if err != nil {
				t.Fatalf("ReadFile() error = %v", err)
			}
			var got Event
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}
			if got.Event != c.event.Event {
				t.Fatalf("Event = %q, want %q", got.Event, c.event.Event)
			}
			if got.ID != c.event.ID || got.Ref != c.event.Ref || got.Type != c.event.Type {
				t.Fatalf("unexpected id/ref/type: %+v", got)
			}
			if got.Title != c.event.Title || got.From != c.event.From || got.To != c.event.To {
				t.Fatalf("unexpected title/from/to: %+v", got)
			}
			if got.Timestamp.IsZero() {
				t.Fatal("Timestamp should be set")
			}
		})
	}
}

// TestAppendEvent_OldFormatStillDecodes locks in that events written before
// the ref/title fields existed (promote/gather shape) still decode cleanly
// with the current Event struct, and that new events with the added fields
// remain parseable line-by-line alongside them in the same ledger file.
func TestAppendEvent_OldFormatStillDecodes(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".campaign", "workitems")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	oldFormatLine := `{"ts":"2026-07-09T00:04:15Z","event":"promote","id":"feature-shiny-20260709","type":"feature","from":"workflow/feature/shiny","to":"festivals/planning/shiny-x","target":"festival","promoted_to":"festivals/planning/shiny-x"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, AuditFile), []byte(oldFormatLine), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := AppendEvent(context.Background(), root, Event{
		Event: EventCreate,
		ID:    "design-new-20260711",
		Ref:   "WI-fedcba",
		Type:  "design",
		Title: "New",
		To:    "workflow/design/new",
	}); err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, AuditFile))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines in ledger, got %d: %q", len(lines), string(data))
	}

	var old Event
	if err := json.Unmarshal([]byte(lines[0]), &old); err != nil {
		t.Fatalf("Unmarshal(old) error = %v", err)
	}
	if old.Event != EventPromote || old.ID != "feature-shiny-20260709" || old.Target != "festival" {
		t.Fatalf("old event decoded unexpectedly: %+v", old)
	}
	if old.Ref != "" || old.Title != "" {
		t.Fatalf("old event should decode with zero-value new fields, got Ref=%q Title=%q", old.Ref, old.Title)
	}

	var newEvt Event
	if err := json.Unmarshal([]byte(lines[1]), &newEvt); err != nil {
		t.Fatalf("Unmarshal(new) error = %v", err)
	}
	if newEvt.Event != EventCreate || newEvt.Ref != "WI-fedcba" || newEvt.Title != "New" {
		t.Fatalf("new event decoded unexpectedly: %+v", newEvt)
	}
}

// TestAppendBestEffort_WritesEventOnSuccess locks in that AppendBestEffort is
// the same construction-and-append path as AppendEvent when the write
// succeeds: every FS-mutating workitem command routes through this one
// function instead of re-implementing the ledger append.
func TestAppendBestEffort_WritesEventOnSuccess(t *testing.T) {
	root := t.TempDir()
	var warnings bytes.Buffer

	AppendBestEffort(context.Background(), &warnings, root, Event{
		Event: EventMove,
		ID:    "design-example-2026-07-11",
		Ref:   "WI-abc123",
		Type:  "design",
		From:  "workflow/design/example",
		To:    "workflow/design/dungeon/archived/2026-07-11/example",
	})

	if warnings.Len() != 0 {
		t.Fatalf("expected no warning on success, got %q", warnings.String())
	}

	data, err := os.ReadFile(filepath.Join(root, ".campaign", "workitems", AuditFile))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var got Event
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if got.Event != EventMove || got.ID != "design-example-2026-07-11" || got.Ref != "WI-abc123" {
		t.Fatalf("unexpected event: %+v", got)
	}
}

// TestAppendBestEffort_WarnsWithoutFailing locks in the mandatory best-effort
// contract: a ledger write failure must never abort the caller's already
// applied filesystem mutation, only warn. Forcing os.MkdirAll to fail by
// occupying the ledger directory's path with a file simulates a real write
// failure without needing root or a read-only filesystem.
func TestAppendBestEffort_WarnsWithoutFailing(t *testing.T) {
	root := t.TempDir()
	campaignDir := filepath.Join(root, ".campaign")
	if err := os.MkdirAll(campaignDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A file where the "workitems" directory needs to be created makes
	// MkdirAll fail with ENOTDIR.
	if err := os.WriteFile(filepath.Join(campaignDir, "workitems"), []byte("occupied"), 0o644); err != nil {
		t.Fatal(err)
	}

	var warnings bytes.Buffer
	AppendBestEffort(context.Background(), &warnings, root, Event{
		Event: EventMove,
		ID:    "design-blocked-2026-07-11",
		From:  "workflow/design/blocked",
		To:    "workflow/design/dungeon/archived/blocked",
	})

	if warnings.Len() == 0 {
		t.Fatal("expected a warning to be written when the ledger append fails")
	}
	if !strings.Contains(warnings.String(), "failed to append workitem audit event") {
		t.Fatalf("warning = %q, want it to describe the ledger append failure", warnings.String())
	}
}
