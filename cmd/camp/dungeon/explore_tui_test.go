package dungeon

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Obedience-Corp/camp/internal/dungeon/explore"
)

func TestExploreFeedShowsDayTitleAndDungeon(t *testing.T) {
	m := exploreModel{
		width:  100,
		height: 30,
		query:  explore.Query{Status: "finished", Dungeon: explore.LensAll},
		index: explore.Index{Dungeons: []explore.DungeonPrint{{Path: "festivals/.dungeon"}}, Items: []explore.Item{
			{DoneDate: "2026-10-05", DateSource: explore.DateBucket, Status: "completed", DungeonLabel: "Festivals", DungeonPath: "festivals/.dungeon", Kind: explore.KindFestival, Title: "festival-activity-desktop", ID: "FA0024", Summary: "Ship Festival Activity", Path: "festivals/.dungeon/completed/2026-10-05/festival-activity-desktop-FA0024", IsDir: true},
			{DoneDate: "2026-08-21", DateSource: explore.DateHistory, Status: "done", DungeonLabel: "Intents", DungeonPath: ".campaign/intents/.dungeon", Kind: explore.KindMarkdown, Title: "Add blog sections", Path: ".campaign/intents/.dungeon/done/add-blog.md"},
		}},
		protocol: explore.ProtocolOff,
	}
	if err := m.applyQuery(); err != nil {
		t.Fatal(err)
	}
	view := m.View()
	for _, want := range []string{"Festivals", "Finished", "festival-activity-desktop", "FA0024", "Add blog sections", "Intents"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q\n%s", want, view)
		}
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	moved := next.(exploreModel)
	if moved.cursor != 1 {
		t.Fatalf("cursor = %d", moved.cursor)
	}
	opened, _ := moved.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	hopped := opened.(exploreModel)
	if hopped.quitting {
		t.Fatal("g quit without shell integration")
	}
	if !strings.Contains(hopped.status, "shell-init") {
		t.Fatalf("status = %q", hopped.status)
	}
}

func TestExploreViewEmitsKittySequenceForFocusedReplay(t *testing.T) {
	m := exploreModel{
		width:    100,
		height:   30,
		protocol: explore.ProtocolKitty,
		query:    explore.Query{Status: "finished", Dungeon: explore.LensAll},
		index: explore.Index{
			Dungeons: []explore.DungeonPrint{{Path: "festivals/.dungeon"}},
			Items: []explore.Item{
				{
					DoneDate: "2026-10-05", DateSource: explore.DateBucket, Status: "completed",
					DungeonLabel: "Festivals", DungeonPath: "festivals/.dungeon", Kind: explore.KindFestival,
					Title: "festival-activity-desktop", ID: "FA0024", Path: "festivals/.dungeon/completed/2026-10-05/festival-activity-desktop-FA0024",
					Replay: "festivals/.dungeon/completed/2026-10-05/festival-activity-desktop-FA0024/festival-replay.gif", IsDir: true,
				},
				{
					DoneDate: "2026-08-21", DateSource: explore.DateHistory, Status: "done",
					DungeonLabel: "Intents", DungeonPath: ".campaign/intents/.dungeon", Kind: explore.KindMarkdown,
					Title: "Add blog sections", Path: ".campaign/intents/.dungeon/done/add-blog.md",
				},
			},
		},
		mediaFor: "festivals/.dungeon/completed/2026-10-05/festival-activity-desktop-FA0024",
		poster:   []byte{1, 2, 3, 4},
	}
	if err := m.applyQuery(); err != nil {
		t.Fatal(err)
	}
	view := m.View()
	if !strings.Contains(view, "a=T,f=100,q=2,i=1") {
		t.Fatal("focused festival did not emit a Kitty image sequence")
	}
	cleared, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if strings.Contains(cleared.View(), "a=T,f=100") {
		t.Fatal("image sequence remained after leaving the festival")
	}
}
