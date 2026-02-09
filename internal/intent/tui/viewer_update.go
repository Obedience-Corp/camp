package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/obediencecorp/camp/internal/intent"
)

// Update implements tea.Model.
func (m IntentViewerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	// Handle search mode
	if m.searchMode {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "esc":
				m.searchMode = false
				m.searchInput.Blur()
				m.searchInput.SetValue("")
				m.searchQuery = ""
				m.searchMatches = nil
				m.searchMatchIdx = -1
				// Re-render content without highlights
				m.renderContent()
				return m, nil
			case "enter":
				m.searchMode = false
				m.searchInput.Blur()
				// Keep query active for n/N navigation
				m.searchQuery = m.searchInput.Value()
				m.findSearchMatches()
				if len(m.searchMatches) > 0 {
					m.scrollToMatch()
				}
				// Re-render with highlights
				m.viewport.SetContent(m.applySearchHighlight(m.content))
				return m, nil
			default:
				m.searchInput, cmd = m.searchInput.Update(msg)
				// Live update matches
				m.searchQuery = m.searchInput.Value()
				m.findSearchMatches()
				if len(m.searchMatches) > 0 {
					m.scrollToMatch()
				}
				// Re-render with highlights
				m.viewport.SetContent(m.applySearchHighlight(m.content))
				return m, cmd
			}
		}
		return m, nil
	}

	// Handle confirmation dialog
	if m.showConfirm {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			m.confirmDialog.HandleKey(msg.String())
			if m.confirmDialog.IsDone() {
				m.showConfirm = false
				if m.confirmDialog.Confirmed() {
					switch m.pendingAction {
					case "delete":
						return m, m.deleteIntent()
					case "archive":
						return m, m.archiveIntent()
					}
				}
				m.pendingAction = ""
			}
		}
		return m, nil
	}

	// Handle move overlay
	if m.moveOverlay {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "esc":
				m.moveOverlay = false
				return m, nil
			case "j", "down":
				if m.moveStatusIdx < len(moveStatusOptions)-1 {
					m.moveStatusIdx++
				}
			case "k", "up":
				if m.moveStatusIdx > 0 {
					m.moveStatusIdx--
				}
			case "enter":
				newStatus := moveStatusOptions[m.moveStatusIdx].status
				if m.intent.Status != newStatus {
					m.moveOverlay = false
					return m, m.moveIntent(newStatus)
				}
				m.moveOverlay = false
			}
		}
		return m, nil
	}

	// Handle gather-similar overlay
	if m.gatherOverlay {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "esc":
				m.gatherOverlay = false
				m.selectedSimilar = make(map[string]bool)
				return m, nil
			case "j", "down":
				if len(m.similarIntents) > 0 && m.gatherCursorIdx < len(m.similarIntents)-1 {
					m.gatherCursorIdx++
				}
			case "k", "up":
				if m.gatherCursorIdx > 0 {
					m.gatherCursorIdx--
				}
			case " ":
				// Toggle selection
				if len(m.similarIntents) > 0 {
					id := m.similarIntents[m.gatherCursorIdx].Intent.ID
					if m.selectedSimilar[id] {
						delete(m.selectedSimilar, id)
					} else {
						m.selectedSimilar[id] = true
					}
				}
			case "enter":
				// Proceed to title input if any selected
				if len(m.selectedSimilar) > 0 {
					m.gatherOverlay = false
					m.showGatherTitle = true
					// Collect intents for dialog: current + selected similar
					intents := []*intent.Intent{m.intent}
					for _, sim := range m.similarIntents {
						if m.selectedSimilar[sim.Intent.ID] {
							intents = append(intents, sim.Intent)
						}
					}
					m.gatherDialog = NewGatherDialog(intents)
				}
			}
		}
		return m, nil
	}

	// Handle gather title input dialog
	if m.showGatherTitle {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			var cmd tea.Cmd
			m.gatherDialog, cmd = m.gatherDialog.Update(msg)
			if m.gatherDialog.Done() {
				if m.gatherDialog.Cancelled() {
					m.showGatherTitle = false
					m.gatherOverlay = true // Go back to selection
				} else {
					m.showGatherTitle = false
					return m, m.executeViewerGather()
				}
			}
			return m, cmd
		}
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.updateViewerKeys(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.Width = msg.Width - 4
		m.viewport.Height = msg.Height - 8
		m.renderContent()

	case ViewerEditorFinishedMsg:
		m.refreshOnReturn = true
		m.loadContent() // Reload content after edit
		return m, nil

	case ViewerMoveFinishedMsg:
		if msg.Err == nil {
			m.intent.Status = msg.NewStatus
			m.refreshOnReturn = true
		}
		return m, nil

	case ViewerArchiveFinishedMsg:
		if msg.Err == nil {
			m.refreshOnReturn = true
			return m, m.closeViewer()
		}
		return m, nil

	case ViewerDeleteFinishedMsg:
		if msg.Err == nil {
			m.refreshOnReturn = true
			return m, m.closeViewer()
		}
		return m, nil

	case ViewerSimilarFoundMsg:
		if msg.Err != nil {
			// Could show error message, for now just ignore
			return m, nil
		}
		// Show overlay even if empty so user sees "No similar intents found"
		m.similarIntents = msg.Similar
		m.gatherOverlay = true
		m.gatherCursorIdx = 0
		m.selectedSimilar = make(map[string]bool)
		return m, nil

	case ViewerGatherFinishedMsg:
		if msg.Err != nil {
			// Could show error message, for now close overlay
			m.showGatherTitle = false
			m.gatherOverlay = false
			m.selectedSimilar = make(map[string]bool)
			return m, nil
		}
		// Gather succeeded - close viewer and refresh
		m.refreshOnReturn = true
		return m, m.closeViewer()
	}

	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

