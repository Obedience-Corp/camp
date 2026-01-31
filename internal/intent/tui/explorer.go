// Package tui provides terminal UI components for intent management.
package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/obediencecorp/camp/internal/concept"
	"github.com/obediencecorp/camp/internal/config"
	"github.com/obediencecorp/camp/internal/intent"
	"github.com/obediencecorp/camp/internal/intent/gather"
)

// IntentGroup represents a collapsible group of intents by status.
type IntentGroup struct {
	Name     string
	Status   intent.Status
	Intents  []*intent.Intent
	Expanded bool
}

// Focus mode determines which component has keyboard focus.
type focusMode int

const (
	focusList focusMode = iota
	focusSearch
	focusTypeFilter
	focusStatusFilter
	focusConceptFilter // Filtering by concept
	focusCreating      // Creating new intent
	focusMove          // Moving intent to different status
	focusConfirm       // Confirmation dialog
	focusActionMenu    // Action menu on intent
	focusViewer        // Full-screen intent viewer
	focusGatherDialog  // Gather dialog for combining intents
)

// layoutMode determines the responsive layout based on terminal width.
type layoutMode int

const (
	layoutNarrow layoutMode = iota // <80 columns
	layoutNormal                   // 80-120 columns
	layoutWide                     // >120 columns
)

// Width breakpoints for responsive layout.
const (
	breakpointNarrow = 80
	breakpointWide   = 120
)

// creationStep represents the current step in new intent creation.
type creationStep int

const (
	stepTitle creationStep = iota
	stepType
	stepConcept
)

// typeFilterItems are the available type filter options.
var typeFilterItems = []string{"All", "Idea", "Feature", "Bug", "Research", "Chore"}

// statusFilterItems are the available status filter options.
var statusFilterItems = []string{"All", "Inbox", "Active", "Ready", "Done", "Killed"}

// ExplorerModel is the main model for the Intent Explorer TUI.
// It follows the BubbleTea Elm Architecture pattern.
type ExplorerModel struct {
	// Data
	intents         []*intent.Intent
	filteredIntents []*intent.Intent
	groups          []IntentGroup
	service         *intent.IntentService
	ctx             context.Context

	// Cursor position in nested structure
	// cursorGroup: which group is selected
	// cursorItem: which item within group (-1 means on group header)
	cursorGroup int
	cursorItem  int

	// Search input
	searchInput textinput.Model

	// Filters
	typeWheel   ScrollWheel
	statusWheel ScrollWheel

	// Focus mode
	focus focusMode

	// Display state
	width    int
	height   int
	ready    bool
	quitting bool

	// Status message
	statusMessage string

	// New intent creation state
	creationStep  creationStep
	titleInput    textinput.Model
	createTypeIdx int
	conceptPicker ConceptPickerModel
	conceptSvc    concept.Service

	// Concept filter state
	conceptFilterPath   string             // Active concept filter (empty = all)
	conceptFilterPicker ConceptPickerModel // Picker for selecting filter

	// Move action state
	moveStatusIdx int            // Selected status index in move picker
	intentToMove  *intent.Intent // Intent being moved

	// Confirmation dialog state
	confirmDialog ConfirmationDialog
	pendingAction string         // "delete" or "archive"
	pendingIntent *intent.Intent // Intent for pending action

	// Preview pane state
	previewPane        PreviewPane
	showPreview        bool // Whether preview pane is visible
	previewFocused     bool // Whether preview has focus (vs list)
	previewForceHidden bool // True when terminal is too narrow

	// Help overlay state
	helpOverlay HelpOverlay
	showHelp    bool

	// Action menu state
	actionMenu ActionMenu

	// Full-screen viewer state
	viewer IntentViewerModel

	// Layout state
	layoutMode        layoutMode
	showConceptColumn bool
	fullConceptPaths  bool

	// Multi-select mode for gather
	multiSelectMode bool
	selectedIntents map[string]bool // intent ID -> selected

	// Gather dialog state
	gatherDialog GatherDialog
	intentsDir   string // Base directory for intents (for gather service)
}

// NewExplorerModel creates a new Explorer model.
func NewExplorerModel(ctx context.Context, svc *intent.IntentService, conceptSvc concept.Service, intentsDir string) ExplorerModel {
	// Initialize glamour style once at startup (handles adaptive detection).
	// This avoids the slow OSC terminal query on every markdown render.
	globalCfg, _ := config.LoadGlobalConfig(ctx)
	themeName := "adaptive" // default
	if globalCfg != nil {
		themeName = globalCfg.TUI.Theme
	}
	initGlamourStyle(themeName)

	ti := textinput.New()
	ti.Placeholder = "Search intents..."
	ti.CharLimit = 100
	ti.Width = 40

	tw := NewScrollWheel(typeFilterItems)
	tw.SetWidth(12)

	sw := NewScrollWheel(statusFilterItems)
	sw.SetWidth(12)

	// Title input for new intent creation
	titleIn := textinput.New()
	titleIn.Placeholder = "Enter intent title..."
	titleIn.CharLimit = 100
	titleIn.Width = 40

	return ExplorerModel{
		service:         svc,
		ctx:             ctx,
		conceptSvc:      conceptSvc,
		cursorGroup:     0,
		cursorItem:      -1, // Start on first group header
		searchInput:     ti,
		typeWheel:       tw,
		statusWheel:     sw,
		titleInput:      titleIn,
		focus:           focusList,
		selectedIntents: make(map[string]bool),
		intentsDir:      intentsDir,
	}
}

// intentsLoadedMsg is sent when intents are loaded from the service.
type intentsLoadedMsg struct {
	intents []*intent.Intent
	err     error
}

// editorFinishedMsg is sent when an external editor closes.
type editorFinishedMsg struct {
	err  error
	path string
}

// openFinishedMsg is sent when system open completes.
type openFinishedMsg struct {
	err error
}

// moveFinishedMsg is sent when an intent move completes.
type moveFinishedMsg struct {
	err       error
	intentID  string
	newStatus intent.Status
}

// Init implements tea.Model.
func (m ExplorerModel) Init() tea.Cmd {
	return m.loadIntents()
}

