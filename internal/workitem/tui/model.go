// Package tui provides a Bubble Tea dashboard for browsing campaign work items.
package tui

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/workitem"
)

// Model is the Bubble Tea model for the workitem dashboard.
type Model struct {
	// Data
	allItems      []workitem.WorkItem
	filteredItems []workitem.WorkItem
	err           error

	// Navigation
	cursor int
	width  int
	height int
	ready  bool

	// Search
	searchMode  bool
	searchInput textinput.Model
	searchQuery string

	// Filters
	typeFilter string // empty = all, or "intent"/"design"/"explore"/"festival"

	// Preview
	showPreview    bool
	previewOverlay bool // narrow mode: overlay preview on top of list
	helpVisible    bool

	// Vim navigation
	lastKeyWasG bool

	// Selection result (read by command layer after Run)
	Selected *workitem.WorkItem

	// Refresh context — stored here because Bubble Tea's Update() receives
	// tea.Msg, not context.Context. The ctx is only used by refreshCmd().
	ctx          context.Context
	campaignRoot string
	resolver     *paths.Resolver
}

// New creates the dashboard model from a pre-discovered item list.
func New(ctx context.Context, items []workitem.WorkItem, campaignRoot string, resolver *paths.Resolver) Model {
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
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

// currentItem returns the work item under the cursor, or a zero-value item if empty.
func (m Model) currentItem() workitem.WorkItem {
	if len(m.filteredItems) == 0 || m.cursor >= len(m.filteredItems) {
		return workitem.WorkItem{}
	}
	return m.filteredItems[m.cursor]
}

// refilter applies current type filter and search query to allItems.
func (m *Model) refilter() {
	var types []string
	if m.typeFilter != "" {
		types = []string{m.typeFilter}
	}
	m.filteredItems = workitem.Filter(m.allItems, types, nil, m.searchQuery)
	if m.cursor >= len(m.filteredItems) {
		m.cursor = max(0, len(m.filteredItems)-1)
	}
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

func (m Model) refreshCmd() tea.Cmd {
	ctx := m.ctx
	root := m.campaignRoot
	resolver := m.resolver
	return func() tea.Msg {
		items, err := workitem.Discover(ctx, root, resolver)
		return refreshMsg{items: items, err: err}
	}
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
