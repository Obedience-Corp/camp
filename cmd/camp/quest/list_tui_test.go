//go:build dev

package quest

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/quest"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/ui/uitest"
)

func questFixture() []questListItem {
	return []questListItem{
		{Name: "Launch", Status: quest.StatusOpen, RelPath: ".campaign/quests/launch", AbsPath: "/c/.campaign/quests/launch"},
		{Name: "Paused Work", Status: quest.StatusPaused, RelPath: ".campaign/quests/paused-work", AbsPath: "/c/.campaign/quests/paused-work"},
		{Name: "Done", Status: quest.StatusCompleted, RelPath: ".campaign/quests/done", AbsPath: "/c/.campaign/quests/done"},
		{Name: "Old", Status: quest.StatusArchived, RelPath: ".campaign/quests/old", AbsPath: "/c/.campaign/quests/old"},
	}
}

func questKey(m questListModel, s string) questListModel {
	var msg tea.KeyMsg
	switch s {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
	next, _ := m.Update(msg)
	return next.(questListModel)
}

func questSized(m questListModel, w, h int) questListModel {
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return next.(questListModel)
}

func TestQuestModel_LiveFilterHidesDungeon(t *testing.T) {
	m := newQuestListModel(questFixture(), nil)
	got := visibleQuestNames(m)
	want := "Launch,Paused Work"
	if got != want {
		t.Fatalf("live visible = %q, want %q", got, want)
	}
}

func TestQuestModel_FilterCycle(t *testing.T) {
	m := newQuestListModel(questFixture(), nil)
	m = questKey(m, "f") // all
	if m.filter != questFilterAll || visibleQuestNames(m) != "Launch,Paused Work,Done,Old" {
		t.Fatalf("all filter = %s names=%s", m.filter.label(), visibleQuestNames(m))
	}
	m = questKey(m, "f") // dungeon
	if m.filter != questFilterDungeon || visibleQuestNames(m) != "Done,Old" {
		t.Fatalf("dungeon filter = %s names=%s", m.filter.label(), visibleQuestNames(m))
	}
	m = questKey(m, "f") // live
	if m.filter != questFilterLive || visibleQuestNames(m) != "Launch,Paused Work" {
		t.Fatalf("live filter = %s names=%s", m.filter.label(), visibleQuestNames(m))
	}
}

func TestQuestModel_StatusFilter(t *testing.T) {
	m := newQuestListModel(questFixture(), []quest.Status{quest.StatusOpen, quest.StatusCompleted})
	m.filter = questFilterAll
	m.rebuildVisible()
	if visibleQuestNames(m) != "Launch,Done" {
		t.Fatalf("status filter visible = %q", visibleQuestNames(m))
	}
}

func TestQuestModel_NavigationWraps(t *testing.T) {
	m := newQuestListModel(questFixture(), nil)
	if m.cursor != 0 {
		t.Fatalf("cursor should start at 0, got %d", m.cursor)
	}
	m = questKey(m, "k")
	if m.cursor != len(m.visible)-1 {
		t.Fatalf("up from top should wrap to %d, got %d", len(m.visible)-1, m.cursor)
	}
	m = questKey(m, "j")
	if m.cursor != 0 {
		t.Fatalf("down from bottom should wrap to 0, got %d", m.cursor)
	}
}

func TestQuestModel_CopyPath(t *testing.T) {
	prev := ui.WriteClipboard
	t.Cleanup(func() { ui.WriteClipboard = prev })
	var copied string
	ui.WriteClipboard = func(s string) error { copied = s; return nil }

	m := newQuestListModel(questFixture(), nil)
	m = questKey(m, "y")
	if copied != "/c/.campaign/quests/launch" {
		t.Fatalf("copied = %q, want launch path", copied)
	}
	if m.status != "copied!" {
		t.Fatalf("status = %q, want copied!", m.status)
	}
}

func TestQuestModel_GoNeedsShellIntegration(t *testing.T) {
	m := newQuestListModel(questFixture(), nil)
	m = questKey(m, "enter")
	if m.quitting {
		t.Fatal("go without shell integration must not quit")
	}
	if m.gotoPath != "" {
		t.Fatalf("go without shell integration must not set a path, got %q", m.gotoPath)
	}
	if !m.statusErr || m.status == "" {
		t.Fatalf("go without shell integration should surface an error status, got %q", m.status)
	}
}

func TestQuestModel_GoWithShellIntegration(t *testing.T) {
	m := newQuestListModel(questFixture(), nil)
	m.gotoEnabled = true
	m.cursor = 1
	m = questKey(m, "enter")
	if !m.quitting {
		t.Fatal("go with shell integration should quit")
	}
	if m.gotoPath != "/c/.campaign/quests/paused-work" {
		t.Fatalf("gotoPath = %q", m.gotoPath)
	}
}

func TestQuestModel_QuitKeys(t *testing.T) {
	for _, k := range []string{"q", "esc"} {
		m := questKey(newQuestListModel(questFixture(), nil), k)
		if !m.quitting {
			t.Fatalf("%q should quit", k)
		}
	}
}

func TestQuestView_BoundedAtEverySize(t *testing.T) {
	sizes := []struct{ w, h int }{
		{120, 40}, {80, 24}, {60, 20}, {40, 10}, {30, 8}, {24, 6}, {20, 5}, {15, 6},
		{10, 3}, {8, 2}, {1, 1}, {0, 0}, {40, 1}, {1, 40},
	}
	for _, s := range sizes {
		m := questSized(newQuestListModel(questFixture(), nil), s.w, s.h)
		m.cursor = len(m.visible) - 1
		uitest.AssertBounded(t, m.View(), s.w, s.h)
	}
}

func TestQuestView_SelectionVisibleWhenScrolled(t *testing.T) {
	m := newQuestListModel(questFixture(), nil)
	m.filter = questFilterAll
	m.rebuildVisible()
	m = questSized(m, 40, 6)
	m.cursor = len(m.visible) - 1
	out := m.View()
	uitest.AssertBounded(t, out, 40, 6)
	if !strings.Contains(out, "> ") {
		t.Fatalf("selected cursor not rendered when scrolled:\n%s", out)
	}
}

func TestQuestView_UsesSharedChrome(t *testing.T) {
	m := questSized(newQuestListModel(questFixture(), nil), 80, 24)
	out := m.View()
	for _, want := range []string{"Quests", "showing: live", "Launch", "open"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q:\n%s", want, out)
		}
	}
}

