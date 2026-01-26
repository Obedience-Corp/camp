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
	"github.com/obediencecorp/camp/internal/intent"
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
	previewPane    PreviewPane
	showPreview    bool // Whether preview pane is visible
	previewFocused bool // Whether preview has focus (vs list)

	// Help overlay state
	helpOverlay HelpOverlay
	showHelp    bool
}

// NewExplorerModel creates a new Explorer model.
func NewExplorerModel(ctx context.Context, svc *intent.IntentService, conceptSvc concept.Service) ExplorerModel {
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
		service:     svc,
		ctx:         ctx,
		conceptSvc:  conceptSvc,
		cursorGroup: 0,
		cursorItem:  -1, // Start on first group header
		searchInput: ti,
		typeWheel:   tw,
		statusWheel: sw,
		titleInput:  titleIn,
		focus:       focusList,
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
		case "v":
			// Toggle preview pane visibility
			m.showPreview = !m.showPreview
			m.recalculateLayout()
			// Load preview content for currently selected intent
			if m.showPreview {
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
		case "enter", " ":
			m.handleSelect()
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

	if m.cursorItem == -1 {
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
	} else if m.cursorItem == 0 {
		// On first item, move to group header
		m.cursorItem = -1
	} else {
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

// handleSelect handles Enter/Space key - toggle group or select item.
func (m *ExplorerModel) handleSelect() {
	if len(m.groups) == 0 {
		return
	}

	if m.cursorItem == -1 {
		// On group header, toggle expansion
		m.groups[m.cursorGroup].Expanded = !m.groups[m.cursorGroup].Expanded
	}
	// On item - future: open detail view
}

// View implements tea.Model.
func (m ExplorerModel) View() string {
	if m.quitting {
		return ""
	}
	if !m.ready {
		return "Loading..."
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

	// Show help overlay if active (rendered over main view)
	if m.showHelp {
		return m.viewHelp()
	}

	var b strings.Builder

	// Title
	b.WriteString(titleStyle.Render("Intent Explorer"))
	if m.showPreview && m.previewFocused {
		b.WriteString(helpStyle.Render(" [preview focused]"))
	}
	b.WriteString("\n")

	// Search input and type filter
	b.WriteString(m.searchInput.View())
	if m.focus == focusSearch {
		b.WriteString("  ")
		b.WriteString(helpStyle.Render("(enter to search, esc to cancel)"))
	}
	b.WriteString("  ")

	// Type filter indicator
	typeValue := m.typeWheel.SelectedValue()
	if m.focus == focusTypeFilter {
		b.WriteString(helpStyle.Render("Type: "))
		b.WriteString(intentConceptStyle.Render("[" + typeValue + "]"))
		b.WriteString(" ")
		b.WriteString(helpStyle.Render("(j/k to change, enter to select)"))
	} else {
		b.WriteString(helpStyle.Render("Type: [" + typeValue + "]"))
	}
	b.WriteString("  ")

	// Status filter indicator
	statusValue := m.statusWheel.SelectedValue()
	if m.focus == focusStatusFilter {
		b.WriteString(helpStyle.Render("Status: "))
		b.WriteString(intentConceptStyle.Render("[" + statusValue + "]"))
		b.WriteString(" ")
		b.WriteString(helpStyle.Render("(j/k to change, enter to select)"))
	} else {
		b.WriteString(helpStyle.Render("Status: [" + statusValue + "]"))
	}
	b.WriteString("  ")

	// Concept filter indicator
	conceptValue := "All"
	if m.conceptFilterPath != "" {
		// Show just the last part of the path for brevity
		parts := strings.Split(m.conceptFilterPath, "/")
		conceptValue = parts[len(parts)-1]
	}
	b.WriteString(helpStyle.Render("Concept: [" + conceptValue + "]"))
	b.WriteString("\n\n")

	// Calculate widths based on preview visibility
	listWidth := m.width
	if m.showPreview {
		listWidth = m.width * 60 / 100
		if listWidth < 40 {
			listWidth = 40
		}
	}

	// Calculate available width for title within the list (leave room for date and type)
	titleWidth := listWidth - 35
	if titleWidth < 20 {
		titleWidth = 20
	}

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
	if m.showPreview {
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

	// Status bar
	b.WriteString("\n")
	if m.showPreview {
		b.WriteString(helpStyle.Render("j/k: navigate • v: hide preview • tab: switch focus • /: search • n: new • q: quit"))
	} else {
		b.WriteString(helpStyle.Render("j/k: navigate • v: show preview • /: search • t: type • s: status • c: concept • n: new • q: quit"))
	}

	if m.statusMessage != "" {
		b.WriteString("\n")
		b.WriteString(errorStyle.Render(m.statusMessage))
	}

	return b.String()
}

// renderIntentRow renders a single intent row with proper formatting.
func (m ExplorerModel) renderIntentRow(i *intent.Intent, isSelected bool, maxTitleWidth int) string {
	cursor := noCursor
	if isSelected {
		cursor = cursorIndicator
	}

	// Truncate title if needed
	title := i.Title
	if len(title) > maxTitleWidth {
		title = title[:maxTitleWidth-3] + "..."
	}

	// Format date
	date := formatRelativeTime(i.CreatedAt)

	// Build row parts
	titlePart := intentTitleStyle.Render(title)
	typePart := intentTypeStyle.Render(fmt.Sprintf("[%s]", i.Type))
	datePart := intentDateStyle.Render(date)

	// Add concept column - show concept name only (not full path)
	conceptName := "-"
	if i.Concept != "" {
		conceptName = i.ConceptName()
		// Truncate long concept names for alignment
		if len(conceptName) > 15 {
			conceptName = conceptName[:12] + "..."
		}
	}
	conceptPart := intentConceptStyle.Render(conceptName)

	row := fmt.Sprintf("  %s  %s  %s  %s  %s", cursor, titlePart, typePart, datePart, conceptPart)

	if isSelected {
		return intentRowSelectedStyle.Render(row)
	}
	return intentRowStyle.Render(row)
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

// recalculateLayout updates component sizes based on terminal dimensions.
func (m *ExplorerModel) recalculateLayout() {
	if m.width == 0 || m.height == 0 {
		return
	}

	// Reserve space for header (title, filters) and footer (help text)
	headerHeight := 4 // Title + filters + spacing
	footerHeight := 3 // Help text + status message
	contentHeight := m.height - headerHeight - footerHeight
	if contentHeight < 5 {
		contentHeight = 5
	}

	if m.showPreview {
		// Split layout: list on left (60%), preview on right (40%)
		listWidth := m.width * 60 / 100
		previewWidth := m.width - listWidth - 2 // -2 for border/spacing

		// Minimum widths
		if listWidth < 40 {
			listWidth = 40
		}
		if previewWidth < 30 {
			previewWidth = 30
		}

		m.previewPane.SetSize(previewWidth, contentHeight)
	}
}

// loadPreviewContent loads content from an intent file into the preview pane.
func (m *ExplorerModel) loadPreviewContent(i *intent.Intent) {
	if i == nil || i.Path == "" {
		return
	}

	content, err := os.ReadFile(i.Path)
	if err != nil {
		m.previewPane.SetContent(i.Title, "Error loading content: "+err.Error())
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
