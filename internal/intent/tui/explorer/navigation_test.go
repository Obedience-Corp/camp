package explorer

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/obediencecorp/camp/internal/intent"
)

// makeTestModel creates a Model with test intents across groups.
// inbox: n intents, active: n intents, ready/done/killed: 0
func makeTestModel(inboxCount, activeCount int) Model {
	ctx := context.Background()
	m := NewModel(ctx, nil, nil, "/tmp/intents", "/tmp/campaign", "test-id")
	m.ready = true
	m.width = 120
	m.height = 30

	var intents []*intent.Intent
	for i := range inboxCount {
		intents = append(intents, &intent.Intent{
			ID:        fmt.Sprintf("inbox-%d", i),
			Title:     fmt.Sprintf("Inbox Intent %d", i),
			Status:    intent.StatusInbox,
			Type:      intent.TypeFeature,
			CreatedAt: time.Now(),
		})
	}
	for i := range activeCount {
		intents = append(intents, &intent.Intent{
			ID:        fmt.Sprintf("active-%d", i),
			Title:     fmt.Sprintf("Active Intent %d", i),
			Status:    intent.StatusActive,
			Type:      intent.TypeFeature,
			CreatedAt: time.Now(),
		})
	}

	m.intents = intents
	m.filteredIntents = intents
	m.groups = groupIntentsByStatus(intents)
	m.listHeight = 20 // simulate reasonable terminal

	return m
}

func TestCursorVisualLine(t *testing.T) {
	m := makeTestModel(3, 2)

	tests := []struct {
		name        string
		cursorGroup int
		cursorItem  int
		wantLine    int
	}{
		{"first group header", 0, -1, 0},
		{"first group first item", 0, 0, 1},
		{"first group second item", 0, 1, 2},
		{"first group third item", 0, 2, 3},
		{"second group header", 1, -1, 4},
		{"second group first item", 1, 0, 5},
		{"second group second item", 1, 1, 6},
		// Groups 2-4 (Ready, Done, Killed) are collapsed, so just headers
		{"third group header", 2, -1, 7},
		{"fourth group header", 3, -1, 8},
		{"fifth group header", 4, -1, 9},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.cursorGroup = tt.cursorGroup
			m.cursorItem = tt.cursorItem
			got := m.cursorVisualLine()
			if got != tt.wantLine {
				t.Errorf("cursorVisualLine() = %d, want %d", got, tt.wantLine)
			}
		})
	}
}

func TestCursorVisualLine_CollapsedGroup(t *testing.T) {
	m := makeTestModel(3, 2)
	// Collapse inbox group
	m.groups[0].Expanded = false

	// With inbox collapsed: header(0) | active-header(1) | active-0(2) | active-1(3)
	m.cursorGroup = 1
	m.cursorItem = -1
	got := m.cursorVisualLine()
	if got != 1 {
		t.Errorf("cursorVisualLine() with collapsed group = %d, want 1", got)
	}

	m.cursorItem = 0
	got = m.cursorVisualLine()
	if got != 2 {
		t.Errorf("cursorVisualLine() first active item = %d, want 2", got)
	}
}

func TestTotalVisualLines(t *testing.T) {
	m := makeTestModel(3, 2)
	// 5 group headers + 3 inbox items + 2 active items = 10
	got := m.totalVisualLines()
	if got != 10 {
		t.Errorf("totalVisualLines() = %d, want 10", got)
	}
}

func TestTotalVisualLines_AllCollapsed(t *testing.T) {
	m := makeTestModel(3, 2)
	for i := range m.groups {
		m.groups[i].Expanded = false
	}
	// 5 group headers only
	got := m.totalVisualLines()
	if got != 5 {
		t.Errorf("totalVisualLines() all collapsed = %d, want 5", got)
	}
}

