// Package tui provides terminal UI components for intent management.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/obediencecorp/camp/internal/intent"
)

// IntentGroup represents a collapsible group of intents by status.
type IntentGroup struct {
	Name     string
	Status   intent.Status
	Intents  []*intent.Intent
	Expanded bool
}

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
	searching   bool // true when search input has focus

	// Filter state
	statusFilter *intent.Status
	typeFilter   *intent.Type

	// Display state
	width    int
	height   int
	ready    bool
	quitting bool

	// Status message
	statusMessage string
}

// NewExplorerModel creates a new Explorer model.
func NewExplorerModel(ctx context.Context, svc *intent.IntentService) ExplorerModel {
	ti := textinput.New()
	ti.Placeholder = "Search intents..."
	ti.CharLimit = 100
	ti.Width = 40

	return ExplorerModel{
		service:     svc,
		ctx:         ctx,
		cursorGroup: 0,
		cursorItem:  -1, // Start on first group header
		searchInput: ti,
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
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.searching {
			// Handle keys when search input has focus
			switch msg.String() {
			case "esc":
				m.searching = false
				m.searchInput.Blur()
				// Clear search and show all intents
				m.searchInput.SetValue("")
				m.applySearch()
				return m, nil
			case "enter":
				// Exit search mode but keep filter active
				m.searching = false
				m.searchInput.Blur()
				return m, nil
			}
			// Pass all other keys to the text input
			m.searchInput, cmd = m.searchInput.Update(msg)
			// Live update: apply search on every keystroke
			m.applySearch()
			return m, cmd
		}

		// Normal navigation mode
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "/":
			// Enter search mode
			m.searching = true
			m.searchInput.Focus()
			return m, textinput.Blink
		case "j", "down":
			m.moveCursorDown()
		case "k", "up":
			m.moveCursorUp()
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
		m.ready = true

	case intentsLoadedMsg:
		if msg.err != nil {
			m.statusMessage = "Error: " + msg.err.Error()
			return m, nil
		}
		m.intents = msg.intents
		m.filteredIntents = msg.intents
		m.groups = groupIntentsByStatus(msg.intents)
	}

	return m, nil
}

// applySearch filters intents using fuzzy search and rebuilds groups.
func (m *ExplorerModel) applySearch() {
	query := m.searchInput.Value()
	if query == "" {
		// No search, show all intents
		m.filteredIntents = m.intents
		m.statusMessage = ""
	} else {
		// Use fuzzy search via the service
		results, err := m.service.Search(m.ctx, query)
		if err != nil {
			m.statusMessage = "Search error: " + err.Error()
			// Fall back to showing all intents
			m.filteredIntents = m.intents
		} else {
			m.filteredIntents = results
			m.statusMessage = ""
		}
	}

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

	var b strings.Builder

	// Title
	b.WriteString(titleStyle.Render("Intent Explorer"))
	b.WriteString("\n")

	// Search input
	b.WriteString(m.searchInput.View())
	if m.searching {
		b.WriteString("  ")
		b.WriteString(helpStyle.Render("(enter to search, esc to cancel)"))
	}
	b.WriteString("\n\n")

	// Calculate available width for title (leave room for date and type)
	titleWidth := m.width - 30
	if titleWidth < 20 {
		titleWidth = 20
	}

	// Render groups
	for gi, group := range m.groups {
		// Group header
		indicator := "▶"
		if group.Expanded {
			indicator = "▼"
		}

		isGroupSelected := gi == m.cursorGroup && m.cursorItem == -1
		cursor := noCursor
		if isGroupSelected {
			cursor = cursorIndicator
		}

		header := fmt.Sprintf("%s %s %s (%d)", cursor, indicator, group.Name, len(group.Intents))
		if isGroupSelected {
			b.WriteString(groupHeaderSelectedStyle.Render(header))
		} else {
			b.WriteString(groupHeaderStyle.Render(header))
		}
		b.WriteString("\n")

		// Render items if expanded
		if group.Expanded {
			for ii, i := range group.Intents {
				isSelected := gi == m.cursorGroup && ii == m.cursorItem
				b.WriteString(m.renderIntentRow(i, isSelected, titleWidth))
				b.WriteString("\n")
			}
		}
	}

	// Status bar
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("j/k: navigate • enter/space: toggle • /: search • q: quit"))

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

	// Add project if present
	projectPart := ""
	if i.Project != "" {
		projectPart = " " + intentConceptStyle.Render(i.Project)
	}

	row := fmt.Sprintf("  %s  %s  %s  %s%s", cursor, titlePart, typePart, datePart, projectPart)

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
