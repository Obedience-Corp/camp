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
			m.viewer = tui.NewIntentViewerModel(
				m.ctx, selected,
				group.Intents, m.cursorItem,
				m.service, m.width, m.height,
			)
		}
	}
}
