package explorer

import (
	"github.com/obediencecorp/camp/internal/intent/tui"
)

// moveCursorDown moves the cursor down through groups and items.
func (m *Model) moveCursorDown() {
	if len(m.groups) == 0 {
		return
	}

	group := &m.groups[m.cursorGroup]

	if m.cursorItem == -1 {
		// On group header
		if group.Expanded && len(group.Intents) > 0 {
			// Move to first item in group
			m.cursorItem = 0
		} else {
			// Move to next group header
			m.moveToNextGroup()
		}
	} else {
		// On an item
		if m.cursorItem < len(group.Intents)-1 {
			// Move to next item in group
			m.cursorItem++
		} else {
			// Move to next group header
			m.moveToNextGroup()
		}
	}

	m.ensureCursorVisible()
}

// moveCursorUp moves the cursor up through groups and items.
func (m *Model) moveCursorUp() {
	if len(m.groups) == 0 {
		return
	}

	switch m.cursorItem {
	case -1:
		// On group header, move to previous group's last item
		if m.cursorGroup > 0 {
			m.cursorGroup--
			prevGroup := &m.groups[m.cursorGroup]
			if prevGroup.Expanded && len(prevGroup.Intents) > 0 {
				m.cursorItem = len(prevGroup.Intents) - 1
			} else {
				m.cursorItem = -1
			}
		}
	case 0:
		// On first item, move to group header
		m.cursorItem = -1
	default:
		// Move up within group
		m.cursorItem--
	}

	m.ensureCursorVisible()
}

// cursorVisualLine returns the 0-indexed visual line of the current cursor position,
// accounting for group headers and collapsed groups.
func (m *Model) cursorVisualLine() int {
	line := 0
	for gi, group := range m.groups {
		if gi == m.cursorGroup && m.cursorItem == -1 {
			return line
		}
		line++ // group header

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
	if m.listHeight <= 0 {
		return
	}

	line := m.cursorVisualLine()

	// Cursor above visible area - scroll up
	if line < m.scrollOffset {
		m.scrollOffset = line
		return
	}

	// Cursor below visible area - scroll down
	if line >= m.scrollOffset+m.listHeight {
		m.scrollOffset = line - m.listHeight + 1
	}
}

// moveToNextGroup moves cursor to the next group header.
func (m *Model) moveToNextGroup() {
	if m.cursorGroup < len(m.groups)-1 {
		m.cursorGroup++
		m.cursorItem = -1
	}
}

// handleSelect handles Enter/Space key - toggle group or open viewer on item.
func (m *Model) handleSelect() {
	if len(m.groups) == 0 {
		return
	}

	if m.cursorItem == -1 {
		// On group header, toggle expansion
		m.groups[m.cursorGroup].Expanded = !m.groups[m.cursorGroup].Expanded
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
