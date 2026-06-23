package org

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func (m orgTUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		if m.overlay != overlayNone {
			return m.updateOverlay(msg)
		}
		return m.updateBrowse(msg)
	}
	return m, nil
}

func (m orgTUIModel) updateBrowse(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.status = ""
	switch key.String() {
	case "ctrl+c", "q":
		m.quitting = true
		return m, tea.Quit
	case "esc":
		if m.pane == paneMembers {
			m.pane = paneOrgs
			return m, nil
		}
		m.quitting = true
		return m, tea.Quit
	case "down", "j":
		m.moveDown()
		return m, nil
	case "up", "k":
		m.moveUp()
		return m, nil
	case "right", "l", "enter":
		if m.pane == paneOrgs && len(m.members) > 0 {
			m.pane = paneMembers
			m.memCursor = 0
		}
		return m, nil
	case "left", "h", "backspace":
		if m.pane == paneMembers {
			m.pane = paneOrgs
		}
		return m, nil
	case "r":
		if m.pane == paneOrgs && len(m.orgs) > 0 {
			m.openOverlay(overlayRename, "new org name")
		}
		return m, nil
	case "m":
		if m.pane == paneMembers && len(m.members) > 0 {
			m.openOverlay(overlayMove, "target org")
		}
		return m, nil
	case "d":
		if m.pane == paneMembers && len(m.members) > 0 {
			member := m.members[m.memCursor]
			if member.Status == "" || m.fallback == "" {
				return m, nil
			}
			if err := m.assignOrg(member.ID, m.fallback); err != nil {
				m.setError(err)
			} else {
				m.setStatus(fmt.Sprintf("returned %q to org %q", member.Name, m.fallback))
			}
		}
		return m, nil
	}
	return m, nil
}

func (m *orgTUIModel) moveDown() {
	if m.pane == paneOrgs {
		if len(m.orgs) == 0 {
			return
		}
		m.orgCursor = (m.orgCursor + 1) % len(m.orgs)
		m.memCursor = 0
		m.syncFocusedOrg()
		return
	}
	if len(m.members) == 0 {
		return
	}
	m.memCursor = (m.memCursor + 1) % len(m.members)
}

func (m *orgTUIModel) moveUp() {
	if m.pane == paneOrgs {
		if len(m.orgs) == 0 {
			return
		}
		m.orgCursor = (m.orgCursor - 1 + len(m.orgs)) % len(m.orgs)
		m.memCursor = 0
		m.syncFocusedOrg()
		return
	}
	if len(m.members) == 0 {
		return
	}
	m.memCursor = (m.memCursor - 1 + len(m.members)) % len(m.members)
}

func (m *orgTUIModel) openOverlay(kind orgOverlay, placeholder string) {
	m.overlay = kind
	m.input.SetValue("")
	m.input.Placeholder = placeholder
	m.input.Focus()
}

func (m orgTUIModel) updateOverlay(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "esc":
		m.overlay = overlayNone
		m.input.Blur()
		return m, nil
	case "enter":
		return m.commitOverlay()
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(key)
	return m, cmd
}

func (m orgTUIModel) commitOverlay() (tea.Model, tea.Cmd) {
	value := strings.TrimSpace(m.input.Value())
	switch m.overlay {
	case overlayRename:
		old := m.orgs[m.orgCursor].Org
		if err := m.renameOrg(old, value); err != nil {
			m.setError(err)
		} else {
			m.setStatus(fmt.Sprintf("renamed org %q to %q", old, value))
		}
	case overlayMove:
		member := m.members[m.memCursor]
		if err := m.assignOrg(member.ID, value); err != nil {
			m.setError(err)
		} else {
			m.setStatus(fmt.Sprintf("moved %q to org %q", member.Name, value))
		}
	}
	m.overlay = overlayNone
	m.input.Blur()
	return m, nil
}
