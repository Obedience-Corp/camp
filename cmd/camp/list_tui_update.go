package main

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func (m listTUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if msg.Width > 0 {
			m.input.Width = max(msg.Width-8, 4)
		}
		return m, nil
	case remoteLoadedMsg:
		return m.applyRemoteLoaded(msg)
	case tea.KeyMsg:
		if m.overlay != listOverlayNone {
			return m.updateOverlay(msg)
		}
		return m.updateBrowse(msg)
	}
	return m, nil
}

func (m listTUIModel) applyRemoteLoaded(msg remoteLoadedMsg) (tea.Model, tea.Cmd) {
	m.remoteLoading = false
	if msg.err != nil {
		m.setStatus("remote load failed: "+msg.err.Error(), true)
		return m, nil
	}
	// Drop any prior remote rows, then append the fresh fan-out.
	locals := make([]campaignEntry, 0, m.localCount)
	for _, e := range m.all {
		if !isRemoteListEntry(e) {
			locals = append(locals, e)
		}
	}
	m.all = append(locals, msg.rows...)
	m.remoteOn = true
	m.rebuildVisible()

	var unreach []string
	for _, r := range msg.results {
		if r.err != nil {
			unreach = append(unreach, r.machineID)
		}
	}
	if len(unreach) > 0 {
		m.setStatus(fmt.Sprintf("loaded remotes (%d unreachable: %s)", len(unreach), strings.Join(unreach, ", ")), false)
	} else {
		m.setStatus(fmt.Sprintf("loaded %d remote campaign(s)", len(msg.rows)), false)
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
		m.gotoPath = gotoSelectionFor(m.visible[m.cursor])
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
			if isRemoteListEntry(m.visible[m.cursor]) {
				m.setStatus("remote campaigns are read-only here", true)
				return m, nil
			}
			if err := m.cycleStatus(); err != nil {
				m.setError(err)
			}
		}
		return m, nil
	case "m":
		if len(m.visible) > 0 {
			if isRemoteListEntry(m.visible[m.cursor]) {
				m.setStatus("remote campaigns are read-only here", true)
				return m, nil
			}
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
	case "r":
		if !m.machinesConfigured {
			m.setStatus("no machines configured (~/.obey/machines.yaml)", true)
			return m, nil
		}
		if m.remoteLoading {
			return m, nil
		}
		if m.remoteOn {
			// Toggle off: strip remote rows.
			locals := make([]campaignEntry, 0, m.localCount)
			for _, e := range m.all {
				if !isRemoteListEntry(e) {
					locals = append(locals, e)
				}
			}
			m.all = locals
			m.remoteOn = false
			m.rebuildVisible()
			m.setStatus("showing local campaigns", false)
			return m, nil
		}
		m.remoteLoading = true
		m.setStatus("loading remotes…", false)
		return m, loadRemoteCampaignsCmd(m.ctx, m.remoteListFilter())
	}
	return m, nil
}

// loadRemoteCampaignsCmd runs the shared fan-out off the UI thread so the
// "loading remotes…" status line can paint (required; never block Update).
func loadRemoteCampaignsCmd(ctx context.Context, filter listFilter) tea.Cmd {
	return func() tea.Msg {
		rows, results, err := loadRemoteCampaigns(ctx, filter)
		return remoteLoadedMsg{rows: rows, results: results, err: err}
	}
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
			if isRemoteListEntry(e) {
				m.setStatus("remote campaigns are read-only here", true)
			} else if err := m.assignOrg(e.ID, e.Name, value); err != nil {
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