// updateViewerKeys handles keyboard input in the viewer.
func (m IntentViewerModel) updateViewerKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Handle pending "g" key (for gg)
	if m.pendingKey == "g" {
		m.pendingKey = ""
		if key == "g" {
			m.viewport.GotoTop()
			return m, nil
		}
		// Not "g" — fall through to normal handling
	}

	switch key {
	// Exit keys
	case "q", "esc", "backspace":
		// If search is active, clear it first
		if m.searchQuery != "" {
			m.searchQuery = ""
			m.searchMatches = nil
			m.searchMatchIdx = -1
			m.searchInput.SetValue("")
			m.renderContent()
			return m, nil
		}
		return m, m.closeViewer()

	// Search
	case "/":
		m.searchMode = true
		m.searchInput.Focus()
		return m, textinput.Blink
	case "n":
		// Next search match
		if m.searchQuery != "" {
			m.nextMatch()
		}
		return m, nil
	case "N":
		// Previous search match
		if m.searchQuery != "" {
			m.prevMatch()
		}
		return m, nil

	// Vim scrolling
	case "j", "down":
		m.viewport.ScrollDown(1)
	case "k", "up":
		m.viewport.ScrollUp(1)
	case "ctrl+d":
		m.viewport.HalfPageDown()
	case "ctrl+u":
		m.viewport.HalfPageUp()
	case "ctrl+f", "pgdown":
		m.viewport.PageDown()
	case "ctrl+b", "pgup":
		m.viewport.PageUp()
	case "home":
		m.viewport.GotoTop()
	case "G", "end":
		m.viewport.GotoBottom()
	case "g":
		// Set pending key — wait for second "g" to form "gg"
		m.pendingKey = "g"
		return m, nil
	case "ctrl+g":
		// Gather-similar: find similar intents and show selection overlay
		if m.gatherSvc != nil {
			return m, m.findSimilarIntents()
		}
	case "H":
		// Jump to screen top (already visible)
		m.viewport.GotoTop()
	case "M":
		// Jump to screen middle
		m.viewport.SetYOffset(m.viewport.YOffset + m.viewport.Height/2)
	case "L":
		// Jump to screen bottom
		lines := strings.Count(m.content, "\n")
		targetLine := m.viewport.YOffset + m.viewport.Height - 1
		targetLine = min(targetLine, lines)
		m.viewport.SetYOffset(targetLine - m.viewport.Height + 1)

	// Sibling navigation (cycle through intents in same status group)
	case "left", "h":
		if len(m.siblings) > 1 {
			m.navigatePrev()
		}
		return m, nil
	case "right", "l":
		if len(m.siblings) > 1 {
			m.navigateNext()
		}
		return m, nil

	// Actions
	case "e":
		return m, m.openInEditor()
	case "m":
		m.moveOverlay = true
		m.moveStatusIdx = 0
		return m, nil
	case "p":
		// Promote to next status
		nextStatus := getNextStatus(m.intent.Status)
		if nextStatus != m.intent.Status {
			return m, m.moveIntent(nextStatus)
		}
	case "a":
		// Archive - requires confirmation
		if m.intent.Status != intent.StatusKilled {
			m.showConfirm = true
			m.pendingAction = "archive"
			m.confirmDialog = NewConfirmationDialog(
				"Archive Intent",
				fmt.Sprintf("Archive '%s'?\n\nIt will be moved to killed status.", m.intent.Title),
			)
		}
		return m, nil
	case "d":
		// Delete - requires confirmation
		m.showConfirm = true
		m.pendingAction = "delete"
		m.confirmDialog = NewConfirmationDialog(
			"Delete Intent",
			fmt.Sprintf("Delete '%s'?\n\nThis cannot be undone.", m.intent.Title),
		)
		return m, nil
	case "o":
		return m, m.openWithSystem()
	case "O":
		return m, m.revealInFileManager()
	}

	return m, nil
}
