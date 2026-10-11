package explore

import "testing"

func TestApplyKeepsCustomStatusCasing(t *testing.T) {
	items := []Item{
		{DoneDate: "2026-10-01", Title: "a", DungeonPath: "festivals/.dungeon", DungeonLabel: "Festivals", Status: "Reviewed"},
		{DoneDate: "2026-10-02", Title: "b", DungeonPath: "festivals/.dungeon", DungeonLabel: "Festivals", Status: "completed"},
	}
	for _, status := range []string{"Reviewed", "reviewed", "REVIEWED"} {
		got, err := Apply(Index{Items: items}, Query{Status: status})
		if err != nil {
			t.Fatalf("Apply(%q) error = %v", status, err)
		}
		if len(got.Items) != 1 || got.Items[0].Title != "a" || got.StatusLens != "Reviewed" {
			t.Fatalf("Apply(%q) = %+v", status, got)
		}
	}
	if next := NextPreset(items, LensAll); next != "Reviewed" {
		t.Fatalf("NextPreset(all) = %q, want Reviewed", next)
	}
	if next := NextPreset(items, "Reviewed"); next != "finished" {
		t.Fatalf("NextPreset(Reviewed) = %q, want finished", next)
	}
}

func TestApplyResolvesEmptyDiscoveredDungeon(t *testing.T) {
	idx := Index{
		Dungeons: []DungeonPrint{{Path: "festivals/.dungeon", Name: ".dungeon", Label: "Festivals"}},
		Items: []Item{
			{DoneDate: "2026-10-01", Title: "a", DungeonPath: ".campaign/intents/.dungeon", DungeonLabel: "Intents", Status: "done"},
		},
	}
	for _, raw := range []string{"Festivals", "festivals", "festivals/.dungeon"} {
		got, err := Apply(idx, Query{Dungeon: raw})
		if err != nil {
			t.Fatalf("Apply(dungeon %q) error = %v", raw, err)
		}
		if len(got.Items) != 0 || got.DungeonLens != "festivals/.dungeon" || got.DungeonLabel != "Festivals" {
			t.Fatalf("Apply(dungeon %q) = %+v", raw, got)
		}
	}
	if _, err := Apply(idx, Query{Dungeon: "nowhere"}); err == nil {
		t.Fatal("unknown dungeon error = nil")
	}
}
