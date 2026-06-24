package main

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func (m listTUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		if m.overlay != listOverlayNone {
			return m.updateOverlay(msg)
		}
		return m.updateBrowse(msg)
	}
	return m, nil
}

func (m listTUIModel) updateBrowse(key tea.KeyMsg) (tea.Model, tea.Cmd) {
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
	case "s":
		if len(m.visible) > 0 {
			if err := m.cycleStatus(); err != nil {
				m.setError(err)
			}
		}
		return m, nil
	case "m":
		if len(m.visible) > 0 {
			m.overlay = listOverlayMove
			m.input.SetValue("")
			m.input.Placeholder = "target org"
			m.input.Focus()
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
		m.activeOnly = !m.activeOnly
		m.rebuildVisible()
		return m, nil
	}
	return m, nil
}

func (m listTUIModel) updateOverlay(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "esc":
		m.overlay = listOverlayNone
		m.input.Blur()
		return m, nil
	case "enter":
		value := strings.TrimSpace(m.input.Value())
		if value != "" && len(m.visible) > 0 {
			e := m.visible[m.cursor]
			if err := m.assignOrg(e.ID, e.Name, value); err != nil {
				m.setError(err)
			}
		}
		m.overlay = listOverlayNone
		m.input.Blur()
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(key)
	return m, cmd
}
