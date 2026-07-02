package tui

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/priority"
)

func makeTestItems(n int) []workitem.WorkItem {
	items := make([]workitem.WorkItem, n)
	now := time.Now()
	for i := range items {
		items[i] = workitem.WorkItem{
			Key:           "test:" + string(rune('a'+i)),
			WorkflowType:  workitem.WorkflowTypeDesign,
			Title:         "Item " + string(rune('A'+i)),
			RelativePath:  "workflow/design/item-" + string(rune('a'+i)),
			ItemKind:      workitem.ItemKindDirectory,
			SortTimestamp: now.Add(-time.Duration(i) * time.Hour),
			CreatedAt:     now.Add(-time.Duration(i) * time.Hour),
		}
	}
	return items
}

func TestModel_CursorDown(t *testing.T) {
	items := makeTestItems(5)
	m := New(context.Background(), items, "", nil, priority.NewStore(), "")

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = result.(Model)
	if m.cursor != 1 {
		t.Errorf("cursor after j = %d, want 1", m.cursor)
	}
}

func TestModel_CursorUp(t *testing.T) {
	items := makeTestItems(5)
	m := New(context.Background(), items, "", nil, priority.NewStore(), "")
	m.cursor = 3

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = result.(Model)
	if m.cursor != 2 {
		t.Errorf("cursor after k = %d, want 2", m.cursor)
	}
}

func TestModel_CursorDoesNotGoBelowZero(t *testing.T) {
	items := makeTestItems(5)
	m := New(context.Background(), items, "", nil, priority.NewStore(), "")
	m.cursor = 0

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = result.(Model)
	if m.cursor != 0 {
		t.Errorf("cursor should not go below 0, got %d", m.cursor)
	}
}

func TestModel_GJumpsToBottom(t *testing.T) {
	items := makeTestItems(5)
	m := New(context.Background(), items, "", nil, priority.NewStore(), "")

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	m = result.(Model)
	if m.cursor != 4 {
		t.Errorf("cursor after G = %d, want 4", m.cursor)
	}
}

func TestModel_GGJumpsToTop(t *testing.T) {
	items := makeTestItems(5)
	m := New(context.Background(), items, "", nil, priority.NewStore(), "")
	m.cursor = 4

	// First g
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m = result.(Model)
	// Second g
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m = result.(Model)
	if m.cursor != 0 {
		t.Errorf("cursor after gg = %d, want 0", m.cursor)
	}
}

func TestModel_TypeFilter(t *testing.T) {
	items := []workitem.WorkItem{
		{WorkflowType: workitem.WorkflowTypeIntent, Title: "intent"},
		{WorkflowType: workitem.WorkflowTypeDesign, Title: "design"},
		{WorkflowType: workitem.WorkflowTypeFestival, Title: "fest"},
	}
	m := New(context.Background(), items, "", nil, priority.NewStore(), "")

	// Press 1 to filter intents
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	m = result.(Model)
	if len(m.filteredItems) != 1 {
		t.Errorf("after '1': %d items, want 1", len(m.filteredItems))
	}
	if m.filteredItems[0].Title != "intent" {
		t.Errorf("filtered item = %q, want 'intent'", m.filteredItems[0].Title)
	}

	// Press 0 to clear
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}})
	m = result.(Model)
	if len(m.filteredItems) != 3 {
		t.Errorf("after '0': %d items, want 3", len(m.filteredItems))
	}
}

