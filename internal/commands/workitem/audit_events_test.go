package workitem

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	wkaudit "github.com/Obedience-Corp/camp/internal/workitem/audit"
)

// readAuditEvents reads every event appended to the campaign-wide workitem
// ledger, in append order.
func readAuditEvents(t *testing.T, root string) []wkaudit.Event {
	t.Helper()
	path := filepath.Join(root, ".campaign", "workitems", wkaudit.AuditFile)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit ledger: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	events := make([]wkaudit.Event, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		var e wkaudit.Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("unmarshal audit event %q: %v", line, err)
		}
		events = append(events, e)
	}
	return events
}

func TestRunCreate_AppendsCreateAuditEvent(t *testing.T) {
	root := refQuestTestCampaign(t)
	restore := chdir(t, root)
	defer restore()
	t.Setenv("CAMP_QUEST", "")

	cmd := &cobra.Command{}
	cmd.SetOut(os.Stdout)
	cmd.SetErr(os.Stderr)

	if err := runCreate(context.Background(), cmd, "atomic-marker", "design", "Atomic Marker", "design-atomic-marker-fixed", "", "", false); err != nil {
		t.Fatalf("runCreate() error = %v", err)
	}

	events := readAuditEvents(t, root)
	if len(events) != 1 {
		t.Fatalf("expected 1 audit event, got %d: %+v", len(events), events)
	}
	got := events[0]
	if got.Event != wkaudit.EventCreate {
		t.Fatalf("Event = %q, want %q", got.Event, wkaudit.EventCreate)
	}
	if got.ID != "design-atomic-marker-fixed" {
		t.Fatalf("ID = %q, want fixed id", got.ID)
	}
	if got.Ref == "" {
		t.Fatal("expected Ref to be populated")
	}
	if got.Type != "design" {
		t.Fatalf("Type = %q, want design", got.Type)
	}
	if got.Title != "Atomic Marker" {
		t.Fatalf("Title = %q, want Atomic Marker", got.Title)
	}
	if got.To != "workflow/design/atomic-marker" {
		t.Fatalf("To = %q, want workflow/design/atomic-marker", got.To)
	}
}

func TestRunAdopt_AppendsAdoptAuditEvent(t *testing.T) {
	root := refQuestTestCampaign(t)
	restore := chdir(t, root)
	defer restore()
	t.Setenv("CAMP_QUEST", "")

	adoptDir := filepath.Join(root, "workflow", "design", "legacy")
	if err := os.MkdirAll(adoptDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{}
	cmd.SetOut(os.Stdout)
	cmd.SetErr(os.Stderr)
	if err := runAdopt(context.Background(), cmd, "workflow/design/legacy", "design", "Legacy", "design-legacy-fixed", ""); err != nil {
		t.Fatalf("runAdopt() error = %v", err)
	}

	events := readAuditEvents(t, root)
	if len(events) != 1 {
		t.Fatalf("expected 1 audit event, got %d: %+v", len(events), events)
	}
	got := events[0]
	if got.Event != wkaudit.EventAdopt {
		t.Fatalf("Event = %q, want %q", got.Event, wkaudit.EventAdopt)
	}
	if got.ID != "design-legacy-fixed" {
		t.Fatalf("ID = %q, want fixed id", got.ID)
	}
	if got.Ref == "" {
		t.Fatal("expected Ref to be populated")
	}
	if got.Type != "design" {
		t.Fatalf("Type = %q, want design", got.Type)
	}
	if got.Title != "Legacy" {
		t.Fatalf("Title = %q, want Legacy", got.Title)
	}
	if got.To != "workflow/design/legacy" {
		t.Fatalf("To = %q, want workflow/design/legacy", got.To)
	}
}
