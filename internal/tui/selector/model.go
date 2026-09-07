package selector

import (
	"slices"

	tea "github.com/charmbracelet/bubbletea"
)

type Mode int

const (
	ModeNavigate Mode = iota
	ModeFilter
)

const defaultVisible = 5

// Options configures a selector. Help is the single help line the selector owns.
type Options struct {
	Visible   int
	Help      string
	HelpShort string
	Title     string
	InitialID string
}

// Model is a type-to-filter recency wheel. Values are camp Bubble Tea style;
// SetItems, Reset, and SetSize use pointer receivers.
type Model struct {
	items      []Item
	visible    []Item
	opts       Options
	query      string
	mode       Mode
	cursor     int
	selectedID string
	consumed   bool
	width      int
	height     int
}

func New(items []Item, opts Options) Model {
	var m Model
	m.Reset(items, opts)
	return m
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	m.consumed = false
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	if neverConsumed(key) {
		return m, nil
	}
	if m.mode == ModeFilter {
		return m.updateFilter(key), nil
	}
	return m.updateNavigate(key), nil
}

func (m Model) Selected() (Item, bool) {
	if m.cursor < 0 || m.cursor >= len(m.visible) {
		return Item{}, false
	}
	return m.visible[m.cursor], true
}

func (m Model) Query() string   { return m.query }
func (m Model) Mode() Mode      { return m.mode }
func (m Model) Filtering() bool { return m.mode == ModeFilter }
func (m Model) Consumed() bool  { return m.consumed }

func (m *Model) SetItems(items []Item) {
	m.items = slices.Clone(items)
	*m = m.rebuildKeepID()
}

func (m *Model) Reset(items []Item, opts Options) {
	if opts.Visible <= 0 {
		opts.Visible = defaultVisible
	}
	m.items = slices.Clone(items)
	m.opts = opts
	m.query = ""
	m.mode = ModeNavigate
	m.consumed = false
	m.visible = Filter(m.items, "")
	m.cursor = seedCursor(m.visible, opts.InitialID)
	m.syncSelectedID()
}

func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m Model) rebuildKeepID() Model {
	m.visible = Filter(m.items, m.query)
	if m.selectedID != "" {
		for i, it := range m.visible {
			if it.ID == m.selectedID {
				m.cursor = i
				return m
			}
		}
	}
	m.cursor = seedCursor(m.visible, "")
	m.syncSelectedID()
	return m
}

func (m *Model) syncSelectedID() {
	if m.cursor >= 0 && m.cursor < len(m.visible) {
		m.selectedID = m.visible[m.cursor].ID
		return
	}
	m.selectedID = ""
}

func seedCursor(visible []Item, initialID string) int {
	if initialID != "" {
		for i, it := range visible {
			if it.ID == initialID {
				return i
			}
		}
	}
	lastRanked, lastUnpinned := -1, -1
	for i, it := range visible {
		if !it.Rank.IsZero() {
			lastRanked = i
		}
		if it.Pin == PinNone {
			lastUnpinned = i
		}
	}
	switch {
	case lastRanked >= 0:
		return lastRanked
	case lastUnpinned >= 0:
		return lastUnpinned
	case len(visible) == 0:
		return 0
	default:
		return len(visible) - 1
	}
}