func TestFilterOptions(t *testing.T) {
	tests := []struct {
		name   string
		types  []string
		ensure string
		want   []string
	}{
		{"builtins canonical then customs sorted", []string{"feature", "festival", "bug", "intent", "bug"}, "",
			[]string{"", "intent", "festival", "bug", "feature"}},
		{"builtins only", []string{"design", "intent"}, "", []string{"", "intent", "design"}},
		{"skips empty type", []string{"", "chore"}, "", []string{"", "chore"}},
		{"no cap", []string{"f", "e", "d", "c", "b", "a"}, "", []string{"", "a", "b", "c", "d", "e", "f"}},
		{"ensure keeps absent custom", []string{"intent", "chore"}, "bug", []string{"", "intent", "bug", "chore"}},
		{"ensure keeps absent builtin", []string{"intent", "bug"}, "explore", []string{"", "intent", "explore", "bug"}},
		{"ensure already present", []string{"bug"}, "bug", []string{"", "bug"}},
		{"no items", nil, "bug", []string{"", "bug"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items := make([]workitem.WorkItem, len(tt.types))
			for i, typ := range tt.types {
				items[i] = workitem.WorkItem{WorkflowType: workitem.WorkflowType(typ)}
			}
			got := deriveFilterOptions(items, tt.ensure)
			if len(got) != len(tt.want) {
				t.Fatalf("deriveFilterOptions = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("deriveFilterOptions = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func assertFilterStateConsistent(t *testing.T, m Model) {
	t.Helper()
	if !m.isFilterMode() {
		t.Fatal("expected filter mode to be active")
	}
	if m.filterIndex < 0 || m.filterIndex >= len(m.filterOptions) {
		t.Fatalf("filterIndex %d out of range of options %v", m.filterIndex, m.filterOptions)
	}
	if m.filterOptions[m.filterIndex] != m.typeFilter {
		t.Fatalf("highlighted chip %q != applied filter %q", m.filterOptions[m.filterIndex], m.typeFilter)
	}
}

func TestModel_FilterModeStepsLive(t *testing.T) {
	items := []workitem.WorkItem{
		{WorkflowType: workitem.WorkflowTypeIntent, Title: "intent"},
		{WorkflowType: "feature", Title: "feature item"},
		{WorkflowType: "bug", Title: "bug one"},
		{WorkflowType: "bug", Title: "bug two"},
	}
	m := New(context.Background(), items, "", nil, priority.NewStore(), "")

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = result.(Model)
	if !m.isFilterMode() {
		t.Fatal("expected filter mode after 'f'")
	}
	if m.filterIndex != 0 || m.typeFilter != "" {
		t.Fatalf("entering mode changed the filter: index=%d filter=%q", m.filterIndex, m.typeFilter)
	}

	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = result.(Model)
	if m.typeFilter != "intent" || len(m.filteredItems) != 1 {
		t.Fatalf("step 1: filter=%q items=%d, want intent/1", m.typeFilter, len(m.filteredItems))
	}

	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = result.(Model)
	if m.typeFilter != "bug" || len(m.filteredItems) != 2 {
		t.Fatalf("step 2: filter=%q items=%d, want bug/2", m.typeFilter, len(m.filteredItems))
	}

	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = result.(Model)
	if m.typeFilter != "feature" || len(m.filteredItems) != 1 {
		t.Fatalf("step 3: filter=%q items=%d, want feature/1", m.typeFilter, len(m.filteredItems))
	}

	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = result.(Model)
	if m.typeFilter != "feature" {
		t.Fatalf("step past end moved filter to %q", m.typeFilter)
	}

	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	m = result.(Model)
	if m.typeFilter != "bug" || len(m.filteredItems) != 2 {
		t.Fatalf("h step back: filter=%q items=%d, want bug/2", m.typeFilter, len(m.filteredItems))
	}
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = result.(Model)
	if m.typeFilter != "feature" || len(m.filteredItems) != 1 {
		t.Fatalf("l step forward: filter=%q items=%d, want feature/1", m.typeFilter, len(m.filteredItems))
	}

	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = result.(Model)
	if m.isFilterMode() {
		t.Fatal("expected filter mode to exit on second f")
	}
	if m.typeFilter != "feature" || len(m.filteredItems) != 1 {
		t.Fatalf("after f commit: filter=%q items=%d, want feature/1", m.typeFilter, len(m.filteredItems))
	}
}

func TestModel_FilterModeRespectsCommittedSearch(t *testing.T) {
	items := []workitem.WorkItem{
		{WorkflowType: workitem.WorkflowTypeIntent, Title: "Auth Feature"},
		{WorkflowType: "bug", Title: "Auth Bug"},
		{WorkflowType: "bug", Title: "Other Bug"},
		{WorkflowType: "feature", Title: "Something Else"},
	}
	m := New(context.Background(), items, "", nil, priority.NewStore(), "")
	m.width = 120
	m.height = 24
	m.ready = true

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = result.(Model)
	for _, r := range "auth" {
		result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = result.(Model)
	}
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = result.(Model)
	if m.searchQuery != "auth" || len(m.filteredItems) != 2 {
		t.Fatalf("committed search: query=%q items=%d, want auth/2", m.searchQuery, len(m.filteredItems))
	}

	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = result.(Model)
	assertFilterStateConsistent(t, m)

	want := []string{"", "intent", "bug"}
	if len(m.filterOptions) != len(want) {
		t.Fatalf("filterOptions = %v, want %v (types without search matches must not be offered)", m.filterOptions, want)
	}
	for i := range want {
		if m.filterOptions[i] != want[i] {
			t.Fatalf("filterOptions = %v, want %v", m.filterOptions, want)
		}
	}

	counts, total := m.visibleTypeCounts()
	if total != 2 || counts["bug"] != 1 || counts["intent"] != 1 {
		t.Errorf("counts = %v total=%d, want search-narrowed counts bug=1 intent=1 total=2", counts, total)
	}

	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = result.(Model)
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = result.(Model)
	if m.typeFilter != "bug" || len(m.filteredItems) != 1 {
		t.Fatalf("step to bug under search: filter=%q items=%d, want bug/1 (chip count matches list)", m.typeFilter, len(m.filteredItems))
	}
}

func TestModel_FilterModeEscRestores(t *testing.T) {
	items := []workitem.WorkItem{
		{WorkflowType: workitem.WorkflowTypeIntent, Title: "intent"},
		{WorkflowType: "bug", Title: "bug one"},
		{WorkflowType: "bug", Title: "bug two"},
	}
	m := New(context.Background(), items, "", nil, priority.NewStore(), "")

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	m = result.(Model)

	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = result.(Model)
	if m.filterIndex != 1 {
		t.Fatalf("filterIndex = %d, want 1 (positioned on active filter)", m.filterIndex)
	}

	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = result.(Model)
	if m.typeFilter != "bug" || len(m.filteredItems) != 2 {
		t.Fatalf("step: filter=%q items=%d, want bug/2", m.typeFilter, len(m.filteredItems))
	}

	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = result.(Model)
	if m.isFilterMode() {
		t.Fatal("expected filter mode to exit on esc")
	}
	if m.typeFilter != "intent" || len(m.filteredItems) != 1 {
		t.Fatalf("after esc: filter=%q items=%d, want intent/1", m.typeFilter, len(m.filteredItems))
	}
}

func TestModel_FilterModeZeroJumpsToAll(t *testing.T) {
	items := []workitem.WorkItem{
		{WorkflowType: workitem.WorkflowTypeIntent, Title: "intent"},
		{WorkflowType: "bug", Title: "bug"},
	}
	m := New(context.Background(), items, "", nil, priority.NewStore(), "")

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = result.(Model)
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = result.(Model)
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}})
	m = result.(Model)

	if m.filterIndex != 0 || m.typeFilter != "" {
		t.Fatalf("after '0': index=%d filter=%q, want 0/all", m.filterIndex, m.typeFilter)
	}
	if len(m.filteredItems) != 2 {
		t.Errorf("after '0': %d items, want 2", len(m.filteredItems))
	}
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = result.(Model)
	if m.filterIndex != 0 {
		t.Errorf("step before start moved index to %d", m.filterIndex)
	}
}

func TestModel_FilterModeBuiltinDigitsJump(t *testing.T) {
	items := []workitem.WorkItem{
		{WorkflowType: workitem.WorkflowTypeIntent, Title: "intent"},
		{WorkflowType: workitem.WorkflowTypeDesign, Title: "design"},
		{WorkflowType: "bug", Title: "bug"},
	}
	m := New(context.Background(), items, "", nil, priority.NewStore(), "")

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = result.(Model)

	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = result.(Model)
	assertFilterStateConsistent(t, m)
	if m.typeFilter != "design" || len(m.filteredItems) != 1 {
		t.Fatalf("after '2': filter=%q items=%d, want design/1", m.typeFilter, len(m.filteredItems))
	}

	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	m = result.(Model)
	assertFilterStateConsistent(t, m)
	if m.typeFilter != "design" {
		t.Fatalf("'3' with no explore chip moved filter to %q, want unchanged design", m.typeFilter)
	}
}

func TestModel_CustomDigitsUnbound(t *testing.T) {
	items := []workitem.WorkItem{
		{WorkflowType: workitem.WorkflowTypeIntent, Title: "intent"},
		{WorkflowType: "bug", Title: "bug"},
	}
	m := New(context.Background(), items, "", nil, priority.NewStore(), "")

	for _, key := range []rune{'5', '6', '7', '8', '9'} {
		result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		m = result.(Model)
		if m.typeFilter != "" {
			t.Fatalf("digit %q bound a filter: %q", key, m.typeFilter)
		}
	}
	if len(m.filteredItems) != 2 {
		t.Errorf("filtered = %d, want 2", len(m.filteredItems))
	}
}

func TestModel_FilterModeRefreshRebuildsOptions(t *testing.T) {
	items := []workitem.WorkItem{
		{Key: "test:intent", WorkflowType: workitem.WorkflowTypeIntent, Title: "intent"},
		{Key: "test:bug", WorkflowType: "bug", Title: "bug"},
	}
	m := New(context.Background(), items, "", nil, priority.NewStore(), "")

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = result.(Model)
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = result.(Model)
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = result.(Model)
	if m.typeFilter != "bug" {
		t.Fatalf("filter = %q, want bug", m.typeFilter)
	}

	refreshed := []workitem.WorkItem{
		{Key: "test:intent", WorkflowType: workitem.WorkflowTypeIntent, Title: "intent"},
		{Key: "test:chore", WorkflowType: "chore", Title: "chore"},
	}
	result, _ = m.Update(refreshMsg{items: refreshed})
	m = result.(Model)

	want := []string{"", "intent", "bug", "chore"}
	if len(m.filterOptions) != len(want) {
		t.Fatalf("filterOptions = %v, want %v", m.filterOptions, want)
	}
	for i := range want {
		if m.filterOptions[i] != want[i] {
			t.Fatalf("filterOptions = %v, want %v", m.filterOptions, want)
		}
	}
	assertFilterStateConsistent(t, m)
	if m.typeFilter != "bug" {
		t.Errorf("typeFilter = %q, want the kept 'bug' filter", m.typeFilter)
	}
	if len(m.filteredItems) != 0 {
		t.Errorf("kept 'bug' filter: %d items, want 0", len(m.filteredItems))
	}
	m.width = 120
	m.height = 24
	m.ready = true
	if footer := m.renderFooter(); !strings.Contains(footer, "[bug 0]") {
		t.Errorf("footer = %q, want the kept filter shown as zero-count chip", footer)
	}
}

func TestModel_FilterModeRefreshHidesParkedOnlyTypes(t *testing.T) {
	items := []workitem.WorkItem{
		{Key: "test:intent", WorkflowType: workitem.WorkflowTypeIntent, Title: "intent"},
		{Key: "test:bug", WorkflowType: "bug", Title: "bug", ItemKind: workitem.ItemKindDirectory},
	}
	store := priority.NewStore()
	priority.SetAttentionStage(store, "test:parked", priority.AttentionParked)
	m := New(context.Background(), items, "", nil, store, "")

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = result.(Model)

	refreshed := []workitem.WorkItem{
		{Key: "test:intent", WorkflowType: workitem.WorkflowTypeIntent, Title: "intent"},
		{Key: "test:bug", WorkflowType: "bug", Title: "bug", ItemKind: workitem.ItemKindDirectory},
		{Key: "test:parked", WorkflowType: "chore", Title: "parked chore",
			ItemKind: workitem.ItemKindDirectory, LifecycleStage: workitem.LifecycleStageNone},
	}
	result, _ = m.Update(refreshMsg{items: refreshed})
	m = result.(Model)
	for _, item := range m.allItems {
		if item.Key == "test:parked" && item.AttentionStage != "parked" {
			t.Fatalf("test setup: parked item stage = %q, want parked", item.AttentionStage)
		}
	}

	assertFilterStateConsistent(t, m)
	want := []string{"", "intent", "bug"}
	if len(m.filterOptions) != len(want) {
		t.Fatalf("filterOptions = %v, want %v (parked-only type must not be offered)", m.filterOptions, want)
	}
	for i := range want {
		if m.filterOptions[i] != want[i] {
			t.Fatalf("filterOptions = %v, want %v", m.filterOptions, want)
		}
	}

	counts, total := m.visibleTypeCounts()
	if total != 2 || counts["chore"] != 0 {
		t.Errorf("counts = %v total=%d, want parked chore excluded", counts, total)
	}
}

func TestModel_FilterModeShowParkedOffersParkedTypes(t *testing.T) {
	items := []workitem.WorkItem{
		{Key: "test:intent", WorkflowType: workitem.WorkflowTypeIntent, Title: "intent"},
		{Key: "test:parked", WorkflowType: "chore", Title: "parked chore", AttentionStage: "parked"},
	}
	m := New(context.Background(), items, "", nil, priority.NewStore(), "", true)

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = result.(Model)

	assertFilterStateConsistent(t, m)
	want := []string{"", "intent", "chore"}
	if len(m.filterOptions) != len(want) {
		t.Fatalf("filterOptions = %v, want %v (parked types offered with --show-parked)", m.filterOptions, want)
	}
	for i := range want {
		if m.filterOptions[i] != want[i] {
			t.Fatalf("filterOptions = %v, want %v", m.filterOptions, want)
		}
	}

	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = result.(Model)
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = result.(Model)
	if m.typeFilter != "chore" || len(m.filteredItems) != 1 {
		t.Fatalf("step to chore: filter=%q items=%d, want chore/1", m.typeFilter, len(m.filteredItems))
	}
}

func TestModel_FilterModeEntryWithAbsentBuiltin(t *testing.T) {
	items := []workitem.WorkItem{
		{WorkflowType: workitem.WorkflowTypeIntent, Title: "intent"},
		{WorkflowType: "bug", Title: "bug"},
	}
	m := New(context.Background(), items, "", nil, priority.NewStore(), "")
	m.width = 120
	m.height = 24
	m.ready = true

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	m = result.(Model)
	if m.typeFilter != "explore" || len(m.filteredItems) != 0 {
		t.Fatalf("after '3': filter=%q items=%d, want explore/0", m.typeFilter, len(m.filteredItems))
	}

	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = result.(Model)
	assertFilterStateConsistent(t, m)
	if m.typeFilter != "explore" {
		t.Fatalf("entering filter mode changed filter to %q", m.typeFilter)
	}
	if footer := m.renderFooter(); !strings.Contains(footer, "[explore 0]") {
		t.Errorf("footer = %q, want active explore chip with zero count", footer)
	}

	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = result.(Model)
	assertFilterStateConsistent(t, m)
	if m.typeFilter != "bug" || len(m.filteredItems) != 1 {
		t.Fatalf("step off absent chip: filter=%q items=%d, want bug/1", m.typeFilter, len(m.filteredItems))
	}
}

func TestModel_HelpListsFilterMode(t *testing.T) {
	m := New(context.Background(), makeTestItems(2), "", nil, priority.NewStore(), "")
	m.width = 80
	m.height = 24
	m.ready = true
	m.helpVisible = true

	help := m.View()
	if !strings.Contains(help, "Filter by type") {
		t.Error("help missing the 'f' filter mode row")
	}
	if !strings.Contains(help, "Filter: intent") {
		t.Error("help missing builtin accelerator rows")
	}
}

func TestModel_FooterRendersFilterChips(t *testing.T) {
	items := []workitem.WorkItem{
		{WorkflowType: workitem.WorkflowTypeIntent, Title: "intent"},
		{WorkflowType: "bug", Title: "bug one"},
		{WorkflowType: "bug", Title: "bug two"},
	}
	m := New(context.Background(), items, "", nil, priority.NewStore(), "")
	m.width = 120
	m.height = 24
	m.ready = true

	footer := m.renderFooter()
	if !strings.Contains(footer, "f filter") {
		t.Errorf("normal footer = %q, want it to contain 'f filter'", footer)
	}

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = result.(Model)
	footer = m.renderFooter()
	for _, want := range []string{"[all 3]", "intent 1", "bug 2", "Enter apply", "Esc cancel"} {
		if !strings.Contains(footer, want) {
			t.Errorf("filter-mode footer = %q, missing %q", footer, want)
		}
	}
}

func TestChipWindow(t *testing.T) {
	labels := []string{"all 40", "intent 10", "design 10", "bug 10", "feature 10"}
	tests := []struct {
		name      string
		active    int
		avail     int
		wantStart int
		wantEnd   int
	}{
		{"everything fits", 2, 200, 0, 5},
		{"window around active", 3, 34, 2, 5},
		{"active only", 0, 14, 0, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end := chipWindow(labels, tt.active, tt.avail)
			if start != tt.wantStart || end != tt.wantEnd {
				t.Errorf("chipWindow = [%d, %d), want [%d, %d)", start, end, tt.wantStart, tt.wantEnd)
			}
			if tt.active < start || tt.active >= end {
				t.Errorf("active %d outside window [%d, %d)", tt.active, start, end)
			}
			if got := chipRowWidth(labels, tt.active, start, end); got > tt.avail && (end-start) > 1 {
				t.Errorf("window width %d exceeds avail %d", got, tt.avail)
			}
		})
	}
}

func TestModel_FilterChipsFitNarrowWidth(t *testing.T) {
	items := []workitem.WorkItem{
		{WorkflowType: workitem.WorkflowTypeIntent, Title: "a"},
		{WorkflowType: "alpha", Title: "b"},
		{WorkflowType: "bug", Title: "c"},
		{WorkflowType: "chore", Title: "d"},
		{WorkflowType: "feature", Title: "e"},
		{WorkflowType: "zebra", Title: "f"},
	}
	m := New(context.Background(), items, "", nil, priority.NewStore(), "")
	m.width = 30
	m.height = 24
	m.ready = true

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = result.(Model)
	for range 4 {
		result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
		m = result.(Model)
	}
	assertFilterStateConsistent(t, m)
	if m.typeFilter != "chore" {
		t.Fatalf("filter = %q, want chore (middle chip)", m.typeFilter)
	}

	footer := m.renderFooter()
	if got := lipgloss.Width(footer); got > m.width {
		t.Errorf("footer width %d exceeds terminal width %d: %q", got, m.width, footer)
	}
	if !strings.Contains(footer, "[chore 1]") {
		t.Errorf("footer = %q, missing active chip", footer)
	}
	if strings.Count(footer, "…") != 2 {
		t.Errorf("footer = %q, want ellipses on both sides", footer)
	}

	for _, width := range []int{12, 16, 20, 24, 40, 60} {
		m.width = width
		if got := lipgloss.Width(m.renderFooter()); got > width {
			t.Errorf("width %d: footer width %d overflows", width, got)
		}
	}
}

func TestModel_EnterSelectsItem(t *testing.T) {
	items := makeTestItems(3)
	m := New(context.Background(), items, "", nil, priority.NewStore(), "")
	// Move to second item
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = result.(Model)

	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = result.(Model)

	if m.Selected == nil {
		t.Fatal("expected Selected to be set after Enter")
	}
	if m.Selected.Title != items[1].Title {
		t.Errorf("selected = %q, want %q", m.Selected.Title, items[1].Title)
	}
}

func TestModel_SearchEnterCommits(t *testing.T) {
	items := []workitem.WorkItem{
		{WorkflowType: workitem.WorkflowTypeIntent, Title: "Auth Feature"},
		{WorkflowType: workitem.WorkflowTypeDesign, Title: "Dashboard Design"},
	}
	m := New(context.Background(), items, "", nil, priority.NewStore(), "")
	m.width = 80
	m.height = 24
	m.ready = true

	// Enter search mode
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = result.(Model)

	// Type "auth"
	for _, r := range "auth" {
		result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = result.(Model)
	}

	// Live filter should show 1 item
	if len(m.filteredItems) != 1 {
		t.Fatalf("during search: %d items, want 1", len(m.filteredItems))
	}

	// Press Enter to commit
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = result.(Model)

	if m.searchMode {
		t.Error("should have exited search mode")
	}
	if m.searchQuery != "auth" {
		t.Errorf("searchQuery = %q, want 'auth'", m.searchQuery)
	}
	if len(m.filteredItems) != 1 {
		t.Errorf("after Enter: %d items, want 1 (filter committed)", len(m.filteredItems))
	}
}

func TestModel_SearchEscCancels(t *testing.T) {
	items := []workitem.WorkItem{
		{WorkflowType: workitem.WorkflowTypeIntent, Title: "Auth Feature"},
		{WorkflowType: workitem.WorkflowTypeDesign, Title: "Dashboard Design"},
	}
	m := New(context.Background(), items, "", nil, priority.NewStore(), "")
	m.width = 80
	m.height = 24
	m.ready = true

	// Enter search mode
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = result.(Model)

	// Type "auth" — live filter narrows to 1 item
	for _, r := range "auth" {
		result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = result.(Model)
	}
	if len(m.filteredItems) != 1 {
		t.Fatalf("during search: %d items, want 1", len(m.filteredItems))
	}

	// Press Esc to cancel — should restore original unfiltered list
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m = result.(Model)

	if m.searchMode {
		t.Error("should have exited search mode")
	}
	if m.searchQuery != "" {
		t.Errorf("searchQuery = %q, want empty (cancelled)", m.searchQuery)
	}
	if len(m.filteredItems) != 2 {
		t.Errorf("after Esc: %d items, want 2 (filter cancelled, all items restored)", len(m.filteredItems))
	}
}

func TestModel_EmptyView(t *testing.T) {
	m := New(context.Background(), nil, "", nil, priority.NewStore(), "")
	m.width = 80
	m.height = 24
	m.ready = true

	view := m.View()
	if !strings.Contains(view, "No work items") {
		t.Error("empty state should show 'No work items' message")
	}
	if !strings.Contains(view, ".campaign/intents/{inbox,active,ready}") {
		t.Error("empty state should advertise the canonical intent root")
	}
	if strings.Contains(view, "workflow/intents/{inbox,active,ready}") {
		t.Error("empty state should not advertise the legacy intent root")
	}
}

func TestModel_OpenEditorUsesModelContext(t *testing.T) {
	tempDir := t.TempDir()
	docPath := filepath.Join(tempDir, "item.md")
	if err := os.WriteFile(docPath, []byte("test"), 0o644); err != nil {
		t.Fatalf("write doc: %v", err)
	}

	editorPath := filepath.Join(tempDir, "sleep-editor.sh")
	editorScript := "#!/bin/sh\nsleep 2\n"
	if err := os.WriteFile(editorPath, []byte(editorScript), 0o755); err != nil {
		t.Fatalf("write editor script: %v", err)
	}
	t.Setenv("EDITOR", editorPath)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	item := workitem.WorkItem{PrimaryDoc: filepath.Base(docPath)}
	m := New(ctx, []workitem.WorkItem{item}, tempDir, nil, priority.NewStore(), "")

	start := time.Now()
	err := m.editorCommand(docPath).Run()
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected canceled model context to stop the editor")
	}
	if elapsed > time.Second {
		t.Fatalf("editor command ignored model context cancellation; elapsed=%v", elapsed)
	}
}

func TestModel_HelpToggle(t *testing.T) {
	m := New(context.Background(), makeTestItems(1), "", nil, priority.NewStore(), "")
	m.width = 80
	m.height = 24
	m.ready = true

	// Toggle help on
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = result.(Model)
	if !m.helpVisible {
		t.Error("help should be visible after ?")
	}

	view := m.View()
	if !strings.Contains(view, "HELP") {
		t.Error("help view should contain HELP")
	}

	// Toggle help off
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = result.(Model)
	if m.helpVisible {
		t.Error("help should be hidden after second ?")
	}
}

func TestModel_ScrollViewport_CursorBeyondVisibleHeight(t *testing.T) {
	// 20 items but only 5 visible rows — cursor must scroll into view
	items := makeTestItems(20)
	m := New(context.Background(), items, "", nil, priority.NewStore(), "")
	m.width = 80
	m.height = 8 // 8 - 3 (header/footer) = 5 visible rows
	m.ready = true

	// Move cursor down past visible area
	for i := 0; i < 10; i++ {
		result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
		m = result.(Model)
	}

	if m.cursor != 10 {
		t.Fatalf("cursor = %d, want 10", m.cursor)
	}

	// scrollOffset must have advanced so cursor is visible
	viewportHeight := m.height - 3
	if m.scrollOffset+viewportHeight <= m.cursor {
		t.Errorf("cursor %d is below visible window [%d, %d)", m.cursor, m.scrollOffset, m.scrollOffset+viewportHeight)
	}
	if m.cursor < m.scrollOffset {
		t.Errorf("cursor %d is above visible window starting at %d", m.cursor, m.scrollOffset)
	}

	// Verify the rendered view contains the selected item's title
	view := m.View()
	if !strings.Contains(view, items[10].Title) {
		t.Errorf("view should contain cursor item title %q but doesn't", items[10].Title)
	}

	// Verify going back up also scrolls
	for i := 0; i < 10; i++ {
		result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
		m = result.(Model)
	}

	if m.cursor != 0 {
		t.Fatalf("cursor after going back up = %d, want 0", m.cursor)
	}
	if m.scrollOffset != 0 {
		t.Errorf("scrollOffset after going back to top = %d, want 0", m.scrollOffset)
	}
}

func TestModel_GJumpUpdatesScroll(t *testing.T) {
	items := makeTestItems(20)
	m := New(context.Background(), items, "", nil, priority.NewStore(), "")
	m.width = 80
	m.height = 8
	m.ready = true

	// G jumps to bottom — scroll must follow
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	m = result.(Model)

	if m.cursor != 19 {
		t.Fatalf("cursor after G = %d, want 19", m.cursor)
	}
	viewportHeight := m.height - 3
	if m.scrollOffset+viewportHeight <= m.cursor {
		t.Errorf("cursor %d is not visible after G (scroll=%d, vp=%d)", m.cursor, m.scrollOffset, viewportHeight)
	}

	// gg jumps to top — scroll must follow
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m = result.(Model)
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m = result.(Model)

	if m.cursor != 0 {
		t.Fatalf("cursor after gg = %d, want 0", m.cursor)
	}
	if m.scrollOffset != 0 {
		t.Errorf("scrollOffset after gg = %d, want 0", m.scrollOffset)
	}
}

func TestModel_RefilterShrinksViewport(t *testing.T) {
	// Simulate: user scrolls down in 20 items, then refresh returns only 2 items.
	// scrollOffset must clamp so the viewport doesn't start past the end.
	items := makeTestItems(20)
	m := New(context.Background(), items, "", nil, priority.NewStore(), "")
	m.width = 80
	m.height = 8 // viewport = 5 rows
	m.ready = true

	// Scroll to bottom
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	m = result.(Model)
	if m.cursor != 19 {
		t.Fatalf("cursor = %d, want 19", m.cursor)
	}
	if m.scrollOffset == 0 {
		t.Fatal("scrollOffset should be non-zero after scrolling to bottom")
	}

	// Simulate refresh returning only 2 items
	smallItems := makeTestItems(2)
	result, _ = m.Update(refreshMsg{items: smallItems})
	m = result.(Model)

	// cursor should be clamped to last item
	if m.cursor != 1 {
		t.Errorf("cursor after shrink = %d, want 1", m.cursor)
	}
	// scrollOffset must be 0 since all items fit in viewport
	if m.scrollOffset != 0 {
		t.Errorf("scrollOffset after shrink to 2 items = %d, want 0", m.scrollOffset)
	}

	// Verify view renders without panic and shows items
	view := m.View()
	if !strings.Contains(view, smallItems[0].Title) {
		t.Error("view should contain first item after shrink")
	}
}

func TestModel_RefreshDoesNotPrunePriorityStore(t *testing.T) {
	root := t.TempDir()
	items := makeTestItems(1)
	store := priority.NewStore()
	priority.Set(store, "test:stale", priority.High)
	storePath := priority.StorePath(root)
	if err := priority.Save(storePath, store); err != nil {
		t.Fatalf("save priority store: %v", err)
	}
	before, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("read priority store before refresh: %v", err)
	}

	m := New(context.Background(), items, root, nil, store, storePath)
	result, _ := m.Update(refreshMsg{items: items})
	_ = result.(Model)

	after, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("read priority store after refresh: %v", err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("refresh pruned priority store during read")
	}
}

