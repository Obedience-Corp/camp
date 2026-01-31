// Package explorer provides the Intent Explorer TUI component.
package explorer

import (
	"context"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/obediencecorp/camp/internal/concept"
	"github.com/obediencecorp/camp/internal/config"
	"github.com/obediencecorp/camp/internal/intent"
	"github.com/obediencecorp/camp/internal/intent/tui"
)

// focusMode determines which component has keyboard focus.
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

// IntentGroup represents a collapsible group of intents by status.
type IntentGroup struct {
	Name     string
	Status   intent.Status
	Intents  []*intent.Intent
	Expanded bool
}

// Model is the main model for the Intent Explorer TUI.
// It follows the BubbleTea Elm Architecture pattern.
type Model struct {
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
	typeWheel   tui.ScrollWheel
	statusWheel tui.ScrollWheel

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
	conceptPicker tui.ConceptPickerModel
	conceptSvc    concept.Service

	// Concept filter state
	conceptFilterPath   string                 // Active concept filter (empty = all)
	conceptFilterPicker tui.ConceptPickerModel // Picker for selecting filter

	// Move action state
	moveStatusIdx int            // Selected status index in move picker
	intentToMove  *intent.Intent // Intent being moved

	// Confirmation dialog state
	confirmDialog tui.ConfirmationDialog
	pendingAction string         // "delete" or "archive"
	pendingIntent *intent.Intent // Intent for pending action

	// Preview pane state
	previewPane        tui.PreviewPane
	showPreview        bool // Whether preview pane is visible
	previewFocused     bool // Whether preview has focus (vs list)
	previewForceHidden bool // True when terminal is too narrow

	// Help overlay state
	helpOverlay tui.HelpOverlay
	showHelp    bool

	// Action menu state
	actionMenu tui.ActionMenu

	// Full-screen viewer state
	viewer tui.IntentViewerModel

	// Layout state
	layoutMode        layoutMode
	showConceptColumn bool
	fullConceptPaths  bool

	// Multi-select mode for gather
	multiSelectMode bool
	selectedIntents map[string]bool // intent ID -> selected

	// Gather dialog state
	gatherDialog tui.GatherDialog
	intentsDir   string // Base directory for intents (for gather service)
}

// NewModel creates a new Explorer model.
func NewModel(ctx context.Context, svc *intent.IntentService, conceptSvc concept.Service, intentsDir string) Model {
	// Initialize glamour style once at startup (handles adaptive detection).
	// This avoids the slow OSC terminal query on every markdown render.
	globalCfg, _ := config.LoadGlobalConfig(ctx)
	themeName := "adaptive" // default
	if globalCfg != nil {
		themeName = globalCfg.TUI.Theme
	}
	tui.InitGlamourStyle(themeName)

	ti := textinput.New()
	ti.Placeholder = "Search intents..."
	ti.CharLimit = 100
	ti.Width = 40

	tw := tui.NewScrollWheel(typeFilterItems)
	tw.SetWidth(12)

	sw := tui.NewScrollWheel(statusFilterItems)
	sw.SetWidth(12)

	// Title input for new intent creation
	titleIn := textinput.New()
	titleIn.Placeholder = "Enter intent title..."
	titleIn.CharLimit = 100
	titleIn.Width = 40

	return Model{
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

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return m.loadIntents()
}

// loadIntents returns a command that loads intents from the service.
func (m Model) loadIntents() tea.Cmd {
	return func() tea.Msg {
		intents, err := m.service.List(m.ctx, nil)
		return intentsLoadedMsg{intents: intents, err: err}
	}
}

// View implements tea.Model.
func (m Model) View() string {
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

	return m.buildMainView()
}

// SelectedIntent returns the currently selected intent, or nil if none.
func (m Model) SelectedIntent() *intent.Intent {
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
