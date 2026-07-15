package worktrees

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/ui/uitest"
)

func wtFixture() []WorktreeListItem {
	return []WorktreeListItem{
		{Project: "api", Name: "feature-auth", Path: "/w/api/feature-auth", Branch: "feature-auth", LastAccessed: "2 hours ago"},
		{Project: "api", Name: "bugfix", Path: "/w/api/bugfix", Branch: "bugfix", LastAccessed: "1 day ago", Stale: true, StaleReason: "missing .git"},
		{Project: "web", Name: "redesign", Path: "/w/web/redesign", Branch: "redesign", LastAccessed: "just now"},
	}
}

func wtKey(m wtListModel, s string) wtListModel {
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
	return next.(wtListModel)
}

func wtSized(m wtListModel, w, h int) wtListModel {
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return next.(wtListModel)
}

// newWtListModel sorts into a stable (project, name) order so headers render
// once and navigation is deterministic.
func TestWtModel_SortsByProjectThenName(t *testing.T) {
	m := newWtListModel(wtFixture())
	got := []string{}
	for _, e := range m.visible {
		got = append(got, e.Project+"/"+e.Name)
	}
	want := []string{"api/bugfix", "api/feature-auth", "web/redesign"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestWtModel_NavigationWraps(t *testing.T) {
	m := newWtListModel(wtFixture())
	if m.cursor != 0 {
		t.Fatalf("cursor should start at 0, got %d", m.cursor)
	}
	m = wtKey(m, "k") // up from top wraps to last
	if m.cursor != len(m.visible)-1 {
		t.Fatalf("up from top should wrap to %d, got %d", len(m.visible)-1, m.cursor)
	}
	m = wtKey(m, "j") // down wraps back to top
	if m.cursor != 0 {
		t.Fatalf("down from bottom should wrap to 0, got %d", m.cursor)
	}
}

func TestWtModel_StaleFilterToggle(t *testing.T) {
	m := newWtListModel(wtFixture())
	if len(m.visible) != 3 {
		t.Fatalf("expected 3 visible, got %d", len(m.visible))
	}
	m = wtKey(m, "f") // stale-only
	if len(m.visible) != 1 || !m.visible[0].Stale {
		t.Fatalf("stale filter should leave the one stale worktree, got %d", len(m.visible))
	}
	m = wtKey(m, "f") // back to all
	if len(m.visible) != 3 {
		t.Fatalf("toggling filter off should restore all, got %d", len(m.visible))
	}
}

func TestWtModel_CopyPath(t *testing.T) {
	prev := ui.WriteClipboard
	t.Cleanup(func() { ui.WriteClipboard = prev })
	var copied string
	ui.WriteClipboard = func(s string) error { copied = s; return nil }

	m := newWtListModel(wtFixture()) // cursor 0 = api/bugfix
	m = wtKey(m, "y")
	if copied != "/w/api/bugfix" {
		t.Fatalf("copied = %q, want /w/api/bugfix", copied)
	}
	if m.status != "copied!" {
		t.Fatalf("status = %q, want copied!", m.status)
	}
}

func TestWtModel_GoNeedsShellIntegration(t *testing.T) {
	m := newWtListModel(wtFixture())
	m = wtKey(m, "enter")
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

func TestWtModel_GoWithShellIntegration(t *testing.T) {
	m := newWtListModel(wtFixture())
	m.gotoEnabled = true
	m.cursor = 2 // web/redesign
	m = wtKey(m, "enter")
	if !m.quitting {
		t.Fatal("go with shell integration should quit")
	}
	if m.gotoPath != "/w/web/redesign" {
		t.Fatalf("gotoPath = %q, want /w/web/redesign", m.gotoPath)
	}
}

func TestWtModel_QuitKeys(t *testing.T) {
	for _, k := range []string{"q", "esc"} {
		m := wtKey(newWtListModel(wtFixture()), k)
		if !m.quitting {
			t.Fatalf("%q should quit", k)
		}
	}
}

func TestWtView_BoundedAtEverySize(t *testing.T) {
	sizes := []struct{ w, h int }{
		{120, 40}, {80, 24}, {60, 20}, {40, 10}, {30, 8}, {24, 6}, {20, 5}, {15, 6},
		{10, 3}, {8, 2}, {1, 1}, {0, 0}, {40, 1}, {1, 40},
	}
	for _, s := range sizes {
		m := wtSized(newWtListModel(wtFixture()), s.w, s.h)
		m.cursor = 2
		uitest.AssertBounded(t, m.View(), s.w, s.h)
	}
}

func TestWtView_SelectionVisibleWhenScrolled(t *testing.T) {
	m := wtSized(newWtListModel(wtFixture()), 40, 6)
	m.cursor = len(m.visible) - 1
	out := m.View()
	uitest.AssertBounded(t, out, 40, 6)
	if !strings.Contains(out, "> ") {
		t.Fatalf("selected cursor not rendered when scrolled:\n%s", out)
	}
}

func wtTestCmd() *cobra.Command {
	c := &cobra.Command{}
	c.Flags().BoolP("interactive", "i", false, "")
	c.Flags().StringP("project", "p", "", "")
	c.Flags().Bool("stale", false, "")
	return c
}

func TestWorktreesListTUIRequested(t *testing.T) {
	prevJSON := listJSON
	t.Cleanup(func() { listJSON = prevJSON })

	t.Run("bare TTY opens browser", func(t *testing.T) {
		listJSON = false
		if !worktreesListTUIRequested(wtTestCmd(), true) {
			t.Fatal("bare command in a TTY should open the browser")
		}
	})
	t.Run("no TTY prints table", func(t *testing.T) {
		listJSON = false
		if worktreesListTUIRequested(wtTestCmd(), false) {
			t.Fatal("non-terminal stdout should print the table")
		}
	})
	t.Run("json never opens browser", func(t *testing.T) {
		listJSON = true
		if worktreesListTUIRequested(wtTestCmd(), true) {
			t.Fatal("--json should never open the browser")
		}
	})
	t.Run("shaping flag prints table", func(t *testing.T) {
		listJSON = false
		c := wtTestCmd()
		_ = c.Flags().Set("stale", "true")
		if worktreesListTUIRequested(c, true) {
			t.Fatal("--stale should print the filtered table")
		}
	})
	t.Run("interactive forces browser", func(t *testing.T) {
		listJSON = false
		c := wtTestCmd()
		_ = c.Flags().Set("stale", "true")
		_ = c.Flags().Set("interactive", "true")
		if !worktreesListTUIRequested(c, false) {
			t.Fatal("-i should force the browser even with a shaping flag and no TTY")
		}
	})
}
