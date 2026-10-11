package dungeon

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Obedience-Corp/camp/internal/dungeon/explore"
)

func TestExploreRefreshResolvesQueryError(t *testing.T) {
	for _, query := range []explore.Query{{Dungeon: "Designs", Status: "all"}, {Status: "reviewed"}} {
		t.Run(query.Dungeon+query.Status, func(t *testing.T) {
			m := replayModel(explore.ProtocolOff)
			m.index = explore.Index{}
			m.query = query
			// Startup and keyboard changes must use the same validation path.
			if err := m.applyQuery(); err == nil {
				t.Fatal("outdated index unexpectedly resolved query")
			}
			m, _ = press(t, m, exploreLoaded{})
			if !m.statusErr || m.status == "" {
				t.Fatal("invalid query was not reported")
			}
			idx := refreshIndex("New design")
			m, _ = press(t, m, exploreLoaded{index: idx, changed: true})
			if m.statusErr || m.status != "" || strings.Contains(m.View(), "unknown") {
				t.Fatalf("resolved query still reports %q (error=%v)", m.status, m.statusErr)
			}
			if len(m.visible.Items) != 1 {
				t.Fatalf("resolved query has %d rows", len(m.visible.Items))
			}
		})
	}
}

func refreshIndex(titles ...string) explore.Index {
	idx := explore.Index{Dungeons: []explore.DungeonPrint{{Path: "workflow/design/.dungeon", Label: "Designs"}}}
	for _, title := range titles {
		idx.Items = append(idx.Items, explore.Item{
			Title: title, Path: "workflow/design/.dungeon/reviewed/" + title,
			DungeonPath: "workflow/design/.dungeon", DungeonLabel: "Designs", Status: "reviewed",
		})
	}
	return idx
}

func TestExploreRefreshPreservesFocusedItem(t *testing.T) {
	m := replayModel(explore.ProtocolOff)
	m.index, m.query = refreshIndex("B", "C"), explore.Query{Status: "all"}
	if err := m.applyQuery(); err != nil {
		t.Fatal(err)
	}
	m.cursor = 1
	m, _ = press(t, m, exploreLoaded{index: refreshIndex("A", "B", "C"), changed: true})
	if item, ok := m.focused(); !ok || item.Title != "C" {
		t.Fatalf("refresh changed selected item to %#v", item)
	}
	m, _ = press(t, m, exploreLoaded{index: refreshIndex("A"), changed: true})
	if item, ok := m.focused(); !ok || item.Title != "A" {
		t.Fatalf("removed selection did not fall back to first item: %#v", item)
	}
}

func TestExploreRefreshPreservesUnrelatedError(t *testing.T) {
	m := replayModel(explore.ProtocolOff)
	m, _ = press(t, m, runes("g"))
	want := m.status
	m, _ = press(t, m, exploreLoaded{})
	if !m.statusErr || m.status != want {
		t.Fatalf("refresh erased shell integration error: %q", m.status)
	}
}

func TestExploreInvalidQueryCannotActOnOldRows(t *testing.T) {
	m := replayModel(explore.ProtocolOff)
	m.query.Dungeon = "missing"
	if err := m.applyQuery(); err == nil {
		t.Fatal("missing dungeon accepted")
	}
	if _, ok := m.focused(); ok {
		t.Fatal("invalid lens kept an actionable row from the previous query")
	}
	m, _ = press(t, m, exploreLoaded{})
	if !m.statusErr || !strings.Contains(m.status, "missing") {
		t.Fatalf("unchanged refresh lost invalid query error: %q", m.status)
	}
	m, _ = press(t, m, runes("]"))
	if m.statusErr || m.status != "" || len(m.visible.Items) == 0 {
		t.Fatalf("switching to a valid lens did not recover: %q", m.status)
	}
}

func TestExploreRefreshFailureRecovery(t *testing.T) {
	m := replayModel(explore.ProtocolOff)
	m, _ = press(t, m, exploreLoaded{err: errors.New("scan failed")})
	if !m.statusErr || m.status != "scan failed" {
		t.Fatal("refresh failure not reported")
	}
	m, _ = press(t, m, exploreLoaded{})
	if m.statusErr || m.status != "" {
		t.Fatalf("successful refresh retained scan error: %q", m.status)
	}
}

func TestExploreRefreshIgnoresSupersededLoads(t *testing.T) {
	m := replayModel(explore.ProtocolOff)
	old := m.index
	m.query.Status = "all"
	m, cmd := press(t, m, runes("r"))
	if cmd == nil || !m.loading || len(m.index.Items) != len(old.Items) {
		t.Fatal("rescan did not retain the current index while loading")
	}
	gen := m.indexGen
	m, cmd = press(t, m, runes("r"))
	if cmd != nil || m.indexGen != gen {
		t.Fatal("repeated rescan started a duplicate load")
	}
	m, _ = press(t, m, exploreLoaded{gen: gen - 1, err: errors.New("old scan failure")})
	if !m.loading || m.statusErr || m.status != exploreReadingStatus {
		t.Fatal("superseded load error interrupted the active scan")
	}
	m, _ = press(t, m, exploreLoaded{gen: gen, index: refreshIndex("Current"), changed: true})
	m, cmd = press(t, m, exploreLoaded{gen: gen - 1, index: old, changed: true})
	if item, ok := m.focused(); !ok || item.Title != "Current" || cmd != nil || m.loading {
		t.Fatal("superseded load overwrote the refreshed feed")
	}
}

func TestExploreFailedRescanKeepsUsableIndex(t *testing.T) {
	m := replayModel(explore.ProtocolOff)
	before, _ := m.focused()
	m, _ = press(t, m, runes("r"))
	m, _ = press(t, m, exploreLoaded{gen: m.indexGen, err: errors.New("scan failed")})
	if !m.statusErr || m.loading || len(m.index.Items) == 0 {
		t.Fatal("failed rescan discarded the previous feed")
	}
	m, _ = press(t, m, runes("/"))
	m, _ = press(t, m, runes(before.Title))
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if item, ok := m.focused(); !ok || item.Path != before.Path {
		t.Fatal("failed rescan left the old feed visible but unqueryable")
	}
}

func TestExploreRefreshEmptyAndInvalidResults(t *testing.T) {
	for _, status := range []string{"all", "reviewed"} {
		t.Run(status, func(t *testing.T) {
			m := replayModel(explore.ProtocolOff)
			m.index, m.query = refreshIndex("Old"), explore.Query{Status: status}
			if err := m.applyQuery(); err != nil {
				t.Fatal(err)
			}
			m, _ = press(t, m, exploreLoaded{index: refreshIndex(), changed: true})
			if _, ok := m.focused(); ok || m.cursor != 0 {
				t.Fatal("empty refresh retained an actionable selection")
			}
			if m.statusErr != (status == "reviewed") {
				t.Fatalf("empty result has wrong error state: %q", m.status)
			}
		})
	}
}
