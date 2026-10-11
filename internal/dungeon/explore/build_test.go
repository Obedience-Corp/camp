package explore

import "testing"

func TestApplySinceUntil(t *testing.T) {
	items := []Item{
		{DoneDate: "2026-10-01", Title: "a", DungeonPath: "festivals/.dungeon", DungeonLabel: "Festivals", Status: "completed"},
		{DoneDate: "2026-10-05", Title: "b", DungeonPath: "festivals/.dungeon", DungeonLabel: "Festivals", Status: "completed"},
	}
	got, err := Apply(Index{Items: items}, Query{Since: "2026-10-05", Until: "2026-10-05"})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].Title != "b" {
		t.Fatalf("items = %+v", got.Items)
	}
	if _, err := Apply(Index{Items: items}, Query{Since: "2026-10-06", Until: "2026-10-01"}); err == nil {
		t.Fatal("since after until error = nil")
	}
}