// loadIntents returns a command that loads intents from the service.
func (m ExplorerModel) loadIntents() tea.Cmd {
	return func() tea.Msg {
		intents, err := m.service.List(m.ctx, nil)
		return intentsLoadedMsg{intents: intents, err: err}
	}
}

// Update implements tea.Model.
func (m ExplorerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.focus == focusSearch {
			// Handle keys when search input has focus
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

		if m.focus == focusTypeFilter {
			// Handle keys when type filter has focus
			switch msg.String() {
			case "esc", "enter", "t":
				m.focus = focusList
				m.typeWheel.Blur()
				m.applyFilters()
				return m, nil
			}
			// Pass to scroll wheel
			m.typeWheel, cmd = m.typeWheel.Update(msg)
			m.applyFilters()
			return m, cmd
		}

		if m.focus == focusStatusFilter {
			// Handle keys when status filter has focus
			switch msg.String() {
			case "esc", "enter", "s":
				m.focus = focusList
				m.statusWheel.Blur()
				m.applyFilters()
				return m, nil
			}
			// Pass to scroll wheel
			m.statusWheel, cmd = m.statusWheel.Update(msg)
			m.applyFilters()
			return m, cmd
		}

		if m.focus == focusConceptFilter {
			// Handle concept filter picker
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

		if m.focus == focusCreating {
			return m.updateCreating(msg)
		}

		if m.focus == focusMove {
			return m.updateMove(msg)
		}

		if m.focus == focusConfirm {
			// Handle confirmation dialog
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

		if m.focus == focusActionMenu {
			// Handle action menu
			m.actionMenu, cmd = m.actionMenu.Update(msg)
			return m, cmd
		}

		if m.focus == focusGatherDialog {
			// Handle gather dialog
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

		if m.focus == focusViewer {
			// Handle viewer - pass all keys to it
			var viewerModel tea.Model
			viewerModel, cmd = m.viewer.Update(msg)
			m.viewer = viewerModel.(IntentViewerModel)
			return m, cmd
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
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "?":
			// Toggle help overlay
			m.showHelp = !m.showHelp
			if m.showHelp {
				m.helpOverlay = NewHelpOverlay(m.width-10, m.height-6)
			}
			return m, nil
		case "/":
			// Enter search mode
			m.focus = focusSearch
			m.searchInput.Focus()
			return m, textinput.Blink
		case "t":
			// Enter type filter mode
			m.focus = focusTypeFilter
			m.typeWheel.Focus()
			return m, nil
		case "s":
			// Enter status filter mode
			m.focus = focusStatusFilter
			m.statusWheel.Focus()
			return m, nil
		case "c":
			// Enter concept filter mode
			m.focus = focusConceptFilter
			m.conceptFilterPicker = NewConceptPickerModel(m.ctx, m.conceptSvc)
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
				return m, m.openInEditor(selected.Path)
			}
		case "o":
			// Open selected intent with system default handler
			if selected := m.SelectedIntent(); selected != nil {
				return m, m.openWithSystem(selected.Path)
			}
		case "O":
			// Reveal in file manager (macOS Finder)
			if selected := m.SelectedIntent(); selected != nil {
				return m, m.revealInFileManager(selected.Path)
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
			if selected := m.SelectedIntent(); selected != nil {
				nextStatus := getNextStatus(selected.Status)
				if nextStatus == selected.Status {
					m.statusMessage = "Already at final status: " + selected.Status.String()
					return m, nil
				}
				return m, m.moveIntent(selected, nextStatus)
			}
			return m, nil
		case "a":
			// Archive (move to killed status) - requires confirmation
			if selected := m.SelectedIntent(); selected != nil {
				if selected.Status == intent.StatusKilled {
					m.statusMessage = "Already archived"
					return m, nil
				}
				m.focus = focusConfirm
				m.pendingAction = "archive"
				m.pendingIntent = selected
				m.confirmDialog = NewConfirmationDialog(
					"Archive Intent",
					fmt.Sprintf("Archive '%s'?\n\nIt will be moved to killed status.", selected.Title),
				)
			}
			return m, nil
		case "d":
			// Delete intent (permanently) - requires confirmation
			if selected := m.SelectedIntent(); selected != nil {
				m.focus = focusConfirm
				m.pendingAction = "delete"
				m.pendingIntent = selected
				m.confirmDialog = NewConfirmationDialog(
					"Delete Intent",
					fmt.Sprintf("Delete '%s'?\n\nThis cannot be undone.", selected.Title),
				)
			}
			return m, nil
		case "f":
			// Open full-screen viewer for selected intent
			if selected := m.SelectedIntent(); selected != nil {
				group := m.groups[m.cursorGroup]
				m.focus = focusViewer
				m.viewer = NewIntentViewerModel(
					m.ctx, selected,
					group.Intents, m.cursorItem,
					m.service, m.width, m.height,
				)
			}
			return m, nil
		case ".":
			// Open action menu for selected intent
			if selected := m.SelectedIntent(); selected != nil {
				m.focus = focusActionMenu
				m.actionMenu = NewActionMenu(selected)
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
		case "tab":
			// Switch focus between list and preview (only when preview visible)
			if m.showPreview {
				m.previewFocused = !m.previewFocused
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
			if len(m.selectedIntents) >= 2 {
				intents := m.getSelectedIntentObjects()
				m.gatherDialog = NewGatherDialog(intents)
				m.focus = focusGatherDialog
			} else if len(m.selectedIntents) == 1 {
				m.statusMessage = "Select at least 2 intents to gather (Space to select)"
			} else {
				m.statusMessage = "Select intents first (Space to select, then Ctrl-g to gather)"
			}
			return m, nil
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
	case ActionMenuSelectedMsg:
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
			m.viewer = NewIntentViewerModel(
				m.ctx, selected,
				group.Intents, m.cursorItem,
				m.service, m.width, m.height,
			)
		case "edit":
			return m, m.openInEditor(selected.Path)
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
				m.confirmDialog = NewConfirmationDialog(
					"Archive Intent",
					fmt.Sprintf("Archive '%s'?\n\nIt will be moved to killed status.", selected.Title),
				)
			}
		case "delete":
			m.focus = focusConfirm
			m.pendingAction = "delete"
			m.pendingIntent = selected
			m.confirmDialog = NewConfirmationDialog(
				"Delete Intent",
				fmt.Sprintf("Delete '%s'?\n\nThis cannot be undone.", selected.Title),
			)
		case "gather":
			// Enter multi-select mode with current intent pre-selected
			m.multiSelectMode = true
			m.selectedIntents[selected.ID] = true
			m.statusMessage = "Select more intents with Space, then Ctrl-g to gather"
		}
		return m, nil

	case ActionMenuCancelledMsg:
		m.focus = focusList
		return m, nil

	// Viewer messages
	case viewerClosedMsg:
		m.focus = focusList
		// Sync cursor to the intent that was being viewed when closing
		// This handles navigation within the viewer (left/right keys)
		m.cursorItem = msg.finalIndex
		if msg.refresh {
			return m, m.loadIntents()
		}
		return m, nil

	case viewerEditorFinishedMsg:
		// Pass back to viewer if still in viewer mode
		if m.focus == focusViewer {
			var viewerModel tea.Model
			viewerModel, cmd = m.viewer.Update(msg)
			m.viewer = viewerModel.(IntentViewerModel)
			return m, cmd
		}
		return m, nil

	case viewerMoveFinishedMsg:
		// Pass back to viewer
		if m.focus == focusViewer {
			var viewerModel tea.Model
			viewerModel, cmd = m.viewer.Update(msg)
			m.viewer = viewerModel.(IntentViewerModel)
			return m, cmd
		}
		return m, nil

	case viewerArchiveFinishedMsg:
		// Pass back to viewer
		if m.focus == focusViewer {
			var viewerModel tea.Model
			viewerModel, cmd = m.viewer.Update(msg)
			m.viewer = viewerModel.(IntentViewerModel)
			return m, cmd
		}
		return m, nil

	case viewerDeleteFinishedMsg:
		// Pass back to viewer
		if m.focus == focusViewer {
			var viewerModel tea.Model
			viewerModel, cmd = m.viewer.Update(msg)
			m.viewer = viewerModel.(IntentViewerModel)
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
	}

	return m, nil
}

// openInEditor opens a file in the user's $EDITOR.
func (m ExplorerModel) openInEditor(filePath string) tea.Cmd {
	// Check file exists before opening
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return func() tea.Msg {
			return editorFinishedMsg{
				err:  fmt.Errorf("file no longer exists: %s", filepath.Base(filePath)),
				path: filePath,
			}
		}
	}

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	c := exec.Command(editor, filePath)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return editorFinishedMsg{err: err, path: filePath}
	})
}

// openWithSystem opens a file with the system's default handler.
func (m ExplorerModel) openWithSystem(filePath string) tea.Cmd {
	return func() tea.Msg {
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", filePath)
		case "linux":
			cmd = exec.Command("xdg-open", filePath)
		case "windows":
			cmd = exec.Command("cmd", "/c", "start", "", filePath)
		default:
			return openFinishedMsg{err: fmt.Errorf("unsupported platform: %s", runtime.GOOS)}
		}
		err := cmd.Start()
		return openFinishedMsg{err: err}
	}
}

// revealInFileManager opens the file manager and selects the file.
func (m ExplorerModel) revealInFileManager(filePath string) tea.Cmd {
	return func() tea.Msg {
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			// macOS: open -R reveals in Finder and selects the file
			cmd = exec.Command("open", "-R", filePath)
		case "linux":
			// Linux: open the containing directory
			cmd = exec.Command("xdg-open", filepath.Dir(filePath))
		case "windows":
			// Windows: explorer /select, highlights the file
			cmd = exec.Command("explorer", "/select,", filePath)
		default:
			return openFinishedMsg{err: fmt.Errorf("unsupported platform: %s", runtime.GOOS)}
		}
		err := cmd.Start()
		return openFinishedMsg{err: err}
	}
}