func questListTestCmd() *cobra.Command {
	c := &cobra.Command{}
	c.Flags().BoolP("interactive", "i", false, "")
	c.Flags().Bool("all", false, "")
	c.Flags().Bool("dungeon", false, "")
	c.Flags().StringSlice("status", nil, "")
	return c
}

func TestQuestListTUIRequested(t *testing.T) {
	prevJSON := questListJSON
	t.Cleanup(func() { questListJSON = prevJSON })

	t.Run("bare TTY opens browser", func(t *testing.T) {
		questListJSON = false
		if !questListTUIRequested(questListTestCmd(), true) {
			t.Fatal("bare command in a TTY should open the browser")
		}
	})
	t.Run("no TTY prints table", func(t *testing.T) {
		questListJSON = false
		if questListTUIRequested(questListTestCmd(), false) {
			t.Fatal("non-terminal stdout should print the table")
		}
	})
	t.Run("json never opens browser", func(t *testing.T) {
		questListJSON = true
		if questListTUIRequested(questListTestCmd(), true) {
			t.Fatal("--json should never open the browser")
		}
	})
	t.Run("shaping flag prints table", func(t *testing.T) {
		questListJSON = false
		c := questListTestCmd()
		_ = c.Flags().Set("dungeon", "true")
		if questListTUIRequested(c, true) {
			t.Fatal("--dungeon should print the filtered table")
		}
	})
	t.Run("interactive forces browser", func(t *testing.T) {
		questListJSON = false
		c := questListTestCmd()
		_ = c.Flags().Set("dungeon", "true")
		_ = c.Flags().Set("interactive", "true")
		if !questListTUIRequested(c, false) {
			t.Fatal("-i should force the browser even with a shaping flag and no TTY")
		}
	})
}

func visibleQuestNames(m questListModel) string {
	names := make([]string, 0, len(m.visible))
	for _, e := range m.visible {
		names = append(names, e.Name)
	}
	return strings.Join(names, ",")
}
