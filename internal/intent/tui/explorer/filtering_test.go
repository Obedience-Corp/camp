package explorer

import (
	"context"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/intent"
)

func TestStatusSelectionToStatus(t *testing.T) {
	tests := []struct {
		name      string
		selection string
		want      intent.Status
		wantOK    bool
	}{
		{name: "inbox", selection: "Inbox", want: intent.StatusInbox, wantOK: true},
		{name: "notes", selection: "Notes", want: intent.StatusNote, wantOK: true},
		{name: "ready", selection: "Ready", want: intent.StatusReady, wantOK: true},
		{name: "active", selection: "Active", want: intent.StatusActive, wantOK: true},
		{name: "done label", selection: "Done", want: intent.StatusDone, wantOK: true},
		{name: "killed label", selection: "Killed", want: intent.StatusKilled, wantOK: true},
		{name: "done canonical", selection: "dungeon/done", want: intent.StatusDone, wantOK: true},
		{name: "killed canonical", selection: "dungeon/killed", want: intent.StatusKilled, wantOK: true},
		{name: "unknown", selection: "wat", want: "", wantOK: false},
	}

	for _, tt := range tests {
		got, ok := statusSelectionToStatus(tt.selection)
		if ok != tt.wantOK {
			t.Fatalf("%s: ok = %v, want %v", tt.name, ok, tt.wantOK)
		}
		if got != tt.want {
			t.Fatalf("%s: statusSelectionToStatus(%q) = %q, want %q", tt.name, tt.selection, got, tt.want)
		}
	}
}

func TestApplyFilters_StatusDoneAndKilledUseDungeonStatuses(t *testing.T) {
	ctx := context.Background()
	m := NewModel(ctx, nil, nil, "/tmp/intents", "/tmp/campaign", "test-id", "", nil)
	m.ready = true

	now := time.Now()
	m.intents = []*intent.Intent{
		{ID: "inbox-1", Title: "Inbox", Status: intent.StatusInbox, Type: intent.TypeFeature, CreatedAt: now},
		{ID: "done-1", Title: "Done", Status: intent.StatusDone, Type: intent.TypeFeature, CreatedAt: now},
		{ID: "killed-1", Title: "Killed", Status: intent.StatusKilled, Type: intent.TypeFeature, CreatedAt: now},
	}
	m.filteredIntents = m.intents
	m.dungeonExpanded = true
	m.groups = groupIntentsByStatus(m.intents, m.dungeonExpanded)

	statusChip := m.filterBar.ChipByLabel("Status")
	if statusChip == nil {
		t.Fatal("missing Status filter chip")
	}
	statusChip.SetSelected(statusFilterIndex(t, "Done"))
	m.applyFilters()

	if len(m.filteredIntents) != 1 || m.filteredIntents[0].Status != intent.StatusDone {
		t.Fatalf("Done filter returned %+v, want one dungeon/done intent", m.filteredIntents)
	}

	statusChip.SetSelected(statusFilterIndex(t, "Killed"))
	m.applyFilters()

	if len(m.filteredIntents) != 1 || m.filteredIntents[0].Status != intent.StatusKilled {
		t.Fatalf("Killed filter returned %+v, want one dungeon/killed intent", m.filteredIntents)
	}
}

func TestApplyFilters_StatusNotes(t *testing.T) {
	ctx := context.Background()
	m := NewModel(ctx, nil, nil, "/tmp/intents", "/tmp/campaign", "test-id", "", nil)
	m.ready = true

	now := time.Now()
	m.intents = []*intent.Intent{
		{ID: "note-1", Title: "Note", Status: intent.StatusNote, CreatedAt: now},
		{ID: "inbox-1", Title: "Inbox", Status: intent.StatusInbox, Type: intent.TypeFeature, CreatedAt: now},
	}
	m.filteredIntents = m.intents
	m.groups = groupIntentsByStatus(m.intents, m.dungeonExpanded)

	statusChip := m.filterBar.ChipByLabel("Status")
	if statusChip == nil {
		t.Fatal("missing Status filter chip")
	}
	statusChip.SetSelected(statusFilterIndex(t, "Notes"))
	m.applyFilters()

	if len(m.filteredIntents) != 1 || m.filteredIntents[0].Status != intent.StatusNote {
		t.Fatalf("Notes filter returned %+v, want one note", m.filteredIntents)
	}
}

func TestApplyFilters_UsesCachedSearchCorpus(t *testing.T) {
	ctx := context.Background()
	m := NewModel(ctx, nil, nil, "/tmp/intents", "/tmp/campaign", "test-id", "", nil)
	m.ready = true

	now := time.Now()
	m.intents = []*intent.Intent{
		{ID: "alpha", Title: "Alpha", Content: "cached-match", Status: intent.StatusInbox, Type: intent.TypeFeature, CreatedAt: now},
		{ID: "beta", Title: "Beta", Content: "other", Status: intent.StatusInbox, Type: intent.TypeFeature, CreatedAt: now},
	}
	m.rebuildSearchCorpus()
	m.filteredIntents = m.intents
	m.groups = groupIntentsByStatus(m.intents, m.dungeonExpanded)

	m.searchInput.SetValue("cached-match")
	m.applyFilters()

	if len(m.filteredIntents) != 1 || m.filteredIntents[0].ID != "alpha" {
		t.Fatalf("cached search returned %+v, want alpha", m.filteredIntents)
	}

	m.searchCorpus[0] = "stale-only-match"
	m.intents[0].Content = "content-changed-without-rebuild"
	m.searchInput.SetValue("stale-only-match")
	m.applyFilters()

	if len(m.filteredIntents) != 1 || m.filteredIntents[0].ID != "alpha" {
		t.Fatalf("search did not use cached corpus; got %+v, want alpha from cache", m.filteredIntents)
	}
}

func statusFilterIndex(t *testing.T, label string) int {
	t.Helper()
	for i, item := range statusFilterItems {
		if item == label {
			return i
		}
	}
	t.Fatalf("status filter item %q not found in %v", label, statusFilterItems)
	return -1
}