func TestModel_AssignPriorityPrunesStaleEntries(t *testing.T) {
	root := t.TempDir()
	items := makeTestItems(1)
	store := priority.NewStore()
	priority.Set(store, "test:stale", priority.Low)
	storePath := priority.StorePath(root)
	if err := priority.Save(storePath, store); err != nil {
		t.Fatalf("save priority store: %v", err)
	}

	loaded, err := priority.Load(storePath)
	if err != nil {
		t.Fatalf("load priority store: %v", err)
	}
	m := New(context.Background(), items, root, nil, loaded, storePath)
	result, _ := m.assignPriority(priority.High)
	_ = result.(Model)

	updated, err := priority.Load(storePath)
	if err != nil {
		t.Fatalf("load updated priority store: %v", err)
	}
	if _, ok := updated.ManualPriorities["test:stale"]; ok {
		t.Fatal("expected stale priority entry to be pruned")
	}
	entry, ok := updated.ManualPriorities[items[0].Key]
	if !ok {
		t.Fatalf("expected priority entry for %s", items[0].Key)
	}
	if entry.Priority != priority.High {
		t.Fatalf("priority = %q, want %q", entry.Priority, priority.High)
	}
}

func TestModel_TypeFilterShrinksViewport(t *testing.T) {
	// User scrolls down, then applies type filter that reduces list to 1 item.
	items := make([]workitem.WorkItem, 20)
	now := time.Now()
	for i := range items {
		items[i] = workitem.WorkItem{
			Key:           fmt.Sprintf("test:%d", i),
			WorkflowType:  workitem.WorkflowTypeDesign,
			Title:         fmt.Sprintf("Design %d", i),
			SortTimestamp: now.Add(-time.Duration(i) * time.Hour),
		}
	}
	// Add one intent
	items[0].WorkflowType = workitem.WorkflowTypeIntent
	items[0].Title = "The Intent"

	m := New(context.Background(), items, "", nil, priority.NewStore(), "")
	m.width = 80
	m.height = 8
	m.ready = true

	// Scroll to bottom
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	m = result.(Model)

	// Filter to intents only — should shrink to 1 item
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	m = result.(Model)

	if len(m.filteredItems) != 1 {
		t.Fatalf("filtered = %d, want 1", len(m.filteredItems))
	}
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0", m.cursor)
	}
	if m.scrollOffset != 0 {
		t.Errorf("scrollOffset = %d, want 0", m.scrollOffset)
	}
}

