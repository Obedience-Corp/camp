package explorer

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Obedience-Corp/camp/internal/intent"
)

// TestSearchEnterHandoff_PlacesCursorOnFirstFilteredItem is a regression
// test for #279: after typing a search query and pressing Enter, the
// cursor used to remain at cursorItem=-1 (a group header) so j/k looked
// like a no-op and users could not pick from the filtered results.
//
// After the fix, pressing Enter in search mode:
//   - blurs the search input and returns focus to focusList (existing behavior)
//   - positions the cursor on the first item of the first non-empty group
//   - expands that group so the cursor is actually visible
func TestSearchEnterHandoff_PlacesCursorOnFirstFilteredItem(t *testing.T) {
	ctx := context.Background()
	m := NewModel(ctx, nil, nil, "/tmp/intents", "/tmp/campaign", "test-id", "", nil)
	m.ready = true

	now := time.Now()
	m.intents = []*intent.Intent{
		{ID: "alpha", Title: "Alpha foo", Status: intent.StatusInbox, Type: intent.TypeFeature, CreatedAt: now},
		{ID: "beta", Title: "Beta foo", Status: intent.StatusReady, Type: intent.TypeFeature, CreatedAt: now},
		{ID: "gamma", Title: "Gamma unrelated", Status: intent.StatusInbox, Type: intent.TypeFeature, CreatedAt: now},
	}
	m.filteredIntents = m.intents
	m.groups = groupIntentsByStatus(m.intents, m.dungeonExpanded)

	// Simulate the post-live-filter state: user typed "foo", applyFilters
	// narrowed to two intents, cursor parked at (0, -1) which is the
	// regression baseline that made the list look unselectable.
	m.focus = focusSearch
	m.searchInput.SetValue("foo")
	m.filteredIntents = []*intent.Intent{m.intents[0], m.intents[1]}
	m.groups = groupIntentsByStatus(m.filteredIntents, m.dungeonExpanded)
	m.cursorGroup = 0
	m.cursorItem = -1
	m.scrollOffset = 0

	// Sanity: applyFilters parks the cursor on a header (the behavior the user hit).
	if m.cursorItem != -1 {
		t.Fatalf("baseline: cursorItem=-1 (regression baseline), got %d", m.cursorItem)
	}

	// Press Enter to exit search and hand focus to the list.
	model, cmd := m.updateSearch(tea.KeyMsg{Type: tea.KeyEnter})
	got := model.(Model)

	if cmd != nil {
		t.Fatal("search Enter should not dispatch a tea.Cmd")
	}
	if got.focus != focusList {
		t.Fatalf("expected focusList after enter, got %v", got.focus)
	}
	if got.searchInput.Focused() {
		t.Fatal("expected searchInput to be blurred after enter")
	}
	if got.cursorItem != 0 {
		t.Fatalf("expected cursor on first item (cursorItem=0), got %d", got.cursorItem)
	}
	if !got.groups[got.cursorGroup].Expanded {
		t.Fatal("the group holding the cursor should be expanded so the cursor is visible")
	}

	// The first non-empty group must hold one of the matching intents.
	pointedAt := got.groups[got.cursorGroup].Intents[got.cursorItem]
	if !strings.Contains(strings.ToLower(pointedAt.Title), "foo") {
		t.Fatalf("cursor should point at a filtered intent (title containing 'foo'), got %q", pointedAt.Title)
	}
}

// TestSearchEnterHandoff_NoMatchesSurfacesHint covers the case where the
// search query filters everything out. The cursor must not crash on an
// empty group set and the user must get a hint about how to recover.
func TestSearchEnterHandoff_NoMatchesSurfacesHint(t *testing.T) {
	ctx := context.Background()
	m := NewModel(ctx, nil, nil, "/tmp/intents", "/tmp/campaign", "test-id", "", nil)
	m.ready = true

	m.intents = []*intent.Intent{
		{ID: "alpha", Title: "Alpha", Status: intent.StatusInbox, Type: intent.TypeFeature, CreatedAt: time.Now()},
	}
	m.filteredIntents = m.intents
	m.groups = groupIntentsByStatus(m.intents, m.dungeonExpanded)

	// Simulate the empty-result state: user typed a query that filtered
	// everything out, applyFilters left filteredIntents empty, groups
	// regrouped accordingly, cursor parked at (0, -1).
	m.focus = focusSearch
	m.searchInput.SetValue("nomatch-zzz")
	m.filteredIntents = nil
	m.groups = groupIntentsByStatus(nil, m.dungeonExpanded)
	m.cursorGroup = 0
	m.cursorItem = -1

	model, _ := m.updateSearch(tea.KeyMsg{Type: tea.KeyEnter})
	got := model.(Model)

	if got.focus != focusList {
		t.Fatalf("focus should be focusList even with no matches, got %v", got.focus)
	}
	if !strings.Contains(strings.ToLower(got.statusMessage), "no matches") {
		t.Fatalf("expected a 'No matches' hint in statusMessage, got %q", got.statusMessage)
	}
}

