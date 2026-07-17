package intent

import (
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/intent"
)

func TestFilterStatuses_DefaultExcludesDungeon(t *testing.T) {
	intents := []*intent.Intent{
		{ID: "1", Status: intent.StatusInbox},
		{ID: "2", Status: intent.StatusReady},
		{ID: "3", Status: intent.StatusActive},
		{ID: "4", Status: intent.StatusDone},
		{ID: "5", Status: intent.StatusKilled},
		{ID: "6", Status: intent.StatusArchived},
		{ID: "7", Status: intent.StatusSomeday},
	}

	got := filterStatuses(intents, false, nil)
	if len(got) != 3 {
		t.Fatalf("filterStatuses() returned %d intents, want 3", len(got))
	}
	for _, i := range got {
		if i.Status.InDungeon() {
			t.Fatalf("filterStatuses() included dungeon status %q", i.Status)
		}
	}
}

func TestFilterStatuses_AcceptsShortAndCanonicalStatusFilters(t *testing.T) {
	intents := []*intent.Intent{
		{ID: "a", Status: intent.StatusArchived},
		{ID: "b", Status: intent.StatusReady},
	}

	shortFiltered := filterStatuses(intents, false, []string{"archived"})
	if len(shortFiltered) != 1 || shortFiltered[0].ID != "a" {
		t.Fatalf("short status filter mismatch: %#v", shortFiltered)
	}

	canonicalFiltered := filterStatuses(intents, false, []string{"dungeon/archived"})
	if len(canonicalFiltered) != 1 || canonicalFiltered[0].ID != "a" {
		t.Fatalf("canonical status filter mismatch: %#v", canonicalFiltered)
	}
}

func TestFilterStale(t *testing.T) {
	now := time.Now()
	old := now.Add(-10 * 24 * time.Hour)
	recent := now.Add(-1 * 24 * time.Hour)

	intents := []*intent.Intent{
		{ID: "unclaimed-old", UpdatedAt: old},
		{ID: "claimed-stale", AssignedTo: "session-1", UpdatedAt: old},
		{ID: "claimed-fresh", AssignedTo: "session-1", UpdatedAt: recent},
		{ID: "claimed-no-updated-falls-back-to-created", AssignedTo: "session-1", CreatedAt: old},
	}

	t.Run("staleOnly false returns intents unchanged", func(t *testing.T) {
		got := filterStale(intents, false, 7)
		if len(got) != len(intents) {
			t.Fatalf("filterStale(staleOnly=false) returned %d, want %d (unfiltered)", len(got), len(intents))
		}
	})

	t.Run("only claimed intents past the threshold survive", func(t *testing.T) {
		got := filterStale(intents, true, 7)
		want := map[string]bool{"claimed-stale": true, "claimed-no-updated-falls-back-to-created": true}
		if len(got) != len(want) {
			t.Fatalf("filterStale(stale, 7 days) = %d intents, want %d", len(got), len(want))
		}
		for _, i := range got {
			if !want[i.ID] {
				t.Errorf("filterStale(stale, 7 days) unexpectedly included %q", i.ID)
			}
		}
	})

	t.Run("non-positive days falls back to the default threshold", func(t *testing.T) {
		defaultDays := filterStale(intents, true, staleDefaultDays)
		zeroDays := filterStale(intents, true, 0)
		if len(zeroDays) != len(defaultDays) {
			t.Fatalf("filterStale(stale, 0) = %d, want same as default threshold (%d)", len(zeroDays), len(defaultDays))
		}
	})

	t.Run("tighter threshold excludes recently touched claims", func(t *testing.T) {
		got := filterStale(intents, true, 30)
		for _, i := range got {
			if i.ID == "claimed-fresh" {
				t.Fatal("filterStale(stale, 30 days) should not surface a claim updated 1 day ago")
			}
		}
	})
}