func TestFormatRecency(t *testing.T) {
	tests := []struct {
		name string
		ago  time.Duration
		want string
	}{
		{"just now", 0, "now"},
		{"minutes", 13 * time.Minute, "13m"},
		{"hours", 2 * time.Hour, "2h"},
		{"days", 3 * 24 * time.Hour, "3d"},
		{"weeks", 14 * 24 * time.Hour, "2w"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := time.Now().Add(-tt.ago)
			got := formatRecency(ts)
			if got != tt.want {
				t.Errorf("formatRecency(%v ago) = %q, want %q", tt.ago, got, tt.want)
			}
		})
	}
}

func TestFormatRecency_ZeroTime(t *testing.T) {
	got := formatRecency(time.Time{})
	if got != "  -" {
		t.Errorf("formatRecency(zero) = %q, want '  -'", got)
	}
}

func TestRenderRowShowsLifecycleForIntentAndFestival(t *testing.T) {
	items := []workitem.WorkItem{
		{
			WorkflowType:   workitem.WorkflowTypeIntent,
			LifecycleStage: workitem.LifecycleStageInbox,
			Title:          "Captured idea",
			RelativePath:   ".campaign/intents/inbox/captured.md",
			ItemKind:       workitem.ItemKindFile,
			AttentionStage: "current",
			SortTimestamp:  time.Now(),
		},
		{
			WorkflowType:   workitem.WorkflowTypeFestival,
			LifecycleStage: workitem.LifecycleStagePlanning,
			Title:          "Planning festival",
			RelativePath:   "festivals/planning/planning-festival",
			ItemKind:       workitem.ItemKindDirectory,
			AttentionStage: "current",
			SortTimestamp:  time.Now(),
		},
	}

	intentRow := renderRow(items[0], 120, false)
	if !strings.Contains(intentRow, "inbox") {
		t.Fatalf("intent row should show lifecycle inbox, got:\n%s", intentRow)
	}
	if strings.Contains(intentRow, "cur") {
		t.Fatalf("intent row should not show attention lane, got:\n%s", intentRow)
	}

	festivalRow := renderRow(items[1], 120, false)
	if !strings.Contains(festivalRow, "plan") {
		t.Fatalf("festival row should show lifecycle planning, got:\n%s", festivalRow)
	}
	if strings.Contains(festivalRow, "cur") {
		t.Fatalf("festival row should not show attention lane, got:\n%s", festivalRow)
	}
}

