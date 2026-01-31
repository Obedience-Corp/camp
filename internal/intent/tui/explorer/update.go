package explorer

import (
	"fmt"
	"os"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/obediencecorp/camp/internal/intent"
	"github.com/obediencecorp/camp/internal/intent/tui"
	"github.com/obediencecorp/camp/internal/intent/tui/filterchip"
)

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.focus == focusSearch {
			return m.updateSearch(msg)
		}

		if m.focus == focusFilterBar {
			return m.updateFilterBar(msg)
		}

		if m.focus == focusConceptFilter {
			return m.updateConceptFilter(msg)
		}

		if m.focus == focusCreating {
			return m.updateCreating(msg)
		}

		if m.focus == focusMove {
			return m.updateMove(msg)
		}

		if m.focus == focusConfirm {
			return m.updateConfirm(msg)
		}

		if m.focus == focusActionMenu {
			m.actionMenu, cmd = m.actionMenu.Update(msg)
			return m, cmd
		}

		if m.focus == focusGatherDialog {
			return m.updateGatherDialog(msg)
		}

		if m.focus == focusViewer {
			return m.updateViewer(msg)
		}

		// Handle help overlay (highest priority modal)
		if m.showHelp {
			var closed bool
			m.helpOverlay, cmd, closed = m.helpOverlay.Update(msg)
			if closed {
				m.showHelp = false
			}
			return m, cmd
		}

		// Normal navigation mode
		return m.updateNormal(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.searchInput.Width = m.width - 20
		if m.searchInput.Width < 20 {
			m.searchInput.Width = 20
		}
		m.recalculateLayout()
		m.ready = true

	case intentsLoadedMsg:
		if msg.err != nil {
			m.statusMessage = "Error: " + msg.err.Error()
			return m, nil
		}
		m.intents = msg.intents
		m.filteredIntents = msg.intents
		m.groups = groupIntentsByStatus(msg.intents)

	case editorFinishedMsg:
		if msg.err != nil {
			m.statusMessage = "Editor error: " + msg.err.Error()
		} else {
			m.statusMessage = "Edit complete"
		}
		// Refresh intent list to pick up changes
		return m, m.loadIntents()

	case openFinishedMsg:
		if msg.err != nil {
			m.statusMessage = "Open failed: " + msg.err.Error()
		}
		return m, nil

	case moveFinishedMsg:
		if msg.err != nil {
			m.statusMessage = "Move failed: " + msg.err.Error()
		} else {
			m.statusMessage = fmt.Sprintf("Moved to %s", msg.newStatus)
		}
		m.intentToMove = nil
		return m, m.loadIntents()

	case archiveFinishedMsg:
		if msg.err != nil {
			if os.IsPermission(msg.err) {
				m.statusMessage = "Permission denied: cannot archive file"
			} else if os.IsNotExist(msg.err) {
				m.statusMessage = "File no longer exists"
			} else {
				m.statusMessage = "Archive failed: " + msg.err.Error()
			}
		} else {
			m.statusMessage = "Archived"
		}
		return m, m.loadIntents()

	case deleteFinishedMsg:
		if msg.err != nil {
			if os.IsPermission(msg.err) {
				m.statusMessage = "Permission denied: cannot delete file"
			} else if os.IsNotExist(msg.err) {
				m.statusMessage = "File already deleted"
			} else {
				m.statusMessage = "Delete failed: " + msg.err.Error()
			}
		} else {
			m.statusMessage = "Deleted: " + msg.title
		}
		return m, m.loadIntents()

	// Action menu messages
	case tui.ActionMenuSelectedMsg:
		return m.handleActionMenuSelection(msg)

	case tui.ActionMenuCancelledMsg:
		m.focus = focusList
		return m, nil

	// Viewer messages
	case tui.ViewerClosedMsg:
		m.focus = focusList
		// Sync cursor to the intent that was being viewed when closing
		m.cursorItem = msg.FinalIndex
		if msg.Refresh {
			return m, m.loadIntents()
		}
		return m, nil

	case tui.ViewerEditorFinishedMsg:
		if m.focus == focusViewer {
			var viewerModel tea.Model
			viewerModel, cmd = m.viewer.Update(msg)
			m.viewer = viewerModel.(tui.IntentViewerModel)
			return m, cmd
		}
		return m, nil

	case tui.ViewerMoveFinishedMsg:
		if m.focus == focusViewer {
			var viewerModel tea.Model
			viewerModel, cmd = m.viewer.Update(msg)
			m.viewer = viewerModel.(tui.IntentViewerModel)
			return m, cmd
		}
		return m, nil

	case tui.ViewerArchiveFinishedMsg:
		if m.focus == focusViewer {
			var viewerModel tea.Model
			viewerModel, cmd = m.viewer.Update(msg)
			m.viewer = viewerModel.(tui.IntentViewerModel)
			return m, cmd
		}
		return m, nil

	case tui.ViewerDeleteFinishedMsg:
		if m.focus == focusViewer {
			var viewerModel tea.Model
			viewerModel, cmd = m.viewer.Update(msg)
			m.viewer = viewerModel.(tui.IntentViewerModel)
			return m, cmd
		}
		return m, nil

	case tui.ViewerSimilarFoundMsg:
		if m.focus == focusViewer {
			var viewerModel tea.Model
			viewerModel, cmd = m.viewer.Update(msg)
			m.viewer = viewerModel.(tui.IntentViewerModel)
			return m, cmd
		}
		return m, nil

	case tui.ViewerGatherFinishedMsg:
		if m.focus == focusViewer {
			var viewerModel tea.Model
			viewerModel, cmd = m.viewer.Update(msg)
			m.viewer = viewerModel.(tui.IntentViewerModel)
			return m, cmd
		}
		return m, nil

	case gatherFinishedMsg:
		// Handle gather completion
		if msg.err != nil {
			m.statusMessage = "Gather failed: " + msg.err.Error()
		} else {
			m.statusMessage = fmt.Sprintf("Gathered %d intents into: %s", msg.sourceCount, msg.gatheredTitle)
		}
		// Exit multi-select mode and clear selections
		m.exitMultiSelectMode()
		return m, m.loadIntents()

	case filterchip.FilterChangedMsg:
		// A filter selection changed - apply filters
		m.applyFilters()
		return m, nil
	}

	return m, nil
}

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
		if m.confirmDialog.Confirmed() && m.pendingIntent != nil {
			switch m.pendingAction {
			case "delete":
				cmd := m.deleteIntent(m.pendingIntent)
				m.pendingAction = ""
				m.pendingIntent = nil
				return m, cmd
			case "archive":
				cmd := m.archiveIntent(m.pendingIntent)
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
		m.focus = focusList
		if !m.gatherDialog.Cancelled() {
			// Execute gather
			return m, m.executeGather()
		}
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

// updateNormal handles navigation mode keys.
func (m Model) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
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
		// Archive (move to killed status) - requires confirmation
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
			// Scroll preview down
			var cmd tea.Cmd
			m.previewPane, cmd = m.previewPane.Update(msg)
			return m, cmd
		}
		m.moveCursorDown()
		m.updatePreviewForSelection()
	case "k", "up":
		if m.previewFocused && m.showPreview {
			// Scroll preview up
			var cmd tea.Cmd
			m.previewPane, cmd = m.previewPane.Update(msg)
			return m, cmd
		}
		m.moveCursorUp()
		m.updatePreviewForSelection()
	case "ctrl+d":
		if m.previewFocused && m.showPreview {
			var cmd tea.Cmd
			m.previewPane, cmd = m.previewPane.Update(msg)
			return m, cmd
		}
	case "ctrl+u":
		if m.previewFocused && m.showPreview {
			var cmd tea.Cmd
			m.previewPane, cmd = m.previewPane.Update(msg)
			return m, cmd
		}
	case "g":
		if m.previewFocused && m.showPreview {
			var cmd tea.Cmd
			m.previewPane, cmd = m.previewPane.Update(msg)
			return m, cmd
		}
	case "G":
		if m.previewFocused && m.showPreview {
			var cmd tea.Cmd
			m.previewPane, cmd = m.previewPane.Update(msg)
			return m, cmd
		}
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
	case "ctrl+g":
		// Open gather dialog if 2+ intents selected
		return m.handleGatherStart()
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

// handleActionMenuSelection handles action menu selection.
func (m Model) handleActionMenuSelection(msg tui.ActionMenuSelectedMsg) (tea.Model, tea.Cmd) {
	m.focus = focusList
	selected := m.SelectedIntent()
	if selected == nil {
		return m, nil
	}
	switch msg.Action {
	case "view":
		// Open full-screen viewer
		group := m.groups[m.cursorGroup]
		m.focus = focusViewer
		m.viewer = tui.NewIntentViewerModelWithGather(
			m.ctx, selected,
			group.Intents, m.cursorItem,
			m.service, m.gatherSvc, m.width, m.height,
		)
	case "edit":
		return m, openInEditor(selected.Path)
	case "move":
		m.focus = focusMove
		m.intentToMove = selected
		m.moveStatusIdx = 0
	case "promote":
		nextStatus := getNextStatus(selected.Status)
		if nextStatus != selected.Status {
			return m, m.moveIntent(selected, nextStatus)
		}
		m.statusMessage = "Already at final status"
	case "archive":
		if selected.Status != intent.StatusKilled {
			m.focus = focusConfirm
			m.pendingAction = "archive"
			m.pendingIntent = selected
			m.confirmDialog = tui.NewConfirmationDialog(
				"Archive Intent",
				fmt.Sprintf("Archive '%s'?\n\nIt will be moved to killed status.", selected.Title),
			)
		}
	case "delete":
		m.focus = focusConfirm
		m.pendingAction = "delete"
		m.pendingIntent = selected
		m.confirmDialog = tui.NewConfirmationDialog(
			"Delete Intent",
			fmt.Sprintf("Delete '%s'?\n\nThis cannot be undone.", selected.Title),
		)
	case "gather":
		// Enter multi-select mode with current intent pre-selected
		m.enterGatherModeFromAction(selected)
	}
	return m, nil
}
