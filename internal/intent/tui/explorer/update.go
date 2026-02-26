package explorer

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/Obedience-Corp/camp/internal/intent/tui"
	"github.com/Obedience-Corp/camp/internal/intent/tui/filterchip"
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
		m.groups = groupIntentsByStatus(msg.intents, m.dungeonExpanded)

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
		if !selected.Status.InDungeon() {
			m.focus = focusConfirm
			m.pendingAction = "archive"
			m.pendingIntent = selected
			m.confirmDialog = tui.NewConfirmationDialog(
				"Archive Intent",
				fmt.Sprintf("Archive '%s'?\n\nIt will be moved to the dungeon.", selected.Title),
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