// applyFilters filters intents using search query and type filter.
func (m *ExplorerModel) applyFilters() {
	query := m.searchInput.Value()
	m.statusMessage = ""

	// Start with all intents
	var filtered []*intent.Intent

	// Apply search if there's a query
	if query == "" {
		filtered = m.intents
	} else {
		// Use fuzzy search via the service
		results, err := m.service.Search(m.ctx, query)
		if err != nil {
			m.statusMessage = "Search error: " + err.Error()
			filtered = m.intents
		} else {
			filtered = results
		}
	}

	// Apply type filter
	typeSelection := m.typeWheel.SelectedValue()
	if typeSelection != "All" && typeSelection != "" {
		typeFiltered := make([]*intent.Intent, 0)
		targetType := strings.ToLower(typeSelection)
		for _, i := range filtered {
			if string(i.Type) == targetType {
				typeFiltered = append(typeFiltered, i)
			}
		}
		filtered = typeFiltered
	}

	// Apply status filter
	statusSelection := m.statusWheel.SelectedValue()
	if statusSelection != "All" && statusSelection != "" {
		statusFiltered := make([]*intent.Intent, 0)
		targetStatus := strings.ToLower(statusSelection)
		for _, i := range filtered {
			if string(i.Status) == targetStatus {
				statusFiltered = append(statusFiltered, i)
			}
		}
		filtered = statusFiltered
	}

	// Apply concept filter
	if m.conceptFilterPath != "" {
		conceptFiltered := make([]*intent.Intent, 0)
		for _, i := range filtered {
			// Match if intent's concept starts with the filter path
			if strings.HasPrefix(i.Concept, m.conceptFilterPath) {
				conceptFiltered = append(conceptFiltered, i)
			}
		}
		filtered = conceptFiltered
	}

	m.filteredIntents = filtered

	// Rebuild groups from filtered intents
	m.groups = groupIntentsByStatus(m.filteredIntents)

	// Reset cursor position
	m.cursorGroup = 0
	m.cursorItem = -1
}

// moveCursorDown moves the cursor down through groups and items.
func (m *ExplorerModel) moveCursorDown() {
	if len(m.groups) == 0 {
		return
	}

	group := &m.groups[m.cursorGroup]

	if m.cursorItem == -1 {
		// On group header
		if group.Expanded && len(group.Intents) > 0 {
			// Move to first item in group
			m.cursorItem = 0
		} else {
			// Move to next group header
			m.moveToNextGroup()
		}
	} else {
		// On an item
		if m.cursorItem < len(group.Intents)-1 {
			// Move to next item in group
			m.cursorItem++
		} else {
			// Move to next group header
			m.moveToNextGroup()
		}
	}
}

