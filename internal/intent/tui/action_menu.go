// Package tui provides terminal UI components for intent management.
package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/obediencecorp/camp/internal/intent"
)

// ActionMenuItem represents an item in the action menu.
type ActionMenuItem struct {
	Label   string
	Action  string // "view", "edit", "move", "promote", "archive", "delete", "open", "reveal"
	Enabled bool
}

// ActionMenu is a modal action menu that appears when Enter is pressed on an intent.
type ActionMenu struct {
	items       []ActionMenuItem
	selectedIdx int
	visible     bool
	width       int
}

// ActionMenuSelectedMsg is sent when an action is selected.
type ActionMenuSelectedMsg struct {
	Action string
}

// ActionMenuCancelledMsg is sent when the menu is cancelled.
type ActionMenuCancelledMsg struct{}

// NewActionMenu creates a new action menu for the given intent.
func NewActionMenu(i *intent.Intent) ActionMenu {
	items := []ActionMenuItem{
		{Label: "View full screen", Action: "view", Enabled: true},
		{Label: "Edit in editor", Action: "edit", Enabled: true},
		{Label: "Move to status", Action: "move", Enabled: true},
		{Label: "Promote", Action: "promote", Enabled: i.Status != intent.StatusDone},
		{Label: "Archive", Action: "archive", Enabled: i.Status != intent.StatusKilled},
		{Label: "Delete", Action: "delete", Enabled: true},
	}

	// Find first enabled item
	selectedIdx := 0
	for idx, item := range items {
		if item.Enabled {
			selectedIdx = idx
			break
		}
	}

	return ActionMenu{
		items:       items,
		selectedIdx: selectedIdx,
		visible:     true,
		width:       26,
	}
}

// Update handles keyboard input for the action menu.
func (m ActionMenu) Update(msg tea.Msg) (ActionMenu, tea.Cmd) {
	if !m.visible {
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "q":
			m.visible = false
			return m, func() tea.Msg { return ActionMenuCancelledMsg{} }

		case "j", "down":
			m.moveDown()

		case "k", "up":
			m.moveUp()

		case "enter":
			if m.items[m.selectedIdx].Enabled {
				m.visible = false
				action := m.items[m.selectedIdx].Action
				return m, func() tea.Msg { return ActionMenuSelectedMsg{Action: action} }
			}
		}
	}

	return m, nil
}

// moveDown moves selection to the next enabled item.
func (m *ActionMenu) moveDown() {
	for i := m.selectedIdx + 1; i < len(m.items); i++ {
		if m.items[i].Enabled {
			m.selectedIdx = i
			return
		}
	}
}

// moveUp moves selection to the previous enabled item.
func (m *ActionMenu) moveUp() {
	for i := m.selectedIdx - 1; i >= 0; i-- {
		if m.items[i].Enabled {
			m.selectedIdx = i
			return
		}
	}
}

// IsVisible returns whether the menu is visible.
func (m ActionMenu) IsVisible() bool {
	return m.visible
}

// View renders the action menu.
func (m ActionMenu) View() string {
	if !m.visible {
		return ""
	}

	var b strings.Builder

	// Title
	title := actionMenuTitleStyle.Render("─ Actions ─")
	b.WriteString(title)
	b.WriteString("\n")

	// Menu items
	for i, item := range m.items {
		var line string
		if i == m.selectedIdx {
			if item.Enabled {
				line = actionMenuSelectedStyle.Render("● " + item.Label)
			} else {
				line = actionMenuDisabledSelectedStyle.Render("○ " + item.Label)
			}
		} else {
			if item.Enabled {
				line = actionMenuItemStyle.Render("  " + item.Label)
			} else {
				line = actionMenuDisabledStyle.Render("  " + item.Label)
			}
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	return actionMenuBoxStyle.
		Width(m.width).
		Render(b.String())
}

// Styles for the action menu.
var (
	actionMenuBoxStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("205")).
				Padding(0, 1)

	actionMenuTitleStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("205")).
				Bold(true)

	actionMenuItemStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("255"))

	actionMenuSelectedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("205")).
				Bold(true)

	actionMenuDisabledStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("241"))

	actionMenuDisabledSelectedStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("241")).
					Bold(true)
)