func TestMoveCursorDown(t *testing.T) {
	m := makeTestModel(2, 1)
	// Start at inbox header (group 0, item -1)
	m.cursorGroup = 0
	m.cursorItem = -1

	// Down -> first inbox item
	m.moveCursorDown()
	if m.cursorGroup != 0 || m.cursorItem != 0 {
		t.Errorf("After first down: group=%d item=%d, want 0,0", m.cursorGroup, m.cursorItem)
	}

	// Down -> second inbox item
	m.moveCursorDown()
	if m.cursorGroup != 0 || m.cursorItem != 1 {
		t.Errorf("After second down: group=%d item=%d, want 0,1", m.cursorGroup, m.cursorItem)
	}

	// Down -> active group header
	m.moveCursorDown()
	if m.cursorGroup != 1 || m.cursorItem != -1 {
		t.Errorf("After third down: group=%d item=%d, want 1,-1", m.cursorGroup, m.cursorItem)
	}

	// Down -> first active item
	m.moveCursorDown()
	if m.cursorGroup != 1 || m.cursorItem != 0 {
		t.Errorf("After fourth down: group=%d item=%d, want 1,0", m.cursorGroup, m.cursorItem)
	}
}

func TestMoveCursorUp(t *testing.T) {
	m := makeTestModel(2, 1)
	// Start at active item 0
	m.cursorGroup = 1
	m.cursorItem = 0

	// Up -> active header
	m.moveCursorUp()
	if m.cursorGroup != 1 || m.cursorItem != -1 {
		t.Errorf("After first up: group=%d item=%d, want 1,-1", m.cursorGroup, m.cursorItem)
	}

	// Up -> last inbox item
	m.moveCursorUp()
	if m.cursorGroup != 0 || m.cursorItem != 1 {
		t.Errorf("After second up: group=%d item=%d, want 0,1", m.cursorGroup, m.cursorItem)
	}

	// Up -> first inbox item
	m.moveCursorUp()
	if m.cursorGroup != 0 || m.cursorItem != 0 {
		t.Errorf("After third up: group=%d item=%d, want 0,0", m.cursorGroup, m.cursorItem)
	}

	// Up -> inbox header
	m.moveCursorUp()
	if m.cursorGroup != 0 || m.cursorItem != -1 {
		t.Errorf("After fourth up: group=%d item=%d, want 0,-1", m.cursorGroup, m.cursorItem)
	}

	// Up at top -> stays at top
	m.moveCursorUp()
	if m.cursorGroup != 0 || m.cursorItem != -1 {
		t.Errorf("Up at top should stay: group=%d item=%d, want 0,-1", m.cursorGroup, m.cursorItem)
	}
}

func TestMoveCursorDown_SkipsCollapsedGroup(t *testing.T) {
	m := makeTestModel(2, 2)
	// Collapse inbox
	m.groups[0].Expanded = false

	m.cursorGroup = 0
	m.cursorItem = -1

	// Down should skip to active header (not into collapsed inbox items)
	m.moveCursorDown()
	if m.cursorGroup != 1 || m.cursorItem != -1 {
		t.Errorf("After down from collapsed group: group=%d item=%d, want 1,-1", m.cursorGroup, m.cursorItem)
	}
}

func TestJumpToTop(t *testing.T) {
	m := makeTestModel(5, 3)
	m.cursorGroup = 1
	m.cursorItem = 2
	m.scrollOffset = 5

	m.jumpToTop()

	if m.cursorGroup != 0 || m.cursorItem != -1 {
		t.Errorf("jumpToTop: group=%d item=%d, want 0,-1", m.cursorGroup, m.cursorItem)
	}
	if m.scrollOffset != 0 {
		t.Errorf("jumpToTop: scrollOffset=%d, want 0", m.scrollOffset)
	}
}

func TestJumpToBottom(t *testing.T) {
	m := makeTestModel(3, 2)

	m.jumpToBottom()

	// Last group is Killed (index 4), collapsed, no items -> header
	if m.cursorGroup != 4 || m.cursorItem != -1 {
		t.Errorf("jumpToBottom: group=%d item=%d, want 4,-1", m.cursorGroup, m.cursorItem)
	}
}

func TestJumpToBottom_LastGroupExpanded(t *testing.T) {
	m := makeTestModel(3, 2)
	// Expand the last group and add an item
	m.groups[4].Expanded = true
	m.groups[4].Intents = []*intent.Intent{
		{ID: "killed-0", Title: "Killed Intent", Status: intent.StatusKilled, CreatedAt: time.Now()},
	}

	m.jumpToBottom()

	if m.cursorGroup != 4 || m.cursorItem != 0 {
		t.Errorf("jumpToBottom expanded: group=%d item=%d, want 4,0", m.cursorGroup, m.cursorItem)
	}
}