// moveCursorUp moves the cursor up through groups and items.
func (m *ExplorerModel) moveCursorUp() {
	if len(m.groups) == 0 {
		return
	}

	switch m.cursorItem {
	case -1:
		// On group header, move to previous group's last item
		if m.cursorGroup > 0 {
			m.cursorGroup--
			prevGroup := &m.groups[m.cursorGroup]
			if prevGroup.Expanded && len(prevGroup.Intents) > 0 {
				m.cursorItem = len(prevGroup.Intents) - 1
			} else {
				m.cursorItem = -1
			}
		}
	case 0:
		// On first item, move to group header
		m.cursorItem = -1
	default:
		// Move up within group
		m.cursorItem--
	}
}

// moveToNextGroup moves cursor to the next group header.
func (m *ExplorerModel) moveToNextGroup() {
	if m.cursorGroup < len(m.groups)-1 {
		m.cursorGroup++
		m.cursorItem = -1
	}
}

// handleSelect handles Enter/Space key - toggle group or open viewer on item.
func (m *ExplorerModel) handleSelect() {
	if len(m.groups) == 0 {
		return
	}

	if m.cursorItem == -1 {
		// On group header, toggle expansion
		m.groups[m.cursorGroup].Expanded = !m.groups[m.cursorGroup].Expanded
	} else {
		// On intent item - open full-screen viewer directly
		if selected := m.SelectedIntent(); selected != nil {
			group := m.groups[m.cursorGroup]
			m.focus = focusViewer
			m.viewer = NewIntentViewerModel(
				m.ctx, selected,
				group.Intents, m.cursorItem,
				m.service, m.width, m.height,
			)
		}
	}
}

