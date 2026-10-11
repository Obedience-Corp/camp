package explore

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	intexplore "github.com/Obedience-Corp/camp/internal/dungeon/explore"
)

func TestExploreFeedShowsDayTitleAndDungeon(t *testing.T) {
	m := exploreModel{
		width:  100,
		height: 30,
		query:  intexplore.Query{Status: "finished", Dungeon: intexplore.LensAll},
		index: intexplore.Index{Dungeons: []intexplore.DungeonPrint{{Path: "festivals/.dungeon"}}, Items: []intexplore.Item{
			{DoneDate: "2026-10-05", DateSource: intexplore.DateBucket, Status: "completed", DungeonLabel: "Festivals", DungeonPath: "festivals/.dungeon", Kind: intexplore.KindFestival, Title: "festival-activity-desktop", ID: "FA0024", Summary: "Ship Festival Activity", Path: "festivals/.dungeon/completed/2026-10-05/festival-activity-desktop-FA0024", IsDir: true},
			{DoneDate: "2026-08-21", DateSource: intexplore.DateHistory, Status: "done", DungeonLabel: "Intents", DungeonPath: ".campaign/intents/.dungeon", Kind: intexplore.KindMarkdown, Title: "Add blog sections", Path: ".campaign/intents/.dungeon/done/add-blog.md"},
		}},
		protocol: intexplore.ProtocolOff,
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
		protocol: intexplore.ProtocolKitty,
		query:    intexplore.Query{Status: "finished", Dungeon: intexplore.LensAll},
		index: intexplore.Index{
			Dungeons: []intexplore.DungeonPrint{{Path: "festivals/.dungeon"}},
			Items: []intexplore.Item{
				{
					DoneDate: "2026-10-05", DateSource: intexplore.DateBucket, Status: "completed",
					DungeonLabel: "Festivals", DungeonPath: "festivals/.dungeon", Kind: intexplore.KindFestival,
					Title: "festival-activity-desktop", ID: "FA0024", Path: "festivals/.dungeon/completed/2026-10-05/festival-activity-desktop-FA0024",
					Replay: "festivals/.dungeon/completed/2026-10-05/festival-activity-desktop-FA0024/festival-replay.gif", IsDir: true,
				},
				{
					DoneDate: "2026-08-21", DateSource: intexplore.DateHistory, Status: "done",
					DungeonLabel: "Intents", DungeonPath: ".campaign/intents/.dungeon", Kind: intexplore.KindMarkdown,
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
