package selector

import (
	"testing"
	"time"
)

func TestFilterEmptyReturnsAllOrdered(t *testing.T) {
	t.Parallel()
	items := []Item{
		{ID: "bug", Label: "bug"},
		{ID: "idea", Label: "idea"},
		{ID: "research", Label: "research"},
	}
	got := Filter(items, "")
	if len(got) != 3 {
		t.Fatalf("Filter empty dropped rows: got %d", len(got))
	}
	if got[0].ID != "bug" || got[1].ID != "idea" || got[2].ID != "research" {
		t.Fatalf("unranked order = %v", ids(got))
	}
	gotWS := Filter(items, "  \t")
	if len(gotWS) != 3 {
		t.Fatal("whitespace query must behave like empty")
	}
}

func TestOrderPinUnrankedRanked(t *testing.T) {
	t.Parallel()
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	items := []Item{
		{ID: "camp", Label: "camp", Rank: t2},
		{ID: "none", Label: "(none)", Pin: PinTop},
		{ID: "zzz", Label: "zzz-archive"},
		{ID: "fest", Label: "fest", Rank: t1},
		{ID: "new", Label: "+ New Project", Pin: PinTop},
		{ID: "agent", Label: "agent-simulator"},
	}
	got := Order(items)
	want := []string{"none", "new", "agent", "zzz", "fest", "camp"}
	if have := ids(got); !equalIDs(have, want) {
		t.Fatalf("Order = %v, want %v", have, want)
	}
}

func TestOrderTiedRanksLabelAsc(t *testing.T) {
	t.Parallel()
	ts := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
	got := Order([]Item{
		{ID: "b", Label: "camp-buzz", Rank: ts},
		{ID: "a", Label: "camp-activity", Rank: ts},
	})
	if got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("tied ranks = %v", ids(got))
	}
}

func TestFilterKeepsRecencyAmongMatches(t *testing.T) {
	t.Parallel()
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	items := []Item{
		{ID: "camp", Label: "camp", Rank: recent},
		{ID: "camp-activity", Label: "camp-activity", Rank: old},
		{ID: "fest", Label: "fest", Rank: recent},
	}
	got := Filter(items, "camp")
	if len(got) != 2 {
		t.Fatalf("got %d matches: %v", len(got), ids(got))
	}
	if got[0].ID != "camp-activity" || got[1].ID != "camp" {
		t.Fatalf("filter must keep recency, not score: %v", ids(got))
	}
}

func TestFilterPinnedMustMatch(t *testing.T) {
	t.Parallel()
	items := []Item{
		{ID: "none", Label: "(none)", Pin: PinTop},
		{ID: "camp", Label: "camp"},
	}
	got := Filter(items, "camp")
	if len(got) != 1 || got[0].ID != "camp" {
		t.Fatalf("unmatched pin must disappear: %v", ids(got))
	}
	none := Filter(items, "none")
	if len(none) != 1 || none[0].ID != "none" {
		t.Fatalf("none should match (none): %v", ids(none))
	}
}

func ids(items []Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.ID
	}
	return out
}

func equalIDs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