// View implements tea.Model.
func (m ExplorerModel) View() string {
	if m.quitting {
		return ""
	}
	if !m.ready {
		return "Loading..."
	}

	// Full-screen viewer takes over entire display
	if m.focus == focusViewer {
		return m.viewer.View()
	}

	// Show creation form if in creating mode
	if m.focus == focusCreating {
		return m.viewCreating()
	}

	// Show concept filter picker if active
	if m.focus == focusConceptFilter {
		return m.viewConceptFilter()
	}

	// Show move status picker if active
	if m.focus == focusMove {
		return m.viewMove()
	}

	// Show confirmation dialog if active
	if m.focus == focusConfirm {
		return m.viewConfirmation()
	}

	// Show gather dialog if active
	if m.focus == focusGatherDialog {
		return m.viewGatherDialog()
	}

	// Show help overlay if active (rendered over main view)
	if m.showHelp {
		return m.viewHelp()
	}

	// Show action menu overlay
	if m.focus == focusActionMenu {
		return m.viewActionMenu()
	}

	var b strings.Builder

	// Title with optional selection count
	b.WriteString(titleStyle.Render("Intent Explorer"))
	if m.multiSelectMode && len(m.selectedIntents) > 0 {
		b.WriteString("  ")
		b.WriteString(selectionCountStyle.Render(fmt.Sprintf("%d selected", len(m.selectedIntents))))
	}
	if m.shouldShowPreview() && m.previewFocused {
		b.WriteString(helpStyle.Render(" [preview focused]"))
	}
	b.WriteString("\n")

	// Search input (only show when in focus or has value)
	if m.focus == focusSearch || m.searchInput.Value() != "" {
		b.WriteString(m.searchInput.View())
		if m.focus == focusSearch {
			b.WriteString("  ")
			b.WriteString(helpStyle.Render("(enter to search, esc to cancel)"))
		}
		b.WriteString("\n")
	}

	// Filter bar - show active filters as pills
	filterBar := m.renderFilterBar()
	if filterBar != "" {
		b.WriteString(filterBar)
		b.WriteString("\n")
	}

	// Only show filter wheels when actively filtering
	if m.focus == focusTypeFilter || m.focus == focusStatusFilter {
		// Type filter indicator
		typeValue := m.typeWheel.SelectedValue()
		if m.focus == focusTypeFilter {
			b.WriteString(helpStyle.Render("Type: "))
			b.WriteString(intentConceptStyle.Render("[" + typeValue + "]"))
			b.WriteString(" ")
			b.WriteString(helpStyle.Render("(j/k to change, enter to select)"))
		}

		// Status filter indicator
		statusValue := m.statusWheel.SelectedValue()
		if m.focus == focusStatusFilter {
			b.WriteString(helpStyle.Render("Status: "))
			b.WriteString(intentConceptStyle.Render("[" + statusValue + "]"))
			b.WriteString(" ")
			b.WriteString(helpStyle.Render("(j/k to change, enter to select)"))
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")

	// Calculate widths based on preview visibility and layout mode
	listWidth := m.width
	if m.shouldShowPreview() {
		switch m.layoutMode {
		case layoutWide:
			listWidth = m.width * 50 / 100
		default:
			listWidth = m.width * 60 / 100
		}
		listWidth = max(listWidth, 40)
	}

	// Calculate available width for title within the list
	titleWidth := listWidth - 35
	if m.layoutMode == layoutNarrow {
		titleWidth = m.width - 28 // cursor + type + date
	}
	titleWidth = max(titleWidth, 20)

	// Build the list view
	var listBuilder strings.Builder

	// Handle empty state
	if len(m.filteredIntents) == 0 {
		if m.searchInput.Value() != "" || m.typeWheel.SelectedValue() != "All" ||
			m.statusWheel.SelectedValue() != "All" || m.conceptFilterPath != "" {
			listBuilder.WriteString(helpStyle.Render("\n  No intents match current filters.\n  Press Escape to clear filters.\n"))
		} else {
			listBuilder.WriteString(helpStyle.Render("\n  No intents found.\n  Press 'n' to create one.\n"))
		}
	}

	for gi, group := range m.groups {
		// Group header
		indicator := "▶"
		if group.Expanded {
			indicator = "▼"
		}

		isGroupSelected := gi == m.cursorGroup && m.cursorItem == -1
		cursor := noCursor
		if isGroupSelected && !m.previewFocused {
			cursor = cursorIndicator
		}

		header := fmt.Sprintf("%s %s %s (%d)", cursor, indicator, group.Name, len(group.Intents))
		if isGroupSelected && !m.previewFocused {
			listBuilder.WriteString(groupHeaderSelectedStyle.Render(header))
		} else {
			listBuilder.WriteString(groupHeaderStyle.Render(header))
		}
		listBuilder.WriteString("\n")

		// Render items if expanded
		if group.Expanded {
			for ii, i := range group.Intents {
				isSelected := gi == m.cursorGroup && ii == m.cursorItem && !m.previewFocused
				listBuilder.WriteString(m.renderIntentRow(i, isSelected, titleWidth))
				listBuilder.WriteString("\n")
			}
		}
	}

	// Combine list and preview if preview is visible
	if m.shouldShowPreview() {
		listView := listBuilder.String()
		previewView := m.previewPane.View()

		// Join horizontally with gap
		combined := lipgloss.JoinHorizontal(
			lipgloss.Top,
			listView,
			"  ", // gap between list and preview
			previewView,
		)
		b.WriteString(combined)
	} else {
		b.WriteString(listBuilder.String())
	}

	// Status bar - adaptive based on layout mode
	b.WriteString("\n")
	b.WriteString(m.renderStatusBar())

	if m.statusMessage != "" {
		b.WriteString("\n")
		b.WriteString(errorStyle.Render(m.statusMessage))
	}

	return b.String()
}

// renderIntentRow renders a single intent row with proper formatting.
// Layout is responsive based on terminal width.
func (m ExplorerModel) renderIntentRow(i *intent.Intent, isSelected bool, maxTitleWidth int) string {
	cursor := noCursor
	if isSelected {
		cursor = cursorIndicator
	}

	// Checkbox for multi-select mode
	checkbox := ""
	if m.multiSelectMode {
		if m.selectedIntents[i.ID] {
			checkbox = checkboxCheckedStyle.Render("☑ ")
		} else {
			checkbox = checkboxUncheckedStyle.Render("☐ ")
		}
	}

	// Truncate title if needed (account for checkbox width in multi-select)
	effectiveTitleWidth := maxTitleWidth
	if m.multiSelectMode {
		effectiveTitleWidth -= 3 // checkbox takes space
	}
	title := i.Title
	if len(title) > effectiveTitleWidth {
		title = title[:effectiveTitleWidth-3] + "..."
	}

	// Format date
	date := formatRelativeTime(i.CreatedAt)

	// Build row parts
	titlePart := intentTitleStyle.Render(title)
	typePart := intentTypeStyle.Render(fmt.Sprintf("[%s]", i.Type))
	datePart := intentDateStyle.Render(date)

	var row string

	switch m.layoutMode {
	case layoutNarrow:
		// Minimal: cursor, checkbox, title, type, date (no concept)
		row = fmt.Sprintf("  %s %s%s  %s  %s", cursor, checkbox, titlePart, typePart, datePart)

	case layoutNormal:
		// Normal: cursor, checkbox, title, type, date, concept (truncated)
		conceptName := "-"
		if i.Concept != "" {
			conceptName = i.ConceptName()
			if len(conceptName) > 15 {
				conceptName = conceptName[:12] + "..."
			}
		}
		conceptPart := intentConceptStyle.Render(conceptName)
		row = fmt.Sprintf("  %s %s%s  %s  %s  %s", cursor, checkbox, titlePart, typePart, datePart, conceptPart)

	case layoutWide:
		// Wide: cursor, checkbox, title, type, date, full concept path
		concept := "-"
		if i.Concept != "" {
			if m.fullConceptPaths {
				concept = i.Concept
			} else {
				concept = i.ConceptName()
			}
		}
		conceptPart := intentConceptStyle.Render(concept)
		row = fmt.Sprintf("  %s %s%s  %s  %s  %s", cursor, checkbox, titlePart, typePart, datePart, conceptPart)
	}

	if isSelected {
		return intentRowSelectedStyle.Render(row)
	}
	return intentRowStyle.Render(row)
}

// renderStatusBar renders the status bar adapted to terminal width.
func (m ExplorerModel) renderStatusBar() string {
	// Add multi-select mode hints
	if m.multiSelectMode {
		count := len(m.selectedIntents)
		switch m.layoutMode {
		case layoutNarrow:
			return helpStyle.Render(fmt.Sprintf("Space: select • Ctrl-g: gather (%d) • Esc: cancel", count))
		default:
			return helpStyle.Render(fmt.Sprintf("Space: toggle select • Ctrl-g: gather %d intents • Esc: exit multi-select • ?: help", count))
		}
	}

	switch m.layoutMode {
	case layoutNarrow:
		// Minimal hints
		return helpStyle.Render("j/k • v • / • n • ? • q")
	case layoutNormal:
		if m.shouldShowPreview() {
			return helpStyle.Render("j/k: nav • v: hide preview • tab: focus • /: search • n: new • q: quit")
		}
		return helpStyle.Render("j/k: nav • v: preview • /: search • t/s: filter • .: actions • Space->Ctrl-g: gather • q: quit")
	case layoutWide:
		if m.shouldShowPreview() {
			return helpStyle.Render("j/k: navigate • v: hide preview • tab: switch focus • /: search • f: full view • n: new • ?: help • q: quit")
		}
		return helpStyle.Render("j/k: navigate • v: preview • /: search • t/s: filter • .: actions • Space->Ctrl-g: gather • ?: help • q: quit")
	}
	return ""
}

// renderFilterBar renders the active filter pills and selection count.
// Returns empty string if no filters are active and not in multi-select mode.
func (m ExplorerModel) renderFilterBar() string {
	var pills []string

	// Check active filters
	typeValue := m.typeWheel.SelectedValue()
	if typeValue != "" && typeValue != "All" {
		pills = append(pills, filterPillStyle.Render(fmt.Sprintf("type:%s ×", strings.ToLower(typeValue))))
	}

	statusValue := m.statusWheel.SelectedValue()
	if statusValue != "" && statusValue != "All" {
		pills = append(pills, filterPillStyle.Render(fmt.Sprintf("status:%s ×", strings.ToLower(statusValue))))
	}

	if m.conceptFilterPath != "" {
		conceptName := m.conceptFilterPath
		// Show just the last part for brevity
		parts := strings.Split(m.conceptFilterPath, "/")
		if len(parts) > 0 {
			conceptName = parts[len(parts)-1]
		}
		pills = append(pills, filterPillStyle.Render(fmt.Sprintf("concept:%s ×", conceptName)))
	}

	if m.searchInput.Value() != "" && m.focus != focusSearch {
		query := m.searchInput.Value()
		if len(query) > 15 {
			query = query[:12] + "..."
		}
		pills = append(pills, filterPillStyle.Render(fmt.Sprintf("search:%s ×", query)))
	}

	// Build the filter bar
	var parts []string

	// Selection count badge (always show when in multi-select)
	if m.multiSelectMode {
		count := len(m.selectedIntents)
		parts = append(parts, selectionCountStyle.Render(fmt.Sprintf("%d selected", count)))
	}

	// Filter pills
	if len(pills) > 0 {
		parts = append(parts, strings.Join(pills, " "))
		parts = append(parts, helpStyle.Render("[Esc: clear]"))
	}

	if len(parts) == 0 {
		return ""
	}

	return "Active: " + strings.Join(parts, "  ")
}

// hasActiveFilters returns true if any filter is active.
func (m ExplorerModel) hasActiveFilters() bool {
	typeValue := m.typeWheel.SelectedValue()
	statusValue := m.statusWheel.SelectedValue()
	return (typeValue != "" && typeValue != "All") ||
		(statusValue != "" && statusValue != "All") ||
		m.conceptFilterPath != "" ||
		(m.searchInput.Value() != "" && m.focus != focusSearch)
}

// clearAllFilters resets all filter values to their defaults.
func (m *ExplorerModel) clearAllFilters() {
	m.typeWheel.SetSelected(0) // "All"
	m.statusWheel.SetSelected(0) // "All"
	m.conceptFilterPath = ""
	m.searchInput.SetValue("")
	m.applyFilters()
	m.statusMessage = "Filters cleared"
}

// toggleSelection toggles the selection state of an intent for multi-select gather.
func (m *ExplorerModel) toggleSelection(i *intent.Intent) {
	if i == nil {
		return
	}

	if m.selectedIntents[i.ID] {
		delete(m.selectedIntents, i.ID)
		// Exit multi-select mode if no selections remain
		if len(m.selectedIntents) == 0 {
			m.multiSelectMode = false
		}
	} else {
		m.selectedIntents[i.ID] = true
		m.multiSelectMode = true
	}
}

// exitMultiSelectMode clears all selections and exits multi-select mode.
func (m *ExplorerModel) exitMultiSelectMode() {
	m.selectedIntents = make(map[string]bool)
	m.multiSelectMode = false
	m.statusMessage = "Selection cleared"
}

// getSelectedIntentObjects returns the full Intent objects for all selected IDs.
func (m ExplorerModel) getSelectedIntentObjects() []*intent.Intent {
	var intents []*intent.Intent
	for _, i := range m.intents {
		if m.selectedIntents[i.ID] {
			intents = append(intents, i)
		}
	}
	return intents
}

// getSelectedIDs returns the IDs of all selected intents.
func (m ExplorerModel) getSelectedIDs() []string {
	ids := make([]string, 0, len(m.selectedIntents))
	for id := range m.selectedIntents {
		ids = append(ids, id)
	}
	return ids
}

// viewActionMenu renders the main view with action menu overlay.
func (m ExplorerModel) viewActionMenu() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Intent Explorer"))
	b.WriteString("\n\n")

	if selected := m.SelectedIntent(); selected != nil {
		b.WriteString("Selected: " + selected.Title + "\n\n")
	}

	b.WriteString(m.actionMenu.View())
	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render("j/k: navigate • Enter: select • Esc: cancel"))

	return b.String()
}

