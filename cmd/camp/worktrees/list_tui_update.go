package worktrees

import (
	tea "github.com/charmbracelet/bubbletea"
)

func (m wtListModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		return m.updateBrowse(msg)
	}
	return m, nil
}

func (m wtListModel) updateBrowse(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.status = ""
	switch key.String() {
	case "ctrl+c", "q", "esc":
		m.quitting = true
		return m, tea.Quit
	case "g", "enter":
		if len(m.visible) == 0 {
			return m, nil
		}
		if !m.gotoEnabled {
			m.setStatus("go needs shell integration: run eval \"$(camp shell-init <shell>)\"", true)
			return m, nil
		}
		m.gotoPath = m.visible[m.cursor].Path
		m.quitting = true
		return m, tea.Quit
	case "down", "j":
		if len(m.visible) > 0 {
			m.cursor = (m.cursor + 1) % len(m.visible)
		}
		return m, nil
	case "up", "k":
		if len(m.visible) > 0 {
			m.cursor = (m.cursor - 1 + len(m.visible)) % len(m.visible)
		}
		return m, nil
	case "y":
		if len(m.visible) > 0 {
			if err := m.copyPath(); err != nil {
				m.setStatus("copy failed: "+err.Error(), true)
			} else {
				m.setStatus("copied!", false)
			}
		}
		return m, nil
	case "f":
		m.staleOnly = !m.staleOnly
		m.rebuildVisible()
		return m, nil
	}
	return m, nil
}
