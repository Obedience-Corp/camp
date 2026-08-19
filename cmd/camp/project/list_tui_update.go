package project

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func (m projListModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if msg.Width > 0 {
			m.input.Width = max(msg.Width-8, 4)
		}
		return m, nil
	case tea.KeyMsg:
		if m.overlay != projOverlayNone {
			return m.updateSearch(msg)
		}
		return m.updateBrowse(msg)
	}
	return m, nil
}

func (m projListModel) updateBrowse(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.status = ""
	switch key.String() {
	case "ctrl+c", "q", "esc":
		m.quitting = true
		return m, tea.Quit
	case "g", "enter":
		return m.goSelected()
	case "down", "j":
		m.move(1)
		return m, nil
	case "up", "k":
		m.move(-1)
		return m, nil
	case "y":
		return m.copySelected()
	case "f":
		m.groupBy = m.groupBy.next()
		m.rebuildVisible()
		m.setStatus("grouped by "+m.groupBy.label(), false)
		return m, nil
	case "/":
		m.overlay = projOverlaySearch
		m.input.SetValue(m.query)
		m.input.Focus()
		return m, nil
	}
	return m, nil
}

func (m projListModel) updateSearch(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "esc":
		m.overlay = projOverlayNone
		m.input.Blur()
		m.query = ""
		m.rebuildVisible()
		return m, nil
	case "enter", "g":
		m.overlay = projOverlayNone
		m.input.Blur()
		m.query = strings.TrimSpace(m.input.Value())
		m.rebuildVisible()
		return m.goSelected()
	case "down", "j":
		m.move(1)
		return m, nil
	case "up", "k":
		m.move(-1)
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(key)
	m.query = strings.TrimSpace(m.input.Value())
	m.rebuildVisible()
	return m, cmd
}

func (m *projListModel) move(delta int) {
	if len(m.visible) == 0 {
		return
	}
	n := len(m.visible)
	m.cursor = (m.cursor + delta + n) % n
}

func (m projListModel) goSelected() (tea.Model, tea.Cmd) {
	if len(m.visible) == 0 {
		return m, nil
	}
	if !m.gotoEnabled {
		m.setStatus(m.goNeedsShellHint(), true)
		return m, nil
	}
	m.gotoPath = m.visible[m.cursor].AbsPath
	m.quitting = true
	return m, tea.Quit
}

func (m projListModel) copySelected() (tea.Model, tea.Cmd) {
	if len(m.visible) == 0 {
		return m, nil
	}
	if err := m.copyPath(); err != nil {
		m.setStatus("copy failed: "+err.Error(), true)
	} else {
		m.setStatus("copied!", false)
	}
	return m, nil
}