// formatRelativeTime returns a human-friendly relative time string.
func formatRelativeTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	now := time.Now()
	diff := now.Sub(t)

	switch {
	case diff < time.Minute:
		return "just now"
	case diff < time.Hour:
		mins := int(diff.Minutes())
		if mins == 1 {
			return "1m ago"
		}
		return fmt.Sprintf("%dm ago", mins)
	case diff < 24*time.Hour:
		hours := int(diff.Hours())
		if hours == 1 {
			return "1h ago"
		}
		return fmt.Sprintf("%dh ago", hours)
	case diff < 7*24*time.Hour:
		days := int(diff.Hours() / 24)
		if days == 1 {
			return "1d ago"
		}
		return fmt.Sprintf("%dd ago", days)
	default:
		return t.Format("Jan 2")
	}
}

// SelectedIntent returns the currently selected intent, or nil if none.
func (m ExplorerModel) SelectedIntent() *intent.Intent {
	if len(m.groups) == 0 || m.cursorItem == -1 {
		return nil
	}
	group := m.groups[m.cursorGroup]
	if m.cursorItem >= 0 && m.cursorItem < len(group.Intents) {
		return group.Intents[m.cursorItem]
	}
	return nil
}

// groupIntentsByStatus organizes intents into groups by their status.
// Groups are ordered: inbox, active, ready, done, killed.
// Empty groups are still included to maintain consistent ordering.
func groupIntentsByStatus(intents []*intent.Intent) []IntentGroup {
	// Define groups in display order with default expansion
	groups := []IntentGroup{
		{Name: "Inbox", Status: intent.StatusInbox, Expanded: true},
		{Name: "Active", Status: intent.StatusActive, Expanded: true},
		{Name: "Ready", Status: intent.StatusReady, Expanded: false},
		{Name: "Done", Status: intent.StatusDone, Expanded: false},
		{Name: "Killed", Status: intent.StatusKilled, Expanded: false},
	}

	// Create a map for quick lookup
	groupMap := make(map[intent.Status]*IntentGroup)
	for i := range groups {
		groupMap[groups[i].Status] = &groups[i]
	}

	// Distribute intents to groups
	for _, i := range intents {
		if group, ok := groupMap[i.Status]; ok {
			group.Intents = append(group.Intents, i)
		}
	}

	return groups
}

