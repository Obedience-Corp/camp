// Package explorer provides the Intent Explorer TUI component.
package explorer

import (
	"context"

	"github.com/Obedience-Corp/camp/internal/concept"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/intent/gather"
	"github.com/Obedience-Corp/camp/internal/intent/tui"
	"github.com/Obedience-Corp/camp/internal/intent/tui/filterchip"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// focusMode determines which component has keyboard focus.
type focusMode int

const (
	focusList focusMode = iota
	focusSearch
	focusFilterBar     // Filter bar has focus (Tab navigation between chips)
	focusConceptFilter // Filtering by concept (concept picker modal)
	focusMove          // Moving intent to different status
	focusConfirm       // Confirmation dialog
	focusActionMenu    // Action menu on intent
	focusViewer        // Full-screen intent viewer
	focusGatherDialog  // Gather dialog for combining intents
	focusAddTUI        // Full add TUI is active
	focusPromoteTarget // Promote target picker
	focusDungeonReason // Text input for dungeon move reason
)

// IntentGroup represents a collapsible group of intents by status.
type IntentGroup struct {
	Name            string
	Status          intent.Status
	Intents         []*intent.Intent
	Expanded        bool
	IsDungeonParent bool // True for the "Dungeon" meta-group
	IsDungeonChild  bool // True for Done/Killed/Archived/Someday under dungeon
	DungeonCount    int  // Total intent count across all dungeon children (parent only)
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
	filterBar filterchip.Bar

	// Focus mode
	focus focusMode

	// Display state
	width    int
	height   int
	ready    bool
	quitting bool

	// Status message
	statusMessage string

	// Concept service (for concept filter and add TUI)
	conceptSvc concept.Service

	// Concept filter state
	conceptFilterPath   string                 // Active concept filter (empty = all)
	conceptFilterPicker tui.ConceptPickerModel // Picker for selecting filter

	// Move action state
	moveStatusIdx int            // Selected status index in move picker
	intentToMove  *intent.Intent // Intent being moved

	// Confirmation dialog state
	confirmDialog tui.ConfirmationDialog
	pendingAction string         // "delete", "promote", "promote-ready", or "gather"
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

	// List scrolling
	scrollOffset int // First visible line in the list
	listHeight   int // Number of visible lines in the list area

	// Vim-style count prefix (e.g., "5j" moves 5 lines)
	countBuffer int    // Accumulated digit count (0 = no count entered)
	pendingKey  string // For multi-key sequences like "gg"

	// Multi-select mode for gather
	multiSelectMode bool
	selectedIntents map[string]bool // intent ID -> selected

	// Gather dialog state
	gatherDialog tui.GatherDialog
	intentsDir   string          // Base directory for intents (for gather service)
	gatherSvc    *gather.Service // Gather service for finding similar intents

	// Promote target picker state
	promoteTargetIdx    int
	promoteTargetIntent *intent.Intent

	// Dungeon move reason state
	dungeonReasonInput  textinput.Model
	dungeonReasonFor    intent.Status  // Which dungeon status we're moving to
	dungeonReasonAction string         // "move" or "archive"
	dungeonReasonIntent *intent.Intent // Intent being moved to dungeon

	// Dungeon expansion state
	dungeonExpanded bool

	// Campaign info for git commits
	campaignRoot string
	campaignID   string

	// Full add TUI integration
	addModel  *tui.IntentAddModel
	author    string
	shortcuts map[string]string
}

// NewModel creates a new Explorer model.
func NewModel(ctx context.Context, svc *intent.IntentService, conceptSvc concept.Service, intentsDir, campaignRoot, campaignID, author string, shortcuts map[string]string) Model {
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

	// Create filter bar with type, status, and concept chips
	typeChip := filterchip.NewChip("Type", typeFilterItems)
	statusChip := filterchip.NewChip("Status", statusFilterItems)
	fb := filterchip.NewBar(typeChip, statusChip)

	return Model{
		service:         svc,
		ctx:             ctx,
		conceptSvc:      conceptSvc,
		cursorGroup:     0,
		cursorItem:      -1, // Start on first group header
		searchInput:     ti,
		filterBar:       fb,
		focus:           focusList,
		selectedIntents: make(map[string]bool),
		intentsDir:      intentsDir,
		gatherSvc:       gather.NewService(svc, intentsDir),
		campaignRoot:    campaignRoot,
		campaignID:      campaignID,
		author:          author,
		shortcuts:       shortcuts,
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

	// Show full add TUI if active
	if m.focus == focusAddTUI {
		return m.addModel.View()
	}

	// Show concept filter picker if active
	if m.focus == focusConceptFilter {
		return m.viewConceptFilter()
	}

	// Show promote target picker if active
	if m.focus == focusPromoteTarget {
		return m.viewPromoteTarget()
	}

	// Show dungeon reason input if active
	if m.focus == focusDungeonReason {
		return m.viewDungeonReason()
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
// Groups are ordered: Inbox, Active, Ready, then a collapsible Dungeon parent
// that contains Done, Killed, Archived, Someday as children.
// When dungeonExpanded is false, only the Dungeon parent group is shown.
// When true, the 4 child groups are appended after the parent.
func groupIntentsByStatus(intents []*intent.Intent, dungeonExpanded bool) []IntentGroup {
	// Dungeon child group definitions (indentation applied at render time)
	dungeonChildren := []IntentGroup{
		{Name: "Done", Status: intent.StatusDone, Expanded: false, IsDungeonChild: true},
		{Name: "Killed", Status: intent.StatusKilled, Expanded: false, IsDungeonChild: true},
		{Name: "Archived", Status: intent.StatusArchived, Expanded: false, IsDungeonChild: true},
		{Name: "Someday", Status: intent.StatusSomeday, Expanded: false, IsDungeonChild: true},
	}

	// Create a map for intent distribution
	groupMap := make(map[intent.Status]*IntentGroup)
	for i := range dungeonChildren {
		groupMap[dungeonChildren[i].Status] = &dungeonChildren[i]
	}

	// Top-level groups (pipeline order: inbox → ready → active)
	topGroups := []IntentGroup{
		{Name: "Inbox", Status: intent.StatusInbox, Expanded: true},
		{Name: "Ready", Status: intent.StatusReady, Expanded: true},
		{Name: "Active", Status: intent.StatusActive, Expanded: true},
	}
	for i := range topGroups {
		groupMap[topGroups[i].Status] = &topGroups[i]
	}

	// Distribute intents to groups
	for _, i := range intents {
		if group, ok := groupMap[i.Status]; ok {
			group.Intents = append(group.Intents, i)
		}
	}

	// Calculate dungeon total count
	dungeonTotal := 0
	for _, g := range dungeonChildren {
		dungeonTotal += len(g.Intents)
	}

	// Build dungeon parent
	dungeonParent := IntentGroup{
		Name:            "Dungeon",
		IsDungeonParent: true,
		Expanded:        dungeonExpanded,
		DungeonCount:    dungeonTotal,
	}

	// Assemble final group list
	groups := make([]IntentGroup, 0, len(topGroups)+1+len(dungeonChildren))
	groups = append(groups, topGroups...)
	groups = append(groups, dungeonParent)

	if dungeonExpanded {
		groups = append(groups, dungeonChildren...)
	}

	return groups
}
