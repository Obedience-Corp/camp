package selector

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func typeItems() []Item {
	return []Item{
		{ID: "idea", Label: "idea"},
		{ID: "feature", Label: "feature"},
		{ID: "bug", Label: "bug"},
		{ID: "research", Label: "research"},
		{ID: "chore", Label: "chore"},
		{ID: "feedback", Label: "feedback"},
	}
}

func keyRunes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestNewInitialIDIdea(t *testing.T) {
	t.Parallel()
	m := New(typeItems(), Options{InitialID: "idea", Help: "help"})
	got, ok := m.Selected()
	if !ok || got.ID != "idea" {
		t.Fatalf("selected = %v ok=%v, want idea", got.ID, ok)
	}
}

func TestNewWithoutInitialIDUsesLastUnpinned(t *testing.T) {
	t.Parallel()
	m := New(typeItems(), Options{})
	got, ok := m.Selected()
	if !ok || got.ID != "research" {
		t.Fatalf("name-asc last unpinned = %q, want research", got.ID)
	}
}

func TestNewRecencyWinnerIsLast(t *testing.T) {
	t.Parallel()
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	items := []Item{
		{ID: "agent", Label: "agent-simulator"},
		{ID: "fest", Label: "fest", Rank: old},
		{ID: "camp", Label: "camp", Rank: recent},
	}
	m := New(items, Options{})
	got, ok := m.Selected()
	if !ok || got.ID != "camp" {
		t.Fatalf("selected = %q, want camp (most recent last)", got.ID)
	}
}

func TestSlashThenJTypesJ(t *testing.T) {
	t.Parallel()
	m := New(typeItems(), Options{InitialID: "idea"})
	m, _ = m.Update(keyRunes("/"))
	if m.Query() != "" || !m.Filtering() {
		t.Fatalf("after /: query=%q filtering=%v", m.Query(), m.Filtering())
	}
	if !m.Consumed() {
		t.Fatal("/ must be consumed")
	}
	m, _ = m.Update(keyRunes("j"))
	if m.Query() != "j" || m.Mode() != ModeFilter {
		t.Fatalf("after /j: query=%q mode=%v", m.Query(), m.Mode())
	}
}

func TestNavigateJMoves(t *testing.T) {
	t.Parallel()
	m := New(typeItems(), Options{InitialID: "idea"})
	m, _ = m.Update(keyRunes("j"))
	got, _ := m.Selected()
	if got.ID == "idea" {
		t.Fatal("j in navigate should move")
	}
	if m.Query() != "" {
		t.Fatalf("j must not type in navigate, query=%q", m.Query())
	}
	if !m.Consumed() {
		t.Fatal("j in navigate is consumed")
	}
}

func TestNavigateBackspaceNotConsumed(t *testing.T) {
	t.Parallel()
	m := New(typeItems(), Options{InitialID: "idea"})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if m.Consumed() {
		t.Fatal("backspace in navigate must not be consumed")
	}
}

func TestNavigateLNotConsumed(t *testing.T) {
	t.Parallel()
	m := New(typeItems(), Options{InitialID: "idea"})
	m, _ = m.Update(keyRunes("l"))
	if m.Consumed() {
		t.Fatal("l in navigate must not be consumed")
	}
	if m.Query() != "" || m.Filtering() {
		t.Fatal("l must not start a filter")
	}
}

func TestTypeBEntersFilter(t *testing.T) {
	t.Parallel()
	m := New(typeItems(), Options{InitialID: "idea"})
	m, _ = m.Update(keyRunes("b"))
	if m.Query() != "b" || !m.Filtering() {
		t.Fatalf("b should filter, query=%q filtering=%v", m.Query(), m.Filtering())
	}
	m, _ = m.Update(keyRunes("u"))
	got, ok := m.Selected()
	if !ok || got.ID != "bug" {
		t.Fatalf("query bu selected = %q, want bug", got.ID)
	}
}

func TestKeepIDAcrossQueryEdits(t *testing.T) {
	t.Parallel()
	items := []Item{
		{ID: "camp", Label: "camp"},
		{ID: "camp-activity", Label: "camp-activity"},
		{ID: "camp-buzz", Label: "camp-buzz"},
	}
	m := New(items, Options{InitialID: "camp"})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	got, _ := m.Selected()
	if got.ID != "camp-activity" {
		t.Fatalf("moved to %q", got.ID)
	}
	m, _ = m.Update(keyRunes("c"))
	got, _ = m.Selected()
	if got.ID != "camp-activity" {
		t.Fatalf("query edit yanked to %q, want camp-activity", got.ID)
	}
}