func TestMoveCursorDownN(t *testing.T) {
	m := makeTestModel(10, 0)
	m.cursorGroup = 0
	m.cursorItem = -1

	// Move down 5 positions: header -> item0 -> item1 -> item2 -> item3 -> item4
	m.moveCursorDownN(5)

	if m.cursorGroup != 0 || m.cursorItem != 4 {
		t.Errorf("moveCursorDownN(5): group=%d item=%d, want 0,4", m.cursorGroup, m.cursorItem)
	}
}

func TestMoveCursorUpN(t *testing.T) {
	m := makeTestModel(10, 0)
	m.cursorGroup = 0
	m.cursorItem = 9

	// Move up 5 positions
	m.moveCursorUpN(5)

	if m.cursorGroup != 0 || m.cursorItem != 4 {
		t.Errorf("moveCursorUpN(5): group=%d item=%d, want 0,4", m.cursorGroup, m.cursorItem)
	}
}

func TestMoveCursorDownN_StopsAtBottom(t *testing.T) {
	m := makeTestModel(3, 0)
	m.cursorGroup = 0
	m.cursorItem = -1

	// Move down 100 positions (more than exist)
	m.moveCursorDownN(100)

	// Should stop at last group header (killed, index 4)
	if m.cursorGroup != 4 || m.cursorItem != -1 {
		t.Errorf("moveCursorDownN(100): group=%d item=%d, want 4,-1", m.cursorGroup, m.cursorItem)
	}
}

func TestMoveCursorUpN_StopsAtTop(t *testing.T) {
	m := makeTestModel(3, 0)
	m.cursorGroup = 4
	m.cursorItem = -1

	// Move up 100 positions (more than exist)
	m.moveCursorUpN(100)

	if m.cursorGroup != 0 || m.cursorItem != -1 {
		t.Errorf("moveCursorUpN(100): group=%d item=%d, want 0,-1", m.cursorGroup, m.cursorItem)
	}
}

func TestEnsureCursorVisible_ScrollsDown(t *testing.T) {
	m := makeTestModel(20, 0)
	m.listHeight = 5
	m.scrollOffset = 0

	// Place cursor at visual line 10 (item 9)
	m.cursorGroup = 0
	m.cursorItem = 9

	m.ensureCursorVisible()

	// Cursor is at line 10 (0-indexed), viewport is 5 lines
	// scrollOffset should be at least 10 - 5 + 1 = 6
	if m.scrollOffset < 6 {
		t.Errorf("ensureCursorVisible scrollDown: scrollOffset=%d, want >= 6", m.scrollOffset)
	}
}

func TestEnsureCursorVisible_ScrollsUp(t *testing.T) {
	m := makeTestModel(20, 0)
	m.listHeight = 5
	m.scrollOffset = 10

	// Place cursor at visual line 2 (item 1)
	m.cursorGroup = 0
	m.cursorItem = 1

	m.ensureCursorVisible()

	// Cursor is at line 2, scrollOffset should be <= 2
	if m.scrollOffset != 2 {
		t.Errorf("ensureCursorVisible scrollUp: scrollOffset=%d, want 2", m.scrollOffset)
	}
}

func TestEnsureCursorVisible_NoChangeWhenVisible(t *testing.T) {
	m := makeTestModel(20, 0)
	m.listHeight = 10
	m.scrollOffset = 0

	// Place cursor at line 5 - within visible range [0, 10)
	m.cursorGroup = 0
	m.cursorItem = 4

	m.ensureCursorVisible()

	if m.scrollOffset != 0 {
		t.Errorf("ensureCursorVisible no change: scrollOffset=%d, want 0", m.scrollOffset)
	}
}

func TestEnsureCursorVisible_ZeroListHeight(t *testing.T) {
	m := makeTestModel(5, 0)
	m.listHeight = 0
	m.scrollOffset = 0
	m.cursorGroup = 0
	m.cursorItem = 3

	// Should not panic or change anything
	m.ensureCursorVisible()
	if m.scrollOffset != 0 {
		t.Errorf("ensureCursorVisible zero height: scrollOffset=%d, want 0", m.scrollOffset)
	}
}

