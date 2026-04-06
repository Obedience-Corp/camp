// Package tui provides a Bubble Tea dashboard for browsing campaign work items.
package tui

import (
	"context"
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

// typeFilterKeys maps keyboard shortcuts to workflow type filter values.
// Empty string means "show all".
var typeFilterKeys = map[string]string{
	"0": "",
	"1": "intent",
	"2": "design",
	"3": "explore",
	"4": "festival",
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
	typeFilter string // empty = all, or "intent"/"design"/"explore"/"festival"

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
}

// New creates the dashboard model from a pre-discovered item list.
func New(ctx context.Context, items []workitem.WorkItem, campaignRoot string, resolver *paths.Resolver, store *priority.Store, storePath string) Model {
	ti := textinput.New()
	ti.Placeholder = "search..."
	ti.CharLimit = 64

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
	}
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
	m.filteredItems = workitem.Filter(m.allItems, types, nil, m.searchQuery)
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