func TestModel_PreserveSelection_KeyFound(t *testing.T) {
	items := makeTestItems(10)
	m := New(context.Background(), items, "", nil, priority.NewStore(), "")
	m.cursor = 5
	targetKey := items[5].Key

	// Reverse the filteredItems to simulate a resort.
	reversed := make([]workitem.WorkItem, len(items))
	for i, item := range items {
		reversed[len(items)-1-i] = item
	}
	m.filteredItems = reversed
	m.preserveSelection(targetKey)

	if m.filteredItems[m.cursor].Key != targetKey {
		t.Errorf("cursor at %d points to %q, want %q", m.cursor, m.filteredItems[m.cursor].Key, targetKey)
	}
}

func TestModel_PreserveSelection_KeyNotFound(t *testing.T) {
	items := makeTestItems(5)
	m := New(context.Background(), items, "", nil, priority.NewStore(), "")
	m.cursor = 10 // intentionally out of range

	m.preserveSelection("nonexistent:key")

	if m.cursor != 4 {
		t.Errorf("cursor = %d, want 4 (clamped to last item)", m.cursor)
	}
}

func TestModel_PreserveSelection_EmptyList(t *testing.T) {
	m := New(context.Background(), nil, "", nil, priority.NewStore(), "")
	m.cursor = 5

	m.preserveSelection("some:key")

	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0 (empty list)", m.cursor)
	}
}
