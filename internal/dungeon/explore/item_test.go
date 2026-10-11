package explore

import (
	"math/rand/v2"
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

// A shell completes a directory with a trailing slash and may open a path
// with ./; both name the dungeon the stored path does.
func TestApplyAcceptsADungeonPathAsAShellWritesIt(t *testing.T) {
	idx := Index{
		Dungeons: []DungeonPrint{
			{Path: "festivals/.dungeon", Name: ".dungeon", Label: "Festivals"},
			{Path: "workflow/pitch/.dungeon", Name: ".dungeon", Label: "workflow/pitch"},
			{Path: ".dungeon", Name: ".dungeon", Label: "Camp"},
		},
		Items: []Item{
			{DoneDate: "2026-10-01", Title: "a", DungeonPath: "festivals/.dungeon", DungeonLabel: "Festivals", Status: "completed"},
			{DoneDate: "2026-10-02", Title: "b", DungeonPath: "workflow/pitch/.dungeon", DungeonLabel: "workflow/pitch", Status: "completed"},
			{DoneDate: "2026-10-03", Title: "c", DungeonPath: ".dungeon", DungeonLabel: "Camp", Status: "completed"},
		},
	}
	for raw, want := range map[string]string{
		"festivals/.dungeon":       "festivals/.dungeon",
		"festivals/.dungeon/":      "festivals/.dungeon",
		"./festivals/.dungeon":     "festivals/.dungeon",
		"./festivals//.dungeon/":   "festivals/.dungeon",
		" festivals/.dungeon/ ":    "festivals/.dungeon",
		"workflow/pitch/.dungeon/": "workflow/pitch/.dungeon",
		"workflow/pitch/":          "workflow/pitch/.dungeon",
		"./workflow/pitch":         "workflow/pitch/.dungeon",
		"./.dungeon/":              ".dungeon",
		"Festivals":                "festivals/.dungeon",
	} {
		got, err := Apply(idx, Query{Dungeon: raw})
		if err != nil {
			t.Errorf("Apply(dungeon %q) error = %v", raw, err)
			continue
		}
		if got.DungeonLens != want || len(got.Items) != 1 || got.Items[0].DungeonPath != want {
			t.Errorf("Apply(dungeon %q) = lens %q, %+v; want the one row in %s", raw, got.DungeonLens, got.Items, want)
		}
	}
	for _, raw := range []string{"./", "..", "festival/", "festivals/.dungeon/completed", "/festivals/.dungeon"} {
		if got, err := Apply(idx, Query{Dungeon: raw}); err == nil || err.Error() != errUnknownDungeon(raw).Error() {
			t.Errorf("Apply(dungeon %q) = %+v, %v; want it refused as written", raw, got, err)
		}
	}
}

// A directory spelled exactly as asked wins over a lens that matches only
// when case is ignored.
func TestApplyPrefersTheStatusDirectorySpelledAsAsked(t *testing.T) {
	items := []Item{
		{DoneDate: "2026-10-01", Title: "lower", DungeonPath: ".dungeon", DungeonLabel: "Camp", Status: "completed"},
		{DoneDate: "2026-10-02", Title: "upper", DungeonPath: ".dungeon", DungeonLabel: "Camp", Status: "Completed"},
		{DoneDate: "2026-10-03", Title: "in Finished", DungeonPath: ".dungeon", DungeonLabel: "Camp", Status: "Finished"},
	}
	for status, want := range map[string]struct {
		lens  string
		title string
	}{
		"completed":  {"completed", "lower"},
		"Completed":  {"Completed", "upper"},
		"COMPLETED":  {"completed", "lower"},
		"Completed/": {"Completed", "upper"},
		"completed/": {"completed", "lower"},
		"Finished":   {"Finished", "in Finished"},
		"finished":   {"finished", "lower"},
	} {
		got, err := Apply(Index{Items: items}, Query{Status: status})
		if err != nil {
			t.Errorf("Apply(%q) error = %v", status, err)
			continue
		}
		if got.StatusLens != want.lens || len(got.Items) != 1 || got.Items[0].Title != want.title {
			t.Errorf("Apply(%q) = lens %q, %+v; want lens %q and the row %q", status, got.StatusLens, got.Items, want.lens, want.title)
		}
	}
	if next := NextPreset(items, "Completed"); next != "Finished" {
		t.Errorf("NextPreset(Completed) = %q, want the next directory", next)
	}
}

// A dungeon set directly inside another carries the outer one's path as its
// label. A path names one dungeon, whatever label reads the same.
func TestApplyTakesAStoredDungeonPathBeforeAnyLabel(t *testing.T) {
	idx := Index{
		Dungeons: []DungeonPrint{
			{Path: ".dungeon", Name: ".dungeon", Label: "Camp"},
			{Path: ".dungeon/.dungeon", Name: ".dungeon", Label: ".dungeon"},
			{Path: " lead/dungeon", Name: "dungeon", Label: " lead"},
		},
		Items: []Item{
			{DoneDate: "2026-10-03", Title: "outer", Status: "completed", DungeonPath: ".dungeon", DungeonLabel: "Camp"},
			{DoneDate: "2026-10-02", Title: "inner", Status: "completed", DungeonPath: ".dungeon/.dungeon", DungeonLabel: ".dungeon"},
			{DoneDate: "2026-10-01", Title: "spaced", Status: "completed", DungeonPath: " lead/dungeon", DungeonLabel: " lead"},
		},
	}
	for raw, want := range map[string]string{
		".dungeon":          "outer",
		"./.dungeon/":       "outer",
		"Camp":              "outer",
		".dungeon/.dungeon": "inner",
		" lead/dungeon":     "spaced",
	} {
		got, err := Apply(idx, Query{Dungeon: raw})
		if err != nil {
			t.Fatalf("Apply(dungeon %q) error = %v", raw, err)
		}
		if len(got.Items) != 1 || got.Items[0].Title != want {
			t.Fatalf("Apply(dungeon %q) = lens %q, rows %+v; want only %q", raw, got.DungeonLens, got.Items, want)
		}
	}
	// A label two dungeons share is still ambiguous, and says so.
	idx.Dungeons = append(idx.Dungeons, DungeonPrint{Path: "dungeon", Name: "dungeon", Label: "Camp"})
	if _, err := Apply(idx, Query{Dungeon: "camp"}); err == nil || err.Error() != errAmbiguousDungeon("camp").Error() {
		t.Fatalf("Apply(dungeon camp) error = %v, want the ambiguity", err)
	}
}

// The feed steps through the lenses Presets and DungeonLenses offer. Each
// must resolve, to itself, and show only its own rows, in any camp: dungeons
// nested in dungeons, parents named like labels or lenses, names with spaces.
func TestEveryLensTheFeedOffersResolvesToItsOwnRows(t *testing.T) {
	parents := []string{".", "festivals", "workflow/design", ".dungeon", "dungeon", " lead", "trail ", "all", "ALL", "Camp", "camp", "festivals/.dungeon/archived/x", "explore", "workflow/explore", "a b", "Festivals"}
	statuses := []string{"completed", "done", "archived", "holding", "all", "finished", "Finished", "ALL", "Done", "wip", "all ", " x"}
	// A fixed seed: the same 3000 camps on every run.
	r := rand.New(rand.NewPCG(740, 4))
	for range 3000 {
		var idx Index
		seen := map[string]bool{}
		for range r.IntN(4) + 1 {
			parent := parents[r.IntN(len(parents))]
			name := []string{"dungeon", ".dungeon"}[r.IntN(2)]
			path := parent + "/" + name
			if parent == "." {
				path = name
			}
			if seen[path] {
				continue
			}
			seen[path] = true
			idx.Dungeons = append(idx.Dungeons, DungeonPrint{Path: path, Name: name, Label: CleanText(Label(parent))})
		}
		for range r.IntN(4) + 1 {
			status := statuses[r.IntN(len(statuses))]
			for _, d := range idx.Dungeons {
				idx.Items = append(idx.Items, Item{DoneDate: "2026-01-01", Status: status, DungeonPath: d.Path, DungeonLabel: d.Label, Title: "t", Path: d.Path + "/" + status + "/x"})
			}
		}
		for _, preset := range Presets(idx.Items) {
			got, err := Apply(idx, Query{Status: preset})
			if err != nil || got.StatusLens != preset {
				t.Fatalf("status lens %q in %+v: resolved to %q, error %v", preset, idx.Dungeons, got.StatusLens, err)
			}
			for _, lens := range DungeonLenses(idx.Items, preset) {
				got, err := Apply(idx, Query{Status: preset, Dungeon: lens})
				if err != nil || got.DungeonLens != lens {
					t.Fatalf("dungeon lens %q in %+v: resolved to %q, error %v", lens, idx.Dungeons, got.DungeonLens, err)
				}
				for _, item := range got.Items {
					if lens != LensAll && item.DungeonPath != lens {
						t.Fatalf("dungeon lens %q in %+v shows a row from %q", lens, idx.Dungeons, item.DungeonPath)
					}
				}
			}
		}
	}
}

func TestUndatedRowsSortLastAndFallOutsideDateRanges(t *testing.T) {
	items := []Item{
		{Title: "undated", Status: "completed", DateSource: DateUnknown, DungeonPath: ".dungeon", DungeonLabel: "Camp"},
		{DoneDate: "2026-10-01", Title: "old", Status: "completed", DateSource: DateBucket, DungeonPath: ".dungeon", DungeonLabel: "Camp"},
		{DoneDate: "2026-10-09", Title: "new", Status: "completed", DateSource: DateBucket, DungeonPath: ".dungeon", DungeonLabel: "Camp"},
	}
	Sort(items)
	if got := []string{items[0].Title, items[1].Title, items[2].Title}; !slices.Equal(got, []string{"new", "old", "undated"}) {
		t.Fatalf("sorted = %v, want the undated row after every dated one", got)
	}
	idx := Index{Items: items}
	for name, q := range map[string]Query{
		"since": {Since: "2026-01-01"},
		"until": {Until: "2026-12-31"},
		"both":  {Since: "2026-01-01", Until: "2026-12-31"},
	} {
		got, err := Apply(idx, q)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Items) != 2 {
			t.Fatalf("%s: rows = %+v, want the two dated rows", name, got.Items)
		}
	}
	if got, err := Apply(idx, Query{}); err != nil || len(got.Items) != 3 {
		t.Fatalf("no range: rows = %+v, error %v; want all three", got.Items, err)
	}
}