func TestResetClearsQueryAndMode(t *testing.T) {
	t.Parallel()
	types := []Item{{ID: "projects", Label: "projects"}}
	m := New(types, Options{})
	m, _ = m.Update(keyRunes("p"))
	if m.Query() != "p" {
		t.Fatal("setup")
	}
	items := []Item{
		{ID: "agent", Label: "agent-simulator"},
		{ID: "camp", Label: "camp"},
	}
	m.Reset(items, Options{})
	if m.Query() != "" || m.Filtering() {
		t.Fatalf("Reset leaked filter: query=%q filtering=%v", m.Query(), m.Filtering())
	}
	view := m.View()
	if strings.Contains(view, "filter:") {
		t.Fatalf("Reset must not show filter row: %q", view)
	}
}

func TestSetItemsKeepsQuery(t *testing.T) {
	t.Parallel()
	items := []Item{{ID: "camp", Label: "camp"}, {ID: "fest", Label: "fest"}}
	m := New(items, Options{})
	m, _ = m.Update(keyRunes("c"))
	m.SetItems([]Item{
		{ID: "camp", Label: "camp", Rank: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)},
		{ID: "fest", Label: "fest"},
	})
	if m.Query() != "c" || !m.Filtering() {
		t.Fatalf("SetItems dropped query: %q filtering=%v", m.Query(), m.Filtering())
	}
}

func TestEscClearsFilterWithoutParentCancel(t *testing.T) {
	t.Parallel()
	m := New(typeItems(), Options{InitialID: "idea"})
	m, _ = m.Update(keyRunes("b"))
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.Query() != "" || m.Filtering() {
		t.Fatalf("esc should leave filter, query=%q filtering=%v", m.Query(), m.Filtering())
	}
	if !m.Consumed() {
		t.Fatal("esc in filter must be consumed so parent does not cancel")
	}
}

func TestBackspaceEmptyLeavesFilter(t *testing.T) {
	t.Parallel()
	m := New(typeItems(), Options{InitialID: "idea"})
	m, _ = m.Update(keyRunes("b"))
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if m.Query() != "" || m.Filtering() {
		t.Fatalf("backspace on last rune should leave filter, query=%q mode=%v", m.Query(), m.Mode())
	}
	if !m.Consumed() {
		t.Fatal("backspace in filter is consumed")
	}
}

func TestEnterNeverConsumed(t *testing.T) {
	t.Parallel()
	m := New(typeItems(), Options{InitialID: "idea"})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.Consumed() {
		t.Fatal("enter must never be consumed")
	}
	m, _ = m.Update(keyRunes("b"))
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.Consumed() {
		t.Fatal("enter in filter must never be consumed")
	}
}

func TestCtrlCNeverConsumed(t *testing.T) {
	t.Parallel()
	m := New(typeItems(), Options{})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if m.Consumed() {
		t.Fatal("ctrl+c must never be consumed")
	}
}

func TestNoMatchesKeepsQuery(t *testing.T) {
	t.Parallel()
	m := New(typeItems(), Options{InitialID: "idea", Help: "one help"})
	m, _ = m.Update(keyRunes("xyz"))
	if _, ok := m.Selected(); ok {
		t.Fatal("no matches: Selected should be ok=false")
	}
	view := m.View()
	if !strings.Contains(view, `no matches for "xyz"`) {
		t.Fatalf("view = %q", view)
	}
	if !strings.Contains(view, "filter: xyz") {
		t.Fatalf("missing query row: %q", view)
	}
}

func TestViewOneHelpLineAndQueryRow(t *testing.T) {
	t.Parallel()
	m := New(typeItems(), Options{InitialID: "idea", Help: "↑/↓ move · type to filter · enter select"})
	m, _ = m.Update(keyRunes("b"))
	view := m.View()
	if strings.Count(view, "enter select") != 1 {
		t.Fatalf("want one help line, view=%q", view)
	}
	if !strings.Contains(view, "filter: b") {
		t.Fatalf("missing query row: %q", view)
	}
}

func TestEmptyItems(t *testing.T) {
	t.Parallel()
	m := New(nil, Options{Help: "help"})
	if _, ok := m.Selected(); ok {
		t.Fatal("empty Selected ok")
	}
	if !strings.Contains(m.View(), "(no items)") {
		t.Fatalf("view=%q", m.View())
	}
}

func TestVisibleRangePinsTail(t *testing.T) {
	t.Parallel()
	start, end := visibleRange(8, 7, 5)
	if start != 3 || end != 8 {
		t.Fatalf("tail pin = [%d,%d), want [3,8)", start, end)
	}
	start, end = visibleRange(8, 0, 5)
	if start != 0 || end != 5 {
		t.Fatalf("head = [%d,%d), want [0,5)", start, end)
	}
}

func TestColdNoneInitialID(t *testing.T) {
	t.Parallel()
	items := []Item{
		{ID: "none", Label: "(none)", Pin: PinTop},
		{ID: "projects", Label: "projects"},
		{ID: "workflow", Label: "workflow"},
	}
	m := New(items, Options{InitialID: "none"})
	got, ok := m.Selected()
	if !ok || got.ID != "none" {
		t.Fatalf("cold camp should stay on (none), got %q", got.ID)
	}
}
