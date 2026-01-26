// Package tui provides terminal UI components for intent management.
package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/list"
	"github.com/obediencecorp/camp/internal/intent"
)

// ExplorerModel is the main model for the Intent Explorer TUI.
// It follows the BubbleTea Elm Architecture pattern.
type ExplorerModel struct {
	// Data
	intents         []*intent.Intent
	filteredIntents []*intent.Intent
	service         *intent.IntentService
	ctx             context.Context

	// Selection state
	list   list.Model
	cursor int

	// Search and filter state
	searchQuery  string
	statusFilter *intent.Status
	typeFilter   *intent.Type
	conceptFilter string

	// Display state
	width    int
	height   int
	ready    bool
	quitting bool

	// Status message
	statusMessage string
}

// intentItem implements list.Item for rendering intents in the list.
type intentItem struct {
	intent *intent.Intent
}

func (i intentItem) FilterValue() string { return i.intent.Title }
func (i intentItem) Title() string       { return i.intent.Title }
func (i intentItem) Description() string { return string(i.intent.Status) + " | " + string(i.intent.Type) }

// NewExplorerModel creates a new Explorer model.
func NewExplorerModel(ctx context.Context, svc *intent.IntentService) ExplorerModel {
	// Create list delegate
	delegate := list.NewDefaultDelegate()

	// Create empty list - will be populated in Init
	l := list.New([]list.Item{}, delegate, 0, 0)
	l.Title = "Intent Explorer"
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(true)

	return ExplorerModel{
		service: svc,
		ctx:     ctx,
		list:    l,
	}
}

// intentsLoadedMsg is sent when intents are loaded from the service.
type intentsLoadedMsg struct {
	intents []*intent.Intent
	err     error
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
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.list.SetSize(msg.Width, msg.Height-2)
		m.ready = true

	case intentsLoadedMsg:
		if msg.err != nil {
			m.statusMessage = "Error: " + msg.err.Error()
			return m, nil
		}
		m.intents = msg.intents
		m.filteredIntents = msg.intents
		m.updateListItems()
	}

	// Update the list component
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// updateListItems refreshes the list items from filteredIntents.
func (m *ExplorerModel) updateListItems() {
	items := make([]list.Item, len(m.filteredIntents))
	for i, intent := range m.filteredIntents {
		items[i] = intentItem{intent: intent}
	}
	m.list.SetItems(items)
}

// View implements tea.Model.
func (m ExplorerModel) View() string {
	if m.quitting {
		return ""
	}
	if !m.ready {
		return "Loading..."
	}
	return m.list.View()
}

// SelectedIntent returns the currently selected intent, or nil if none.
func (m ExplorerModel) SelectedIntent() *intent.Intent {
	if item, ok := m.list.SelectedItem().(intentItem); ok {
		return item.intent
	}
	return nil
}
