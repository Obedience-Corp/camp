package explorer

import (
	"fmt"

	"github.com/Obedience-Corp/camp/internal/intent/tui"
	tea "github.com/charmbracelet/bubbletea"
)

// updateSearch handles keys when search input has focus.
func (m Model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg.String() {
	case "esc":
		m.focus = focusList
		m.searchInput.Blur()
		// Clear search and show all intents
		m.searchInput.SetValue("")
		m.applyFilters()
		return m, nil
	case "enter":
		// Exit search mode but keep filter active
		m.focus = focusList
		m.searchInput.Blur()
		return m, nil
	}
	// Pass all other keys to the text input
	m.searchInput, cmd = m.searchInput.Update(msg)
	// Live update: apply search on every keystroke
	m.applyFilters()
	return m, cmd
}

// updateFilterBar handles keys when filter bar has focus.
func (m Model) updateFilterBar(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg.String() {
	case "esc":
		// If dropdown is open, close it; otherwise exit filter bar
		if m.filterBar.HasOpenDropdown() {
			m.filterBar, cmd = m.filterBar.Update(msg)
			return m, cmd
		}
		m.focus = focusList
		m.filterBar.Blur()
		return m, nil
	}

	// Pass to filter bar
	m.filterBar, cmd = m.filterBar.Update(msg)
	return m, cmd
}

// updateConceptFilter handles concept filter picker.
func (m Model) updateConceptFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.conceptFilterPicker, cmd = m.conceptFilterPicker.Update(msg)
	if m.conceptFilterPicker.Done() {
		m.focus = focusList
		if !m.conceptFilterPicker.Cancelled() {
			m.conceptFilterPath = m.conceptFilterPicker.SelectedPath()
		}
		m.applyFilters()
		return m, nil
	}
	return m, cmd
}

// updateConfirm handles confirmation dialog.
func (m Model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.confirmDialog.HandleKey(msg.String())
	if m.confirmDialog.IsDone() {
		m.focus = focusList
		if m.confirmDialog.Confirmed() {
			switch m.pendingAction {
			case "delete":
				if m.pendingIntent != nil {
					cmd := m.deleteIntent(m.pendingIntent)
					m.pendingAction = ""
					m.pendingIntent = nil
					return m, cmd
				}
			case "archive":
				if m.pendingIntent != nil {
					cmd := m.archiveIntent(m.pendingIntent)
					m.pendingAction = ""
					m.pendingIntent = nil
					return m, cmd
				}
			case "promote":
				if m.pendingIntent != nil {
					cmd := m.promoteToFestival(m.pendingIntent)
					m.pendingAction = ""
					m.pendingIntent = nil
					return m, cmd
				}
			case "promote-ready":
				if m.pendingIntent != nil {
					cmd := m.promoteToReady(m.pendingIntent)
					m.pendingAction = ""
					m.pendingIntent = nil
					return m, cmd
				}
			case "gather":
				cmd := m.executeGather()
				m.pendingAction = ""
				m.pendingIntent = nil
				return m, cmd
			}
		}
		// Reset pending state on cancel
		m.pendingAction = ""
		m.pendingIntent = nil
	}
	return m, nil
}

// updateGatherDialog handles gather dialog.
func (m Model) updateGatherDialog(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.gatherDialog, cmd = m.gatherDialog.Update(msg)
	if m.gatherDialog.Done() {
		if !m.gatherDialog.Cancelled() {
			if m.gatherDialog.ArchiveSources() {
				// Destructive action — require confirmation
				m.focus = focusConfirm
				m.pendingAction = "gather"
				count := len(m.gatherDialog.Intents())
				m.confirmDialog = tui.NewConfirmationDialog(
					"Confirm Gather",
					fmt.Sprintf("Gather %d intents into %q?\n\nSource intents will be archived (moved to killed).",
						count, m.gatherDialog.Title()),
				)
				return m, nil
			}
			// Non-destructive — execute immediately
			m.focus = focusList
			return m, m.executeGather()
		}
		m.focus = focusList
	}
	return m, cmd
}

// updateViewer handles viewer - pass all keys to it.
func (m Model) updateViewer(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var viewerModel tea.Model
	var cmd tea.Cmd
	viewerModel, cmd = m.viewer.Update(msg)
	m.viewer = viewerModel.(tui.IntentViewerModel)
	return m, cmd
}
