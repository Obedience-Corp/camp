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
	campui "github.com/Obedience-Corp/camp/internal/ui"
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
	focusConvertType   // Type picker for converting a note into an intent
	focusRename        // Text input for renaming an intent
)

// IntentGroup represents a collapsible group of intents by status.
// Nesting fields generalize the former Dungeon-only collapse mechanism so
// Notes folders can share the same parent/child expand behavior later.
type IntentGroup struct {
	Name     string
	Status   intent.Status
	Intents  []*intent.Intent
	Expanded bool

	// Nesting. Depth 0 is a root group; children carry the parent's Status
	// in ParentStatus. A group with Children is a parent: it renders an
	// aggregate DescendantCount when collapsed and its subtree is skipped
	// by cursor navigation when Expanded is false.
	Depth           int
	ParentStatus    intent.Status
	Children        []int // indices into Model.groups
	DescendantCount int   // intents across this group and all descendants
}

// IsNestParent reports whether this group owns nested child groups.
func (g IntentGroup) IsNestParent() bool {
	return len(g.Children) > 0
}

// Model is the main model for the Intent Explorer TUI.
// It follows the BubbleTea Elm Architecture pattern.
type Model struct {
	// Data
	intents         []*intent.Intent
	filteredIntents []*intent.Intent
	searchCorpus    []string
	groups          []IntentGroup
	service         *intent.IntentService
	autoCommit      *autoCommitter
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
	pendingAction string         // "delete", "promote-ready", or "gather"
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

	// Campaign info for git commits
	campaignRoot string
	campaignID   string

	// Full add TUI integration
	addModel    *tui.IntentAddModel
	addNoteMode bool
	author      string
	shortcuts   map[string]string

	// Notes view: when true the explorer lists only notes. The default explorer
	// also includes active notes as the first group.
	notesMode bool

	// Convert action state (note → intent)
	noteToConvert       *intent.Intent
	convertTypeIdx      int
	convertTargetStatus intent.Status

	// Tag overlay (opened with T on a selected item)
	availableTags []string
	tagOverlay    tui.TagOverlay
	tagging       bool
	tagTarget     *intent.Intent

	// Rename overlay (opened with R on a selected item)
	renameInput  textinput.Model
	renameTarget *intent.Intent

	// pendingReselectID, when set, selects the matching item after the next
	// list reload (used to keep the renamed intent selected).
	pendingReselectID string
}

// WithAvailableTags sets the configured tag list offered by the tag overlay.
func (m Model) WithAvailableTags(tags []string) Model {
	m.availableTags = tags
	return m
}

