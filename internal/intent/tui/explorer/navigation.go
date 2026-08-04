package explorer

import (
	"github.com/Obedience-Corp/camp/internal/intent/tui"
)

// placeCursorAtFirstItem positions the cursor on the first item of the first
// group that has at least one intent, expanding the group if needed. It
// returns true when an item was reachable and the cursor was placed there,
// false when the visible groups are all empty (e.g. no search matches).
//
// Used after the user exits search mode so the filtered list has a visible
// selection rather than landing on a group header (cursorItem=-1). Without
// this, j/k looked like a no-op and users could not tell the list was
// navigable (regression #279).
//
// Special case: when a nest parent (e.g. Dungeon) is collapsed, its child
// groups are still in m.groups but hidden via isGroupVisible. If the active
// filter has matches only under that parent, auto-expand it so the first
// matching child becomes reachable.
func (m *Model) placeCursorAtFirstItem() bool {
	for gi := range m.groups {
		if !m.isGroupVisible(gi) {
			continue
		}
		// Auto-expand nest parents that hold descendant matches so their
		// children become visible on subsequent iterations of this loop.
		if m.groups[gi].IsNestParent() && m.groups[gi].DescendantCount > 0 && !m.groups[gi].Expanded {
			m.groups[gi].Expanded = true
			continue
		}
		if len(m.groups[gi].Intents) == 0 {
			continue
		}
		// Expand the group so the chosen item is actually visible. Without
		// this a collapsed first-non-empty group would still hide the cursor.
		m.groups[gi].Expanded = true
		m.cursorGroup = gi
		m.cursorItem = 0
		m.scrollOffset = 0
		m.ensureCursorVisible()
		return true
	}
	// No matches reachable — leave cursor at the safe header position.
	m.cursorGroup = 0
	m.cursorItem = -1
	m.scrollOffset = 0
	return false
}

// moveCursorDown moves the cursor down one position and adjusts scroll.
func (m *Model) moveCursorDown() {
	m.moveCursorDownOne()
	m.ensureCursorVisible()
}

// moveCursorUp moves the cursor up one position and adjusts scroll.
func (m *Model) moveCursorUp() {
	m.moveCursorUpOne()
	m.ensureCursorVisible()
}

// cursorVisualLine returns the 0-indexed visual line of the current cursor position,
// accounting for group headers, collapsed groups, and hidden nested subtrees.
func (m *Model) cursorVisualLine() int {
	line := 0
	for gi, group := range m.groups {
		if !m.isGroupVisible(gi) {
			continue
		}
		if gi == m.cursorGroup && m.cursorItem == -1 {
			return line
		}
		line++ // group header

		// Nest parents have no direct intents; their children render separately.
		if group.IsNestParent() {
			continue
		}

		if group.Expanded {
			for ii := range group.Intents {
				if gi == m.cursorGroup && ii == m.cursorItem {
					return line
				}
				line++
			}
		}
	}
	return line
}

// ensureCursorVisible adjusts scrollOffset so the cursor is within the visible window.
func (m *Model) ensureCursorVisible() {
	listHeight := m.listHeight
	if listHeight <= 0 {
		// Fallback: estimate from terminal height if recalculateLayout
		// hasn't run yet (e.g., before first WindowSizeMsg).
		listHeight = max(m.height-8, 3)
	}
	if listHeight <= 0 {
		return
	}

	line := m.cursorVisualLine()

	// Cursor above visible area - scroll up
	if line < m.scrollOffset {
		m.scrollOffset = line
		return
	}

	// Cursor below visible area - scroll down
	if line >= m.scrollOffset+listHeight {
		m.scrollOffset = line - listHeight + 1
	}
}

// jumpToTop moves the cursor to the first group header and resets scroll.
func (m *Model) jumpToTop() {
	if len(m.groups) == 0 {
		return
	}
	m.cursorGroup = 0
	m.cursorItem = -1
	m.scrollOffset = 0
}

// jumpToBottom moves the cursor to the last visible item.
func (m *Model) jumpToBottom() {
	if len(m.groups) == 0 {
		return
	}
	// Find last visible group, and if expanded, go to its last item
	last := -1
	for gi := range m.groups {
		if m.isGroupVisible(gi) {
			last = gi
		}
	}
	if last < 0 {
		return
	}
	m.cursorGroup = last
	group := &m.groups[m.cursorGroup]
	if !group.IsNestParent() && group.Expanded && len(group.Intents) > 0 {
		m.cursorItem = len(group.Intents) - 1
	} else {
		m.cursorItem = -1
	}
	m.ensureCursorVisible()
}

