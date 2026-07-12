package main

import (
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Obedience-Corp/camp/internal/config"
)

func responsiveModel() listTUIModel {
	ti := textinput.New()
	ti.Prompt = "> "
	entries := []campaignEntry{
		{ID: "A", Name: "alpha", Org: "default", Status: config.StatusActive, Path: "/tmp/a"},
		{ID: "B", Name: "this-is-an-extremely-long-campaign-name-that-should-truncate", Org: "obey", Status: config.StatusInactive, Path: "/Users/someone/very/deep/nested/directory/structure/inside/project-repo"},
		{ID: "C", Name: "gamma", Org: "obey", Status: config.StatusReference, Path: "/Users/someone/work/gamma"},
		{ID: "D", Name: "delta", Org: "platform", Status: config.StatusActive, Path: "/srv/delta"},
		{ID: "E", Name: "epsilon", Org: "platform", Status: config.StatusActive, Path: "/srv/epsilon/really/long/path/segment/that/keeps/going/on"},
		{ID: "F", Name: "zeta", Org: "research", Status: config.StatusInactive, Path: "/opt/zeta"},
	}
	return listTUIModel{ctx: context.Background(), fallback: "default", all: entries, visible: entries, input: ti}
}

func sized(m listTUIModel, w, h int) listTUIModel {
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return next.(listTUIModel)
}

func viewLines(s string) []string {
	return strings.Split(strings.TrimRight(s, "\n"), "\n")
}

func assertBounded(t *testing.T, out string, w, h int) {
	t.Helper()
	lines := viewLines(out)
	if len(lines) > h {
		t.Fatalf("%dx%d: rendered %d lines, exceeds height %d:\n%s", w, h, len(lines), h, out)
	}
	for i, ln := range lines {
		if got := lipgloss.Width(ln); got > w {
			t.Fatalf("%dx%d: line %d width %d exceeds terminal width %d: %q", w, h, i, got, w, ln)
		}
	}
}

func TestListView_BoundedAtEverySize(t *testing.T) {
	sizes := []struct{ w, h int }{
		{120, 40}, {80, 24}, {60, 20}, {40, 10}, {30, 8}, {24, 6}, {20, 5}, {15, 6},
	}
	for _, s := range sizes {
		m := sized(responsiveModel(), s.w, s.h)
		m.cursor = 3
		assertBounded(t, m.View(), s.w, s.h)
	}
}

func TestListView_HasNoPhantomTrailingRow(t *testing.T) {
	m := sized(responsiveModel(), 80, 24)
	out := m.View()
	if strings.HasSuffix(out, "\n") {
		t.Fatalf("full-screen list view ends with a phantom row: %q", out)
	}
	if got := len(strings.Split(out, "\n")); got > m.height {
		t.Fatalf("rendered %d rows for terminal height %d", got, m.height)
	}
}

func TestListView_SelectionVisibleWhenScrolled(t *testing.T) {
	m := sized(responsiveModel(), 40, 10)
	m.cursor = len(m.visible) - 1
	out := m.View()
	assertBounded(t, out, 40, 10)
	if !strings.Contains(out, "> ") {
		t.Fatalf("selected row cursor not rendered at 40x10:\n%s", out)
	}
}

func TestListView_NoPanicAtAbsurdSizes(t *testing.T) {
	sizes := []struct{ w, h int }{
		{10, 3}, {8, 2}, {5, 2}, {3, 3}, {1, 1}, {2, 1}, {0, 0}, {40, 1}, {1, 40},
	}
	for _, s := range sizes {
		m := sized(responsiveModel(), s.w, s.h)
		m.cursor = 2
		out := m.View()
		if out == "" {
			t.Fatalf("%dx%d produced empty view", s.w, s.h)
		}
		if s.w > 0 && s.h > 0 {
			assertBounded(t, out, s.w, s.h)
		}
	}
}

func TestListView_EmptyListBounded(t *testing.T) {
	m := responsiveModel()
	m.all = nil
	m.visible = nil
	for _, s := range []struct{ w, h int }{{80, 24}, {20, 5}, {10, 3}} {
		mm := sized(m, s.w, s.h)
		out := mm.View()
		if !strings.Contains(out, "no camp") {
			t.Fatalf("%dx%d empty view missing placeholder:\n%s", s.w, s.h, out)
		}
		if s.w > 0 && s.h > 0 {
			assertBounded(t, out, s.w, s.h)
		}
	}
}

func TestListView_OverlayBounded(t *testing.T) {
	for _, s := range []struct{ w, h int }{{80, 24}, {40, 10}, {20, 5}, {10, 3}} {
		m := sized(responsiveModel(), s.w, s.h)
		m.cursor = 1
		m.overlay = listOverlayMove
		out := m.View()
		if out == "" {
			t.Fatalf("%dx%d overlay empty", s.w, s.h)
		}
		if s.w > 0 && s.h > 0 {
			assertBounded(t, out, s.w, s.h)
		}
	}
}

func TestListView_FooterCollapsesWithWidth(t *testing.T) {
	wide := sized(responsiveModel(), 120, 40).footer(116)
	if !strings.Contains(wide, "copy") {
		t.Fatalf("wide footer should show the full help, got %q", wide)
	}
	narrow := sized(responsiveModel(), 30, 8).footer(26)
	if lipgloss.Width(narrow) > 26 {
		t.Fatalf("narrow footer width %d exceeds 26: %q", lipgloss.Width(narrow), narrow)
	}
	if strings.Contains(narrow, "copy") {
		t.Fatalf("narrow footer should have collapsed, got %q", narrow)
	}
}

func TestListColumns_NeverExceedsRemaining(t *testing.T) {
	for rem := 0; rem <= 120; rem++ {
		nameW, statusOn, pathW := listColumns(rem)
		used := nameW
		if statusOn {
			used += 2 + listStatusW
		}
		if pathW > 0 {
			used += 2 + pathW
		}
		if used > rem {
			t.Fatalf("rem=%d: columns use %d (name=%d status=%v path=%d)", rem, used, nameW, statusOn, pathW)
		}
		if nameW < 0 || pathW < 0 {
			t.Fatalf("rem=%d: negative width name=%d path=%d", rem, nameW, pathW)
		}
	}
}
