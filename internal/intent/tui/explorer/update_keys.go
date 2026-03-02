package explorer

import (
	"github.com/Obedience-Corp/camp/internal/intent/tui"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// updateNormal handles navigation mode keys.
func (m Model) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Accumulate digit count prefix (vim-style: 5j, 12gg, etc.)
	if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
		m.countBuffer = m.countBuffer*10 + int(key[0]-'0')
		return m, nil
	}
	if len(key) == 1 && key[0] == '0' && m.countBuffer > 0 {
		m.countBuffer = m.countBuffer * 10
		return m, nil
	}

	// Retrieve and reset count (defaults to 1)
	count := m.countBuffer
	if count == 0 {
		count = 1
	}
	m.countBuffer = 0

	// Handle pending "g" key (for gg / Ngg / ga)
	if m.pendingKey == "g" {
		m.pendingKey = ""
		switch key {
		case "g":
			if count > 1 {
				m.jumpToVisualLine(count - 1) // Ngg: 1-indexed → 0-indexed
			} else {
				m.jumpToTop()
			}
			m.updatePreviewForSelection()
			return m, nil
		case "a":
			// ga: gather intents
			if m.cursorItem == -1 && len(m.selectedIntents) == 0 {
				return m.handleGatherGroup()
			}
			return m.handleGatherStart()
		}
		// Unrecognized — fall through to normal handling
	}

	switch key {
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "?":
		// Toggle help overlay
		m.showHelp = !m.showHelp
		if m.showHelp {
			m.helpOverlay = tui.NewHelpOverlay(m.width-10, m.height-6)
		}
		return m, nil
	case "/":
		// Enter search mode
		m.focus = focusSearch
		m.searchInput.Focus()
		return m, textinput.Blink
	case "tab":
		// Tab navigates to filter bar when not on preview
		if !m.showPreview || !m.previewFocused {
			m.focus = focusFilterBar
			m.filterBar.Focus()
			return m, nil
		}
		// Otherwise toggle preview focus
		m.previewFocused = !m.previewFocused
		return m, nil
	case "c":
		// Enter concept filter mode
		m.focus = focusConceptFilter
		m.conceptFilterPicker = tui.NewConceptPickerModel(m.ctx, m.conceptSvc)
		return m, nil
	case "C":
		// Clear concept filter
		m.conceptFilterPath = ""
		m.applyFilters()
		return m, nil
	case "n":
		// Start new intent creation
		m.focus = focusCreating
		m.creationStep = stepTitle
		m.titleInput.SetValue("")
		m.titleInput.Focus()
		m.createTypeIdx = 0
		return m, textinput.Blink
	case "e":
		// Open selected intent in $EDITOR
		if selected := m.SelectedIntent(); selected != nil {
			return m, openInEditor(selected.Path)
		}
	case "o":
		// Open selected intent with system default handler
		if selected := m.SelectedIntent(); selected != nil {
			return m, openWithSystem(selected.Path)
		}
	case "O":
		// Reveal in file manager (macOS Finder)
		if selected := m.SelectedIntent(); selected != nil {
			return m, revealInFileManager(selected.Path)
		}
	case "m":
		// Start move action to change intent status
		if selected := m.SelectedIntent(); selected != nil {
			m.focus = focusMove
			m.intentToMove = selected
			m.moveStatusIdx = 0
		}
		return m, nil
	case "p":
		// Promote to next status in workflow
		return m.handlePromoteAction()
	case "a":
		// Archive (move to dungeon) - requires confirmation
		return m.handleArchiveAction()
	case "d":
		// Delete intent (permanently) - requires confirmation
		return m.handleDeleteAction()
	case "f":
		// Open full-screen viewer for selected intent
		if selected := m.SelectedIntent(); selected != nil {
			group := m.groups[m.cursorGroup]
			m.focus = focusViewer
			m.viewer = tui.NewIntentViewerModelWithGather(
				m.ctx, selected,
				group.Intents, m.cursorItem,
				m.service, m.gatherSvc, m.width, m.height,
			)
		}
		return m, nil
	case ".":
		// Open action menu for selected intent
		if selected := m.SelectedIntent(); selected != nil {
			m.focus = focusActionMenu
			m.actionMenu = tui.NewActionMenu(selected)
		}
		return m, nil
	case "v":
		// Toggle preview pane visibility
		m.showPreview = !m.showPreview
		m.recalculateLayout()
		// Load preview content for currently selected intent
		if m.shouldShowPreview() {
			if selected := m.SelectedIntent(); selected != nil {
				m.loadPreviewContent(selected)
			}
		}
		return m, nil
	case "j", "down":
		if m.previewFocused && m.showPreview {
			var cmd tea.Cmd
			m.previewPane, cmd = m.previewPane.Update(msg)
			return m, cmd
		}
		m.moveCursorDownN(count)
		m.updatePreviewForSelection()
	case "k", "up":
		if m.previewFocused && m.showPreview {
			var cmd tea.Cmd
			m.previewPane, cmd = m.previewPane.Update(msg)
			return m, cmd
		}
		m.moveCursorUpN(count)
		m.updatePreviewForSelection()
	case "ctrl+d":
		if m.previewFocused && m.showPreview {
			var cmd tea.Cmd
			m.previewPane, cmd = m.previewPane.Update(msg)
			return m, cmd
		}
		halfPage := max(m.listHeight/2, 1)
		m.moveCursorDownN(halfPage)
		m.updatePreviewForSelection()
	case "ctrl+u":
		if m.previewFocused && m.showPreview {
			var cmd tea.Cmd
			m.previewPane, cmd = m.previewPane.Update(msg)
			return m, cmd
		}
		halfPage := max(m.listHeight/2, 1)
		m.moveCursorUpN(halfPage)
		m.updatePreviewForSelection()
	case "g":
		if m.previewFocused && m.showPreview {
			var cmd tea.Cmd
			m.previewPane, cmd = m.previewPane.Update(msg)
			return m, cmd
		}
		// Set pending key — wait for second "g" to form "gg"
		// Count is saved in countBuffer (already reset above, but the
		// pending-g handler reads count which was captured before reset)
		m.pendingKey = "g"
		// Restore count so the pending handler can use it
		if count > 1 {
			m.countBuffer = count
		}
		return m, nil
	case "G":
		if m.previewFocused && m.showPreview {
			var cmd tea.Cmd
			m.previewPane, cmd = m.previewPane.Update(msg)
			return m, cmd
		}
		if count > 1 {
			m.jumpToVisualLine(count - 1) // NG: jump to line N
		} else {
			m.jumpToBottom()
		}
		m.updatePreviewForSelection()
	case "enter":
		m.handleSelect()
	case " ":
		// Space toggles selection for multi-select gather
		if selected := m.SelectedIntent(); selected != nil {
			m.toggleSelection(selected)
		} else if m.cursorItem == -1 {
			// On group header, toggle group expansion
			m.handleSelect()
		}
	case "esc":
		// Clear selections and exit multi-select mode, or clear filters
		if m.multiSelectMode {
			m.exitMultiSelectMode()
			return m, nil
		}
		// Clear active filters
		if m.hasActiveFilters() {
			m.clearAllFilters()
			return m, nil
		}
	}

	return m, nil
}
