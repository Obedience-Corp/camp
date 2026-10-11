package explore

import (
	"slices"
	"testing"
)

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

func TestStatusDirectoryNamedLikeALens(t *testing.T) {
	items := []Item{
		{DoneDate: "2026-10-01", Title: "shipped", DungeonPath: ".dungeon", DungeonLabel: "Camp", Status: "completed"},
		{DoneDate: "2026-10-02", Title: "in finished", DungeonPath: ".dungeon", DungeonLabel: "Camp", Status: "finished"},
		{DoneDate: "2026-10-03", Title: "in all", DungeonPath: ".dungeon", DungeonLabel: "Camp", Status: "all"},
		{DoneDate: "2026-10-04", Title: "reviewed", DungeonPath: ".dungeon", DungeonLabel: "Camp", Status: "Reviewed"},
	}
	idx := Index{Items: items}
	titles := func(status string) (string, []string) {
		t.Helper()
		got, err := Apply(idx, Query{Status: status})
		if err != nil {
			t.Fatalf("Apply(%q) error = %v", status, err)
		}
		var out []string
		for _, item := range got.Items {
			out = append(out, item.Title)
		}
		return got.StatusLens, out
	}
	for status, want := range map[string]struct {
		lens   string
		titles []string
	}{
		"finished":   {"finished", []string{"shipped"}},
		"finished/":  {"finished/", []string{"in finished"}},
		"Finished/":  {"finished/", []string{"in finished"}},
		"all/":       {"all/", []string{"in all"}},
		"completed/": {"completed", []string{"shipped"}},
		"reviewed/":  {"Reviewed", []string{"reviewed"}},
	} {
		lens, got := titles(status)
		if lens != want.lens || !slices.Equal(got, want.titles) {
			t.Errorf("Apply(%q) = lens %q, %v; want lens %q, %v", status, lens, got, want.lens, want.titles)
		}
	}
	if lens, got := titles(LensAll); lens != LensAll || len(got) != len(items) {
		t.Errorf("Apply(all) = lens %q, %v; want every row", lens, got)
	}
	if _, err := Apply(idx, Query{Status: "killed/"}); err == nil {
		t.Error("a directory that does not exist resolved")
	}

	// The s key walks Presets, so each such directory is one stop on it.
	presets := Presets(items)
	for _, want := range []string{"Reviewed", "all/", "finished/"} {
		if !slices.Contains(presets, want) {
			t.Errorf("Presets = %v, want %q in it", presets, want)
		}
	}
	seen := map[string]bool{}
	for lens := "finished"; !seen[lens]; lens = NextPreset(items, lens) {
		seen[lens] = true
	}
	if !seen["finished/"] || !seen["all/"] || len(seen) != len(presets) {
		t.Errorf("stepping reached %v, want every one of %v", seen, presets)
	}
	if got := DungeonLenses(items, "all/"); !slices.Equal(got, []string{LensAll, ".dungeon"}) {
		t.Errorf("DungeonLenses(all/) = %v", got)
	}
	if got := Presets(items[:1]); slices.Contains(got, "finished/") || slices.Contains(got, "all/") {
		t.Errorf("Presets without such directories = %v", got)
	}
}
