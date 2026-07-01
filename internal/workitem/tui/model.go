// Package tui provides a Bubble Tea dashboard for browsing campaign work items.
package tui

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/priority"
)

// chromeHeight is the number of lines consumed by header + footer + separator.
// Update this if the layout structure changes.
const chromeHeight = 3

// builtinFilterTypes pins keys 1-4 to the builtin workflow types.
var builtinFilterTypes = []string{
	string(workitem.WorkflowTypeIntent),
	string(workitem.WorkflowTypeDesign),
	string(workitem.WorkflowTypeExplore),
	string(workitem.WorkflowTypeFestival),
}

// customTypes returns the distinct non-builtin workflow types in items,
// sorted alphabetically.
func customTypes(items []workitem.WorkItem) []string {
	builtin := make(map[string]bool, len(builtinFilterTypes))
	for _, t := range builtinFilterTypes {
		builtin[t] = true
	}
	seen := make(map[string]bool)
	var out []string
	for _, item := range items {
		t := string(item.WorkflowType)
		if t == "" || builtin[t] || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// filterOptions returns the filter-mode chip values: "" (all) first, then
// builtins present in the items in canonical order, then custom types.
func filterOptions(items []workitem.WorkItem) []string {
	present := make(map[string]bool, len(items))
	for _, item := range items {
		if t := string(item.WorkflowType); t != "" {
			present[t] = true
		}
	}
	opts := []string{""}
	for _, t := range builtinFilterTypes {
		if present[t] {
			opts = append(opts, t)
		}
	}
	return append(opts, customTypes(items)...)
}

// Model is the Bubble Tea model for the workitem dashboard.
type Model struct {
	// Data
	allItems      []workitem.WorkItem
	filteredItems []workitem.WorkItem
	err           error

	// Navigation
	cursor       int
	scrollOffset int // first visible row index for list viewport
	width        int
	height       int
	ready        bool

	// Search
	searchMode       bool
	searchInput      textinput.Model
	searchQuery      string // committed search query used for filtering
	savedSearchQuery string // snapshot of committed query when search mode starts

	// Filters
	typeFilter      string // empty = all, or any workflow type
	showParked      bool
	filterMode      bool
	filterOptions   []string // chip values while filter mode is active; "" = all
	filterIndex     int
	savedTypeFilter string // snapshot of typeFilter when filter mode starts

	// Preview
	showPreview    bool
	previewOverlay bool // narrow mode: overlay preview on top of list
	helpVisible    bool

	// Transient status message shown in footer (e.g. "copied!", "clipboard unavailable")
	statusMsg     string
	statusIsError bool

	// Vim navigation
	lastKeyWasG bool

	// Selection result (read by command layer after Run)
	Selected *workitem.WorkItem

	// Refresh context — stored here because Bubble Tea's Update() receives
	// tea.Msg, not context.Context. The ctx is only used by refreshCmd().
	ctx          context.Context
	campaignRoot string
	resolver     *paths.Resolver

	// Priority store for TUI mutations (set/clear priority).
	priorityStore *priority.Store
	storePath     string
	priorityMode  bool
	stageMode     bool
}

// New creates the dashboard model from a pre-discovered item list.
func New(ctx context.Context, items []workitem.WorkItem, campaignRoot string, resolver *paths.Resolver, store *priority.Store, storePath string, showParked ...bool) Model {
	ti := textinput.New()
	ti.Placeholder = "search..."
	ti.CharLimit = 64

	includeParked := false
	if len(showParked) > 0 {
		includeParked = showParked[0]
	}

	return Model{
		allItems:      items,
		filteredItems: items,
		searchInput:   ti,
		showPreview:   true,
		ctx:           ctx,
		campaignRoot:  campaignRoot,
		resolver:      resolver,
		priorityStore: store,
		storePath:     storePath,
		showParked:    includeParked,
	}
}

// typeFilterFor resolves a pressed key to a type filter value.
// "0" clears the filter; 1-4 are the builtin types.
func (m Model) typeFilterFor(key string) (string, bool) {
	if key == "0" {
		return "", true
	}
	if len(key) != 1 || key[0] < '1' || key[0] > '0'+byte(len(builtinFilterTypes)) {
		return "", false
	}
	return builtinFilterTypes[key[0]-'1'], true
}

// enterFilterMode activates the type filter stepper with chips rebuilt from
// the current item set and the index positioned on the active filter.
func (m *Model) enterFilterMode() {
	m.filterOptions = filterOptions(m.allItems)
	m.filterIndex = 0
	for i, opt := range m.filterOptions {
		if opt == m.typeFilter {
			m.filterIndex = i
			break
		}
	}
	m.savedTypeFilter = m.typeFilter
	m.filterMode = true
}

func (m *Model) exitFilterMode() {
	m.filterMode = false
}

func (m Model) isFilterMode() bool {
	return m.filterMode
}

// rebuildFilterOptions refreshes filter-mode chips after the item set
// changed, re-locating the active chip by value.
func (m *Model) rebuildFilterOptions() {
	m.filterOptions = filterOptions(m.allItems)
	m.filterIndex = 0
	for i, opt := range m.filterOptions {
		if opt == m.typeFilter {
			m.filterIndex = i
			break
		}
	}
}

// visibleTypeCounts tallies items per workflow type under the same parked
// visibility rule the list applies, returning the counts and the total.
func (m Model) visibleTypeCounts() (map[string]int, int) {
	counts := make(map[string]int)
	total := 0
	for _, item := range m.allItems {
		if !m.showParked && item.AttentionStage == "parked" {
			continue
		}
		counts[string(item.WorkflowType)]++
		total++
	}
	return counts, total
}

func (m Model) Init() tea.Cmd {
	return nil
}

// viewportHeight returns the number of visible list rows.
func (m Model) viewportHeight() int {
	h := m.height - chromeHeight
	if h < 0 {
		return 0
	}
	return h
}

// currentItem returns the work item under the cursor, or a zero-value item if empty.
func (m Model) currentItem() workitem.WorkItem {
	if len(m.filteredItems) == 0 || m.cursor >= len(m.filteredItems) {
		return workitem.WorkItem{}
	}
	return m.filteredItems[m.cursor]
}

// refilter applies current type filter and search query to allItems,
// then clamps cursor and scrollOffset to stay within bounds.
func (m *Model) refilter() {
	var types []string
	if m.typeFilter != "" {
		types = []string{m.typeFilter}
	}
	m.filteredItems = workitem.FilterAdvanced(m.allItems, workitem.FilterOptions{
		Types:      types,
		Query:      m.searchQuery,
		ShowParked: m.showParked,
	})
	if m.cursor >= len(m.filteredItems) {
		m.cursor = max(0, len(m.filteredItems)-1)
	}
	m.clampScroll()
}

// clampScroll ensures scrollOffset is valid for the current item count and viewport.
func (m *Model) clampScroll() {
	vp := m.viewportHeight()
	maxOffset := max(0, len(m.filteredItems)-vp)
	if m.scrollOffset > maxOffset {
		m.scrollOffset = maxOffset
	}
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
	// Also ensure cursor is visible within the viewport
	if vp > 0 {
		if m.cursor < m.scrollOffset {
			m.scrollOffset = m.cursor
		}
		if m.cursor >= m.scrollOffset+vp {
			m.scrollOffset = m.cursor - vp + 1
		}
	}
}

// preserveSelection moves the cursor to the item matching key after a resort.
// If the key is not found in filteredItems, the cursor is clamped to the
// nearest valid index.
func (m *Model) preserveSelection(key string) {
	if key == "" || len(m.filteredItems) == 0 {
		m.cursor = 0
		m.clampScroll()
		return
	}
	for i, item := range m.filteredItems {
		if item.Key == key {
			m.cursor = i
			m.clampScroll()
			return
		}
	}
	if m.cursor >= len(m.filteredItems) {
		m.cursor = len(m.filteredItems) - 1
	}
	m.clampScroll()
}

// enterPriorityMode activates the priority picker if items are available.
func (m *Model) enterPriorityMode() {
	if len(m.filteredItems) > 0 && m.priorityStore != nil {
		m.priorityMode = true
	}
}

// exitPriorityMode returns to normal navigation mode.
func (m *Model) exitPriorityMode() {
	m.priorityMode = false
}

// isPriorityMode reports whether the priority picker is active.
func (m Model) isPriorityMode() bool {
	return m.priorityMode
}

func (m *Model) enterStageMode() {
	if len(m.filteredItems) > 0 && m.priorityStore != nil {
		m.stageMode = true
	}
}

func (m *Model) exitStageMode() {
	m.stageMode = false
}

func (m Model) isStageMode() bool {
	return m.stageMode
}

// refreshMsg carries the result of a background re-discovery.
type refreshMsg struct {
	items []workitem.WorkItem
	err   error
}

// editorFinishedMsg is sent when an external editor process exits.
type editorFinishedMsg struct {
	err error
}

// clearStatusMsg clears the transient status message after a timeout.
type clearStatusMsg struct{}

func (m Model) refreshCmd() tea.Cmd {
	ctx := m.ctx
	root := m.campaignRoot
	resolver := m.resolver
	return func() tea.Msg {
		items, err := workitem.Discover(ctx, root, resolver)
		return refreshMsg{items: items, err: err}
	}
}

// setStatus sets a transient footer message and returns a command to clear it after 2 seconds.
func (m *Model) setStatus(msg string, isError bool) tea.Cmd {
	m.statusMsg = msg
	m.statusIsError = isError
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg {
		return clearStatusMsg{}
	})
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s[:width]
	}
	return s + strings.Repeat(" ", width-len(s))
}