// TestSearchEnterHandoff_DungeonOnlyMatchesAutoExpand is a regression test
// for the obey-agent review on PR #282: when the Dungeon parent is
// collapsed and the active filter has matches only in dungeon child groups,
// the cursor placement must still find the matches. Without the auto-expand
// path the helper iterated m.groups looking for non-empty Intents — but the
// Dungeon parent has only DungeonCount and an empty Intents slice while
// collapsed, and the dungeon child groups are absent from m.groups
// entirely. The user would see "No matches" even though their query hit a
// Someday item.
func TestSearchEnterHandoff_DungeonOnlyMatchesAutoExpand(t *testing.T) {
	ctx := context.Background()
	m := NewModel(ctx, nil, nil, "/tmp/intents", "/tmp/campaign", "test-id", "", nil)
	m.ready = true

	now := time.Now()
	somedayMatch := &intent.Intent{
		ID:        "someday-foo",
		Title:     "Foo someday item",
		Status:    intent.StatusSomeday,
		Type:      intent.TypeFeature,
		CreatedAt: now,
	}
	m.intents = []*intent.Intent{
		{ID: "inbox-1", Title: "Inbox unrelated", Status: intent.StatusInbox, Type: intent.TypeFeature, CreatedAt: now},
		somedayMatch,
	}
	// Dungeon starts collapsed (the default the user filed the bug under).
	m.dungeonExpanded = false

	// Simulate the post-live-filter state: user typed a query that matches
	// only the Someday item. filteredIntents holds only the dungeon match;
	// groups was rebuilt with dungeonExpanded=false so the Someday group is
	// not present in m.groups — only a Dungeon parent with DungeonCount=1.
	m.focus = focusSearch
	m.searchInput.SetValue("foo")
	m.filteredIntents = []*intent.Intent{somedayMatch}
	m.groups = groupIntentsByStatus(m.filteredIntents, m.dungeonExpanded)
	m.cursorGroup = 0
	m.cursorItem = -1
	m.scrollOffset = 0

	// Verify the baseline state matches the bug condition: a Dungeon parent
	// exists with a non-zero count, but the matching child is not in groups.
	dungeonParentFound := false
	someDayChildPresent := false
	for _, g := range m.groups {
		if g.IsDungeonParent {
			dungeonParentFound = true
			if g.DungeonCount != 1 {
				t.Fatalf("baseline: dungeon parent should report 1 match, got %d", g.DungeonCount)
			}
		}
		if g.IsDungeonChild && g.Status == intent.StatusSomeday {
			someDayChildPresent = true
		}
	}
	if !dungeonParentFound {
		t.Fatal("baseline: dungeon parent should be in m.groups")
	}
	if someDayChildPresent {
		t.Fatal("baseline: someday child should NOT be in m.groups before fix triggers (collapsed parent)")
	}

	// Press Enter to commit the filter and hand focus to the list.
	model, _ := m.updateSearch(tea.KeyMsg{Type: tea.KeyEnter})
	got := model.(Model)

	if got.focus != focusList {
		t.Fatalf("focus should be focusList, got %v", got.focus)
	}
	if !got.dungeonExpanded {
		t.Fatal("dungeon parent should have been auto-expanded to expose the matching child")
	}
	if got.cursorItem != 0 {
		t.Fatalf("cursor should point at the first matching item (cursorItem=0), got %d", got.cursorItem)
	}
	if !got.groups[got.cursorGroup].IsDungeonChild {
		t.Fatalf("cursor should land on a dungeon child group, got group %+v", got.groups[got.cursorGroup])
	}
	pointedAt := got.groups[got.cursorGroup].Intents[got.cursorItem]
	if pointedAt.ID != somedayMatch.ID {
		t.Fatalf("cursor should point at the matching Someday intent, got %q", pointedAt.ID)
	}
	if strings.Contains(strings.ToLower(got.statusMessage), "no matches") {
		t.Fatalf("must not show no-matches hint when dungeon matches exist; got %q", got.statusMessage)
	}
}

// TestPlaceCursorAtFirstItem_NoGroupsHasItems exercises the helper directly
// with an explicit empty-groups state to confirm it falls back safely.
func TestPlaceCursorAtFirstItem_NoGroupsHasItems(t *testing.T) {
	ctx := context.Background()
	m := NewModel(ctx, nil, nil, "/tmp/intents", "/tmp/campaign", "test-id", "", nil)
	m.ready = true
	m.intents = nil
	m.filteredIntents = nil
	m.groups = groupIntentsByStatus(nil, m.dungeonExpanded)

	placed := m.placeCursorAtFirstItem()

	if placed {
		t.Fatal("expected false when no groups have items")
	}
	if m.cursorGroup != 0 || m.cursorItem != -1 {
		t.Fatalf("expected safe fallback (cursorGroup=0, cursorItem=-1); got (%d, %d)",
			m.cursorGroup, m.cursorItem)
	}
}