// createTypeOptions are the available types for new intents.
var createTypeOptions = []string{"idea", "feature", "bug", "research", "chore"}

// updateCreating handles key input during new intent creation.
func (m ExplorerModel) updateCreating(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg.String() {
	case "esc":
		// Cancel creation, return to list
		m.focus = focusList
		m.titleInput.Blur()
		return m, nil
	}

	switch m.creationStep {
	case stepTitle:
		switch msg.String() {
		case "enter":
			if m.titleInput.Value() != "" {
				m.creationStep = stepType
				m.titleInput.Blur()
			}
			return m, nil
		}
		m.titleInput, cmd = m.titleInput.Update(msg)
		return m, cmd

	case stepType:
		switch msg.String() {
		case "j", "down":
			if m.createTypeIdx < len(createTypeOptions)-1 {
				m.createTypeIdx++
			}
		case "k", "up":
			if m.createTypeIdx > 0 {
				m.createTypeIdx--
			}
		case "enter":
			// Move to concept selection
			m.creationStep = stepConcept
			m.conceptPicker = NewConceptPickerModel(m.ctx, m.conceptSvc)
			return m, nil
		}
		return m, nil

	case stepConcept:
		switch msg.String() {
		case "tab":
			// Skip concept selection, create intent without concept
			return m.finishIntentCreation("")
		}
		// Pass to concept picker
		m.conceptPicker, cmd = m.conceptPicker.Update(msg)
		if m.conceptPicker.Done() {
			if m.conceptPicker.Cancelled() {
				// Go back to type selection
				m.creationStep = stepType
				return m, nil
			}
			// Create intent with selected concept
			return m.finishIntentCreation(m.conceptPicker.SelectedPath())
		}
		return m, cmd
	}

	return m, nil
}

// finishIntentCreation creates the intent and returns to list view.
func (m ExplorerModel) finishIntentCreation(conceptPath string) (tea.Model, tea.Cmd) {
	title := m.titleInput.Value()
	intentType := intent.Type(createTypeOptions[m.createTypeIdx])

	opts := intent.CreateOptions{
		Title:   title,
		Type:    intentType,
		Concept: conceptPath,
	}

	_, err := m.service.CreateDirect(m.ctx, opts)
	if err != nil {
		m.statusMessage = "Error creating intent: " + err.Error()
		m.focus = focusList
		return m, nil
	}

	m.statusMessage = "Intent created: " + title
	m.focus = focusList

	// Reload intents
	return m, m.loadIntents()
}

// viewCreating renders the new intent creation form.
func (m ExplorerModel) viewCreating() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Create New Intent"))
	b.WriteString("\n\n")

	switch m.creationStep {
	case stepTitle:
		b.WriteString("Enter title:\n")
		b.WriteString(m.titleInput.View())
		b.WriteString("\n\n")
		b.WriteString(helpStyle.Render("Enter: continue • Esc: cancel"))

	case stepType:
		b.WriteString("Title: " + m.titleInput.Value() + "\n\n")
		b.WriteString("Select type:\n")
		for i, t := range createTypeOptions {
			cursor := "  "
			if i == m.createTypeIdx {
				cursor = "> "
			}
			b.WriteString(cursor + t + "\n")
		}
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("j/k: navigate • Enter: continue • Esc: cancel"))

	case stepConcept:
		b.WriteString("Title: " + m.titleInput.Value() + "\n")
		b.WriteString("Type: " + createTypeOptions[m.createTypeIdx] + "\n\n")
		b.WriteString("Select concept (optional):\n\n")
		b.WriteString(m.conceptPicker.View())
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("Tab: skip • Esc: back"))
	}

	return b.String()
}

// viewConceptFilter renders the concept filter picker.
func (m ExplorerModel) viewConceptFilter() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Filter by Concept"))
	b.WriteString("\n\n")
	b.WriteString(m.conceptFilterPicker.View())
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("Esc: cancel"))

	return b.String()
}

// moveStatusOptions are the available statuses for moving intents.
var moveStatusOptions = []struct {
	name   string
	status intent.Status
}{
	{"Inbox", intent.StatusInbox},
	{"Active", intent.StatusActive},
	{"Ready", intent.StatusReady},
	{"Done", intent.StatusDone},
	{"Killed", intent.StatusKilled},
}

// statusWorkflow defines the promotion order for intents.
// Killed is excluded as it's an archive/terminal state.
var statusWorkflow = []intent.Status{
	intent.StatusInbox,
	intent.StatusActive,
	intent.StatusReady,
	intent.StatusDone,
}

// getNextStatus returns the next status in the promotion workflow.
// Returns the same status if already at the final state.
func getNextStatus(current intent.Status) intent.Status {
	for i, s := range statusWorkflow {
		if s == current && i < len(statusWorkflow)-1 {
			return statusWorkflow[i+1]
		}
	}
	return current // No change if at end or not in workflow
}

// updateMove handles key input during move action.
func (m ExplorerModel) updateMove(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// Cancel move
		m.focus = focusList
		m.intentToMove = nil
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
		// Execute move
		if m.intentToMove != nil {
			newStatus := moveStatusOptions[m.moveStatusIdx].status
			if m.intentToMove.Status == newStatus {
				// Already at this status
				m.statusMessage = "Already at " + newStatus.String()
				m.focus = focusList
				m.intentToMove = nil
				return m, nil
			}
			m.focus = focusList
			return m, m.moveIntent(m.intentToMove, newStatus)
		}
	}
	return m, nil
}

// moveIntent moves an intent to a new status.
func (m ExplorerModel) moveIntent(i *intent.Intent, newStatus intent.Status) tea.Cmd {
	return func() tea.Msg {
		_, err := m.service.Move(m.ctx, i.ID, newStatus)
		return moveFinishedMsg{
			err:       err,
			intentID:  i.ID,
			newStatus: newStatus,
		}
	}
}

// archiveIntent archives an intent (moves to killed status).
func (m ExplorerModel) archiveIntent(i *intent.Intent) tea.Cmd {
	return func() tea.Msg {
		_, err := m.service.Archive(m.ctx, i.ID)
		return archiveFinishedMsg{
			err:      err,
			intentID: i.ID,
		}
	}
}

