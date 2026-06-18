package audit

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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
