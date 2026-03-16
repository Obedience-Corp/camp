package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Obedience-Corp/camp/internal/workitem"
)

func makeTestItems(n int) []workitem.WorkItem {
	items := make([]workitem.WorkItem, n)
	now := time.Now()
	for i := range items {
		items[i] = workitem.WorkItem{
			Key:            "test:" + string(rune('a'+i)),
			WorkflowType:   workitem.WorkflowTypeDesign,
			Title:          "Item " + string(rune('A'+i)),
			RelativePath:   "workflow/design/item-" + string(rune('a'+i)),
			ItemKind:       workitem.ItemKindDirectory,
			SortTimestamp:  now.Add(-time.Duration(i) * time.Hour),
			CreatedAt:      now.Add(-time.Duration(i) * time.Hour),
		}
	}
	return items
}

func TestModel_CursorDown(t *testing.T) {
	items := makeTestItems(5)
	m := New(context.Background(), items, "", nil)

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = result.(Model)
	if m.cursor != 1 {
		t.Errorf("cursor after j = %d, want 1", m.cursor)
	}
}

func TestModel_CursorUp(t *testing.T) {
	items := makeTestItems(5)
	m := New(context.Background(), items, "", nil)
	m.cursor = 3

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = result.(Model)
	if m.cursor != 2 {
		t.Errorf("cursor after k = %d, want 2", m.cursor)
	}
}

func TestModel_CursorDoesNotGoBelowZero(t *testing.T) {
	items := makeTestItems(5)
	m := New(context.Background(), items, "", nil)
	m.cursor = 0

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = result.(Model)
	if m.cursor != 0 {
		t.Errorf("cursor should not go below 0, got %d", m.cursor)
	}
}

func TestModel_GJumpsToBottom(t *testing.T) {
	items := makeTestItems(5)
	m := New(context.Background(), items, "", nil)

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	m = result.(Model)
	if m.cursor != 4 {
		t.Errorf("cursor after G = %d, want 4", m.cursor)
	}
}

func TestModel_GGJumpsToTop(t *testing.T) {
	items := makeTestItems(5)
	m := New(context.Background(), items, "", nil)
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
	m := New(context.Background(), items, "", nil)

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

func TestModel_EnterSelectsItem(t *testing.T) {
	items := makeTestItems(3)
	m := New(context.Background(), items, "", nil)
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
	m := New(context.Background(), items, "", nil)
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
	m := New(context.Background(), items, "", nil)
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
	m := New(context.Background(), nil, "", nil)
	m.width = 80
	m.height = 24
	m.ready = true

	view := m.View()
	if !strings.Contains(view, "No work items") {
		t.Error("empty state should show 'No work items' message")
	}
}

func TestModel_HelpToggle(t *testing.T) {
	m := New(context.Background(), makeTestItems(1), "", nil)
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
	m := New(context.Background(), items, "", nil)
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
	m := New(context.Background(), items, "", nil)
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
	m := New(context.Background(), items, "", nil)
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

	m := New(context.Background(), items, "", nil)
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