// moveCursorDownN moves the cursor down n positions.
func (m *Model) moveCursorDownN(n int) {
	for range n {
		prev := m.cursorVisualLine()
		m.moveCursorDownOne()
		if m.cursorVisualLine() == prev {
			break // hit bottom
		}
	}
	m.ensureCursorVisible()
}

// moveCursorUpN moves the cursor up n positions.
func (m *Model) moveCursorUpN(n int) {
	for range n {
		prev := m.cursorVisualLine()
		m.moveCursorUpOne()
		if m.cursorVisualLine() == prev {
			break // hit top
		}
	}
	m.ensureCursorVisible()
}

// moveCursorDownOne moves the cursor down one position without scroll adjustment.
func (m *Model) moveCursorDownOne() {
	if len(m.groups) == 0 {
		return
	}
	group := &m.groups[m.cursorGroup]
	if m.cursorItem == -1 {
		// Nest parents have no direct intents; always move to next visible group.
		if !group.IsNestParent() && group.Expanded && len(group.Intents) > 0 {
			m.cursorItem = 0
		} else {
			m.moveToNextGroup()
		}
	} else {
		if m.cursorItem < len(group.Intents)-1 {
			m.cursorItem++
		} else {
			m.moveToNextGroup()
		}
	}
}

// moveCursorUpOne moves the cursor up one position without scroll adjustment.
func (m *Model) moveCursorUpOne() {
	if len(m.groups) == 0 {
		return
	}
	switch m.cursorItem {
	case -1:
		prev := m.prevVisibleGroup(m.cursorGroup)
		if prev < 0 {
			return
		}
		m.cursorGroup = prev
		prevGroup := &m.groups[m.cursorGroup]
		if !prevGroup.IsNestParent() && prevGroup.Expanded && len(prevGroup.Intents) > 0 {
			m.cursorItem = len(prevGroup.Intents) - 1
		} else {
			m.cursorItem = -1
		}
	case 0:
		m.cursorItem = -1
	default:
		m.cursorItem--
	}
}

// moveToNextGroup moves cursor to the next visible group header.
func (m *Model) moveToNextGroup() {
	next := m.nextVisibleGroup(m.cursorGroup)
	if next < 0 {
		return
	}
	m.cursorGroup = next
	m.cursorItem = -1
}

// nextVisibleGroup returns the next visible group index after from, or -1.
func (m *Model) nextVisibleGroup(from int) int {
	for gi := from + 1; gi < len(m.groups); gi++ {
		if m.isGroupVisible(gi) {
			return gi
		}
	}
	return -1
}

// prevVisibleGroup returns the previous visible group index before from, or -1.
func (m *Model) prevVisibleGroup(from int) int {
	for gi := from - 1; gi >= 0; gi-- {
		if m.isGroupVisible(gi) {
			return gi
		}
	}
	return -1
}

// jumpToVisualLine moves cursor to visual line n (0-indexed).
// Used by Ngg to jump to a specific line number.
func (m *Model) jumpToVisualLine(targetLine int) {
	line := 0
	for gi, group := range m.groups {
		if !m.isGroupVisible(gi) {
			continue
		}
		if line == targetLine {
			m.cursorGroup = gi
			m.cursorItem = -1
			m.ensureCursorVisible()
			return
		}
		line++
		if group.IsNestParent() {
			continue
		}
		if group.Expanded {
			for ii := range group.Intents {
				if line == targetLine {
					m.cursorGroup = gi
					m.cursorItem = ii
					m.ensureCursorVisible()
					return
				}
				line++
			}
		}
	}
	// Past the end — jump to bottom
	m.jumpToBottom()
}

// handleSelect handles Enter/Space key - toggle group or open viewer on item.
func (m *Model) handleSelect() {
	if len(m.groups) == 0 {
		return
	}

	if m.cursorItem == -1 {
		group := &m.groups[m.cursorGroup]
		// Nest parents (Dungeon today; Notes folders later) toggle Expanded
		// in place — children stay in m.groups and visibility follows Expanded.
		group.Expanded = !group.Expanded
		m.ensureCursorVisible()
	} else {
		// On intent item - open full-screen viewer directly
		if selected := m.SelectedIntent(); selected != nil {
			group := m.groups[m.cursorGroup]
			m.focus = focusViewer
			m.viewer = tui.NewIntentViewerModelWithGather(
				m.ctx, selected,
				group.Intents, m.cursorItem,
				m.service, m.gatherSvc, m.width, m.height,
			)
		}
	}
}