// NewModel creates a new Explorer model.
func NewModel(ctx context.Context, svc *intent.IntentService, conceptSvc concept.Service, intentsDir, campaignRoot, campaignID, author string, shortcuts map[string]string) Model {
	// Initialize glamour style once at startup (handles adaptive detection).
	// This avoids the slow OSC terminal query on every markdown render.
	tui.InitGlamourStyle(config.EffectiveTheme(ctx))

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
		autoCommit:      newAutoCommitter(),
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

// loadIntents returns a command that loads explorer items from the service. The
// default view includes active notes before lifecycle intents; notes mode lists
// the whole note store including archived notes.
func (m Model) loadIntents() tea.Cmd {
	notesMode := m.notesMode
	return func() tea.Msg {
		if notesMode {
			notes, err := m.service.ListNotes(m.ctx, true)
			return intentsLoadedMsg{intents: notes, err: err}
		}
		intents, err := m.service.List(m.ctx, nil)
		if err != nil {
			return intentsLoadedMsg{err: err}
		}
		notes, err := m.service.ListNotes(m.ctx, false)
		if err != nil {
			return intentsLoadedMsg{err: err}
		}
		items := make([]*intent.Intent, 0, len(notes)+len(intents))
		items = append(items, notes...)
		items = append(items, intents...)
		return intentsLoadedMsg{intents: items}
	}
}

// View implements tea.Model.
func (m Model) View() string {
	return campui.FitFullscreenView(m.view(), m.height)
}

func (m Model) view() string {
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

	// Show convert type picker if active
	if m.focus == focusConvertType {
		return m.viewConvert()
	}

	// Show tag overlay if active
	if m.tagging {
		return m.viewTagEdit()
	}

	// Show rename input if active
	if m.focus == focusRename {
		return m.viewRename()
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

// groupIntentsByStatus organizes lifecycle intents into groups by status.
// Groups are ordered: Inbox, Ready, Active, then a collapsible Dungeon parent
// whose children (Done, Killed, Archived, Someday) always live in the groups
// slice at Depth 1. When the parent is collapsed, render and navigation skip
// those children via nesting fields rather than removing them from the slice.
func groupIntentsByStatus(intents []*intent.Intent, dungeonExpanded bool) []IntentGroup {
	// Dungeon child group definitions (Depth 1 under the Dungeon parent).
	dungeonChildren := []IntentGroup{
		{Name: "Done", Status: intent.StatusDone, Expanded: false, Depth: 1},
		{Name: "Killed", Status: intent.StatusKilled, Expanded: false, Depth: 1},
		{Name: "Archived", Status: intent.StatusArchived, Expanded: false, Depth: 1},
		{Name: "Someday", Status: intent.StatusSomeday, Expanded: false, Depth: 1},
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

	// Calculate dungeon total count across all children
	dungeonTotal := 0
	for _, g := range dungeonChildren {
		dungeonTotal += len(g.Intents)
	}

	// Assemble final group list: top groups, dungeon parent, then children.
	// Children always remain in the slice; visibility is controlled by Expanded.
	groups := make([]IntentGroup, 0, len(topGroups)+1+len(dungeonChildren))
	groups = append(groups, topGroups...)

	dungeonParentIdx := len(groups)
	childStart := dungeonParentIdx + 1
	childIndices := make([]int, len(dungeonChildren))
	for i := range dungeonChildren {
		childIndices[i] = childStart + i
		// Parent has no Status; children use Depth for nesting/indent.
		dungeonChildren[i].Depth = 1
	}

	groups = append(groups, IntentGroup{
		Name:            "Dungeon",
		Expanded:        dungeonExpanded,
		Depth:           0,
		Children:        childIndices,
		DescendantCount: dungeonTotal,
	})
	groups = append(groups, dungeonChildren...)

	return groups
}

// groupExplorerItemsByStatus builds the default explorer list: Inbox/Ready/Active,
// then the Notes tree (collapsed by default), then Dungeon. Nested notes under
// notes/<folder>/ are included — they must never be dropped.
//
// foldState maps note-folder status → Expanded. omitEmpty drops empty note
// folders (used while a filter is active).
func groupExplorerItemsByStatus(items []*intent.Intent, dungeonExpanded bool, foldState map[string]bool, omitEmpty bool) []IntentGroup {
	notes := make([]*intent.Intent, 0)
	intents := make([]*intent.Intent, 0, len(items))
	for _, item := range items {
		if item.Status.IsNote() {
			notes = append(notes, item)
			continue
		}
		intents = append(intents, item)
	}

	lifecycle := groupIntentsByStatus(intents, dungeonExpanded)
	// lifecycle: [Inbox, Ready, Active, Dungeon, Done, Killed, Archived, Someday]
	top := lifecycle[:3]
	dungeonPart := lifecycle[3:]
	notesTree := buildNotesTreeGroups(notes, nil, foldState, omitEmpty)

	out := make([]IntentGroup, 0, len(top)+len(notesTree)+len(dungeonPart))
	out = append(out, top...)

	notesStart := len(out)
	out = append(out, notesTree...)
	for i := notesStart; i < notesStart+len(notesTree); i++ {
		for j := range out[i].Children {
			out[i].Children[j] += notesStart
		}
	}

	// Re-append dungeon with Children rewired to absolute indices in out.
	if len(dungeonPart) == 0 {
		return out
	}
	dungeonStart := len(out)
	parent := dungeonPart[0]
	parent.Children = make([]int, 0, len(dungeonPart)-1)
	out = append(out, parent)
	for i := 1; i < len(dungeonPart); i++ {
		out[dungeonStart].Children = append(out[dungeonStart].Children, dungeonStart+i)
		child := dungeonPart[i]
		child.Children = nil
		out = append(out, child)
	}
	return out
}

// nestExpanded returns the Expanded flag for a named nest parent, defaulting
// to false (collapsed) when the parent is not yet present in m.groups.
func (m *Model) nestExpanded(name string) bool {
	for _, g := range m.groups {
		if g.Name == name && g.IsNestParent() {
			return g.Expanded
		}
	}
	return false
}

// noteFoldState captures Expanded flags for all note-status groups so rebuilds
// (filter apply/clear, reload) can restore the user's fold choices.
func (m *Model) noteFoldState() map[string]bool {
	out := make(map[string]bool)
	for _, g := range m.groups {
		if g.Status.IsNote() || g.Status == intent.StatusNote {
			out[string(g.Status)] = g.Expanded
		}
	}
	return out
}

func (m *Model) rebuildStatusGroups() {
	dungeonExpanded := m.nestExpanded("Dungeon")
	fold := m.noteFoldState()
	omitEmpty := m.hasActiveFilters()
	if m.notesMode {
		m.groups = groupNotes(m.filteredIntents, fold, omitEmpty)
		if omitEmpty {
			expandNoteAncestorsForMatches(m.groups)
		}
		return
	}
	m.groups = groupExplorerItemsByStatus(m.filteredIntents, dungeonExpanded, fold, omitEmpty)
	if omitEmpty {
		expandNoteAncestorsForMatches(m.groups)
	}
}

// isGroupVisible reports whether group gi should be shown and navigable.
// Nested groups are hidden while any ancestor nest parent is collapsed.
func (m *Model) isGroupVisible(gi int) bool {
	if gi < 0 || gi >= len(m.groups) {
		return false
	}
	g := m.groups[gi]
	if g.Depth == 0 {
		return true
	}
	for pi, p := range m.groups {
		for _, ci := range p.Children {
			if ci == gi {
				return p.Expanded && m.isGroupVisible(pi)
			}
		}
	}
	return true
}