// archiveFinishedMsg is sent when archive completes.
type archiveFinishedMsg struct {
	err      error
	intentID string
}

// deleteIntent permanently deletes an intent.
func (m ExplorerModel) deleteIntent(i *intent.Intent) tea.Cmd {
	return func() tea.Msg {
		err := m.service.Delete(m.ctx, i.ID)
		return deleteFinishedMsg{
			err:   err,
			title: i.Title,
		}
	}
}

// deleteFinishedMsg is sent when delete completes.
type deleteFinishedMsg struct {
	err   error
	title string
}

// executeGather runs the gather operation using the gather service.
func (m ExplorerModel) executeGather() tea.Cmd {
	return func() tea.Msg {
		svc := gather.NewService(m.service, m.intentsDir)
		opts := gather.GatherOptions{
			Title:          m.gatherDialog.Title(),
			ArchiveSources: m.gatherDialog.ArchiveSources(),
		}
		result, err := svc.Gather(m.ctx, m.gatherDialog.IntentIDs(), opts)
		if err != nil {
			return gatherFinishedMsg{err: err}
		}
		return gatherFinishedMsg{
			gatheredID:    result.Gathered.ID,
			gatheredTitle: result.Gathered.Title,
			sourceCount:   result.SourceCount,
		}
	}
}

// viewMove renders the move status picker.
func (m ExplorerModel) viewMove() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Move Intent"))
	b.WriteString("\n\n")

	if m.intentToMove != nil {
		b.WriteString("Moving: " + m.intentToMove.Title + "\n")
		b.WriteString("Current status: " + m.intentToMove.Status.String() + "\n\n")
	}

	b.WriteString("Select new status:\n")
	for i, opt := range moveStatusOptions {
		cursor := "  "
		if i == m.moveStatusIdx {
			cursor = "> "
		}
		// Mark current status
		marker := ""
		if m.intentToMove != nil && m.intentToMove.Status == opt.status {
			marker = " (current)"
		}
		b.WriteString(cursor + opt.name + marker + "\n")
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("j/k: navigate • Enter: move • Esc: cancel"))

	return b.String()
}

// viewConfirmation renders the confirmation dialog.
func (m ExplorerModel) viewConfirmation() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Confirm Action"))
	b.WriteString("\n\n")
	b.WriteString(m.confirmDialog.View())

	return b.String()
}

// viewHelp renders the help overlay.
func (m ExplorerModel) viewHelp() string {
	return m.helpOverlay.View()
}

// viewGatherDialog renders the gather dialog overlay.
func (m ExplorerModel) viewGatherDialog() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Gather Intents"))
	b.WriteString("\n\n")
	b.WriteString(m.gatherDialog.View())

	return b.String()
}

// recalculateLayout updates component sizes based on terminal dimensions.
// getLayoutMode returns the current layout mode based on terminal width.
func (m *ExplorerModel) getLayoutMode() layoutMode {
	switch {
	case m.width < breakpointNarrow:
		return layoutNarrow
	case m.width >= breakpointWide:
		return layoutWide
	default:
		return layoutNormal
	}
}

func (m *ExplorerModel) recalculateLayout() {
	if m.width == 0 || m.height == 0 {
		return
	}

	oldMode := m.layoutMode
	m.layoutMode = m.getLayoutMode()

	// Reserve space for header (title, filters) and footer (help text)
	headerHeight := 4 // Title + filters + spacing
	footerHeight := 3 // Help text + status message
	contentHeight := m.height - headerHeight - footerHeight
	contentHeight = max(contentHeight, 5)

	switch m.layoutMode {
	case layoutNarrow:
		// Force hide preview on narrow terminals
		m.previewForceHidden = true
		m.showConceptColumn = false
		m.fullConceptPaths = false

	case layoutNormal:
		m.previewForceHidden = false
		m.showConceptColumn = true
		m.fullConceptPaths = false

		if m.shouldShowPreview() {
			listWidth := max(m.width*60/100, 40)
			previewWidth := max(m.width-listWidth-2, 30)
			m.previewPane.SetSize(previewWidth, contentHeight)
		}

	case layoutWide:
		m.previewForceHidden = false
		m.showConceptColumn = true
		m.fullConceptPaths = true

		if m.shouldShowPreview() {
			// More generous 50/50 split for wide terminals
			listWidth := m.width * 50 / 100
			previewWidth := m.width - listWidth - 2
			m.previewPane.SetSize(previewWidth, contentHeight)
		}
	}

	// Notify user of layout mode change
	if oldMode != m.layoutMode && m.ready {
		switch m.layoutMode {
		case layoutNarrow:
			if m.showPreview {
				m.statusMessage = "Preview hidden (narrow terminal)"
			}
		case layoutNormal:
			if oldMode == layoutNarrow && m.showPreview {
				m.statusMessage = "Preview restored"
			}
		}
	}
}

// shouldShowPreview returns whether the preview pane should be displayed.
func (m *ExplorerModel) shouldShowPreview() bool {
	if !m.showPreview {
		return false
	}
	if m.previewForceHidden {
		return false
	}
	return true
}

// loadPreviewContent loads content from an intent file into the preview pane.
func (m *ExplorerModel) loadPreviewContent(i *intent.Intent) {
	if i == nil {
		m.previewPane.SetContent("DEBUG", "intent is nil")
		return
	}
	if i.Path == "" {
		m.previewPane.SetContent("DEBUG", fmt.Sprintf("intent.Path is empty\nID: %s\nTitle: %s", i.ID, i.Title))
		return
	}

	content, err := os.ReadFile(i.Path)
	if err != nil {
		m.previewPane.SetContent(i.Title, fmt.Sprintf("DEBUG: Error reading\nPath: %s\nError: %s", i.Path, err.Error()))
		return
	}

	if len(content) == 0 {
		m.previewPane.SetContent(i.Title, fmt.Sprintf("DEBUG: File is empty\nPath: %s", i.Path))
		return
	}

	m.previewPane.SetContent(i.Title, string(content))
}

// updatePreviewForSelection updates preview content when selection changes.
func (m *ExplorerModel) updatePreviewForSelection() {
	if !m.showPreview {
		return
	}
	if selected := m.SelectedIntent(); selected != nil {
		m.loadPreviewContent(selected)
	}
}
