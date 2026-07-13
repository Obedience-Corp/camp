// Package tui provides a Bubble Tea dashboard for browsing campaign work items.
package tui

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Obedience-Corp/camp/internal/config"
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

// builtinFilterCategories pins the canonical category cycle order.
var builtinFilterCategories = []string{
	config.WorkflowCategoryPlan,
	config.WorkflowCategoryResearch,
	config.WorkflowCategoryPipeline,
	config.WorkflowCategoryReview,
}

func deriveCategoryOptions(items []workitem.WorkItem, ensure string) []string {
	builtin := make(map[string]bool, len(builtinFilterCategories))
	for _, c := range builtinFilterCategories {
		builtin[c] = true
	}
	present := make(map[string]bool, len(items))
	for _, item := range items {
		if c := item.WorkflowCategory; c != "" {
			present[c] = true
		}
	}
	if ensure != "" {
		present[ensure] = true
	}
	opts := []string{""}
	for _, c := range builtinFilterCategories {
		if present[c] {
			opts = append(opts, c)
		}
	}
	var customs []string
	for c := range present {
		if !builtin[c] {
			customs = append(customs, c)
		}
	}
	sort.Strings(customs)
	return append(opts, customs...)
}

// deriveFilterOptions returns the filter-mode chip values: "" (all) first,
// then builtins present in the items in canonical order, then custom types
// sorted alphabetically. ensure is always included even when no item has
// that type, so the active filter is representable as a (zero-count) chip.
func deriveFilterOptions(items []workitem.WorkItem, ensure string) []string {
	builtin := make(map[string]bool, len(builtinFilterTypes))
	for _, t := range builtinFilterTypes {
		builtin[t] = true
	}
	present := make(map[string]bool, len(items))
	for _, item := range items {
		if t := string(item.WorkflowType); t != "" {
			present[t] = true
		}
	}
	if ensure != "" {
		present[ensure] = true
	}
	opts := []string{""}
	for _, t := range builtinFilterTypes {
		if present[t] {
			opts = append(opts, t)
		}
	}
	var customs []string
	for t := range present {
		if !builtin[t] {
			customs = append(customs, t)
		}
	}
	sort.Strings(customs)
	return append(opts, customs...)
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
	typeFilter      string // interactive single-type override
	categoryFilter  string // interactive single-category override
	statusFilter    string // interactive displayed-status override
	initialFilters  workitem.FilterOptions
	limit           int
	categoryForType func(string) string
	showParked      bool
	filterMode      bool
	filterOptions   []string // chip values while filter mode is active; "" = all
	filterIndex     int
	savedTypeFilter string // snapshot of typeFilter when filter mode starts
	savedTypes      []string
	statusMode      bool
	statusOptions   []string
	statusIndex     int
	savedStatus     string
	savedStatuses   []string
	savedStages     []string
	savedAttention  []string

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

func (m *Model) SetCategoryResolver(fn func(string) string) {
	m.categoryForType = fn
}

// SetInitialFilters seeds visible, editable filters supplied by the command.
func (m *Model) SetInitialFilters(opts workitem.FilterOptions, limit int) {
	m.initialFilters = opts
	m.limit = limit
	m.showParked = opts.ShowParked
	m.searchQuery = opts.Query
	m.searchInput.SetValue(opts.Query)
	if len(opts.Types) == 1 {
		m.typeFilter = opts.Types[0]
		m.initialFilters.Types = nil
	}
	if len(opts.Categories) == 1 {
		m.categoryFilter = opts.Categories[0]
		m.initialFilters.Categories = nil
	}
	if len(opts.Statuses) == 1 {
		m.statusFilter = workitem.NormalizeDisplayStatus(opts.Statuses[0])
		m.initialFilters.Statuses = nil
	}
	m.refilter()
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
	m.rebuildFilterOptions()
	m.savedTypeFilter = m.typeFilter
	m.savedTypes = append([]string(nil), m.initialFilters.Types...)
	m.filterMode = true
}

func (m *Model) exitFilterMode() {
	m.filterMode = false
}

func (m Model) isFilterMode() bool {
	return m.filterMode
}

// rebuildFilterOptions derives chips from the visible item set; the active
// filter is always among the options, so the highlighted chip and the
// applied filter cannot diverge.
func (m *Model) rebuildFilterOptions() {
	m.filterOptions = deriveFilterOptions(m.visibleBaseItems(), m.typeFilter)
	m.filterIndex = 0
	for i, opt := range m.filterOptions {
		if opt == m.typeFilter {
			m.filterIndex = i
			break
		}
	}
}

// visibleBaseItems returns the rows visible before applying the type dimension,
// so type chips and counts still reflect every other active filter.
func (m Model) visibleBaseItems() []workitem.WorkItem {
	opts := m.filterOptionsForState(m.searchQuery)
	opts.Types = nil
	return workitem.FilterAdvanced(m.allItems, opts)
}

func (m Model) categoryBaseItems() []workitem.WorkItem {
	opts := m.filterOptionsForState(m.searchQuery)
	opts.Categories = nil
	return workitem.FilterAdvanced(m.allItems, opts)
}

func (m *Model) enterStatusMode() {
	m.statusOptions = deriveStatusOptions(m.visibleBaseItems(), m.statusFilter)
	m.statusIndex = 0
	for i, status := range m.statusOptions {
		if status == m.statusFilter {
			m.statusIndex = i
			break
		}
	}
	m.savedStatus = m.statusFilter
	m.savedStatuses = append([]string(nil), m.initialFilters.Statuses...)
	m.savedStages = append([]string(nil), m.initialFilters.LifecycleStages...)
	m.savedAttention = append([]string(nil), m.initialFilters.AttentionStages...)
	m.statusMode = true
}

func (m Model) isStatusMode() bool { return m.statusMode }

func deriveStatusOptions(_ []workitem.WorkItem, _ string) []string {
	opts := []string{""}
	return append(opts, workitem.DisplayStatusVocabulary()...)
}

// visibleTypeCounts tallies visible items per workflow type, returning the
// counts and the total.
func (m Model) visibleTypeCounts() (map[string]int, int) {
	counts := make(map[string]int)
	total := 0
	for _, item := range m.visibleBaseItems() {
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
	m.filteredItems = workitem.FilterAdvanced(m.allItems, m.filterOptionsForState(m.searchQuery))
	if m.limit > 0 && len(m.filteredItems) > m.limit {
		m.filteredItems = m.filteredItems[:m.limit]
	}
	if m.cursor >= len(m.filteredItems) {
		m.cursor = max(0, len(m.filteredItems)-1)
	}
	m.clampScroll()
}

func (m Model) filterOptionsForState(query string) workitem.FilterOptions {
	opts := m.initialFilters
	if m.typeFilter != "" {
		opts.Types = []string{m.typeFilter}
	}
	if m.categoryFilter != "" {
		opts.Categories = []string{m.categoryFilter}
	}
	if m.statusFilter != "" {
		opts.Statuses = []string{m.statusFilter}
	}
	opts.Query = query
	opts.ShowParked = m.showParked
	return opts
}

func (m *Model) cycleCategory() {
	opts := deriveCategoryOptions(m.categoryBaseItems(), m.categoryFilter)
	if len(opts) <= 1 {
		return
	}
	idx := 0
	for i, opt := range opts {
		if opt == m.categoryFilter {
			idx = i
			break
		}
	}
	m.categoryFilter = opts[(idx+1)%len(opts)]
	m.initialFilters.Categories = nil
	m.refilter()
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