func TestMoveCursorDown_EmptyGroups(t *testing.T) {
	m := makeTestModel(0, 0)
	m.cursorGroup = 0
	m.cursorItem = -1

	// Should not panic
	m.moveCursorDown()
	m.moveCursorUp()
}

func TestBuildMainView_DynamicHeight(t *testing.T) {
	m := makeTestModel(5, 3)
	m.width = 100
	m.height = 25

	view := m.buildMainView()
	lines := strings.Split(view, "\n")

	if len(lines) != m.height {
		t.Errorf("buildMainView output = %d lines, want exactly %d", len(lines), m.height)
	}
}

func TestBuildMainView_HeightWithSearch(t *testing.T) {
	m := makeTestModel(5, 3)
	m.width = 100
	m.height = 25
	m.focus = focusSearch
	m.searchInput.SetValue("test")

	view := m.buildMainView()
	lines := strings.Split(view, "\n")

	// Should still be exactly m.height lines, even with search visible
	if len(lines) != m.height {
		t.Errorf("buildMainView with search = %d lines, want %d", len(lines), m.height)
	}
}

func TestBuildMainView_HeightWithActiveFilters(t *testing.T) {
	m := makeTestModel(5, 3)
	m.width = 100
	m.height = 25
	m.conceptFilterPath = "projects/camp"

	view := m.buildMainView()
	lines := strings.Split(view, "\n")

	if len(lines) != m.height {
		t.Errorf("buildMainView with active filters = %d lines, want %d", len(lines), m.height)
	}
}

func TestBuildMainView_SmallTerminal(t *testing.T) {
	m := makeTestModel(5, 3)
	m.width = 60
	m.height = 10

	view := m.buildMainView()
	lines := strings.Split(view, "\n")

	if len(lines) != m.height {
		t.Errorf("buildMainView small terminal = %d lines, want %d", len(lines), m.height)
	}
}

func TestBuildMainView_FewIntents(t *testing.T) {
	m := makeTestModel(1, 0)
	m.width = 100
	m.height = 30

	view := m.buildMainView()
	lines := strings.Split(view, "\n")

	// Should pad to exactly m.height - no blank space pushing header
	if len(lines) != m.height {
		t.Errorf("buildMainView few intents = %d lines, want %d", len(lines), m.height)
	}
}

func TestBuildMainView_ScrollOffset_Clamped(t *testing.T) {
	// Need more intents than list height to trigger scrolling
	m := makeTestModel(40, 0)
	m.width = 100
	m.height = 15
	m.listHeight = max(m.height-8, 3) // simulate recalculateLayout
	m.scrollOffset = 999              // way past end

	// ensureCursorVisible is the actual clamping mechanism (called in Update)
	m.ensureCursorVisible()

	if m.scrollOffset == 999 {
		t.Error("scrollOffset was not clamped by ensureCursorVisible")
	}

	// Verify the view still renders at the correct height
	view := m.buildMainView()
	lines := strings.Split(view, "\n")
	if len(lines) != m.height {
		t.Errorf("buildMainView clamped scroll = %d lines, want %d", len(lines), m.height)
	}
}

func TestBuildMainView_ContainsScrollIndicator(t *testing.T) {
	m := makeTestModel(30, 0)
	m.width = 100
	m.height = 15

	view := m.buildMainView()

	// With 30 inbox items + 5 group headers = 35 lines, viewport ~10
	// Should show scroll indicator
	if !strings.Contains(view, "[") || !strings.Contains(view, "%]") {
		t.Error("buildMainView should contain scroll percentage indicator")
	}
}

func TestBuildMainView_NoScrollIndicator_WhenFits(t *testing.T) {
	m := makeTestModel(1, 0)
	m.width = 100
	m.height = 30

	view := m.buildMainView()

	// With 1 intent + 5 headers = 6 lines, viewport ~25 - should not scroll
	if strings.Contains(view, "%]") {
		t.Error("buildMainView should not show scroll indicator when content fits")
	}
}
