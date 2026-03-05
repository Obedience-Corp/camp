package main

import (
	"testing"

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
