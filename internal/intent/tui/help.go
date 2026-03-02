// Package tui provides terminal UI components for intent management.
package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// helpContent defines all keyboard shortcuts for the intent explorer.
var helpContent = `
Intent Explorer Keyboard Shortcuts

NAVIGATION
  j/↓         Move down
  k/↑         Move up
  g           Go to top (preview/viewer)
  G           Go to bottom (preview/viewer)
  Ctrl+d      Half page down
  Ctrl+u      Half page up
  Enter       View intent / Toggle group
  Tab         Switch focus (list/preview)
  ←/→ h/l     Prev/next intent (in viewer)

QUICK ACTIONS
  .           Action menu
  f           View full screen
  e           Edit in $EDITOR
  o           Open with system handler
  O           Reveal in file manager
  n           New intent
  p           Promote to next status
  a           Archive intent
  d           Delete intent
  m           Move intent to status

GATHER (Multi-Select)
  Space       Select/deselect intent
  ga          Gather selected intents
  Escape      Exit multi-select mode

FILTERS
  /           Search intents (fuzzy)
  t           Filter by type
  s           Filter by status
  c           Filter by concept
  C           Clear concept filter
  Escape      Clear filters

VIEW
  v           Toggle preview pane
  ?           Show this help
  q           Quit explorer

Press ? or Escape to close
`

// HelpOverlay is a modal overlay that displays keyboard shortcuts.
type HelpOverlay struct {
	viewport viewport.Model
	width    int
	height   int
	visible  bool
}

// NewHelpOverlay creates a new help overlay with the given dimensions.
func NewHelpOverlay(width, height int) HelpOverlay {
	// Create viewport with padding
	vpWidth := width - 6 // Account for border and padding
	vpHeight := height - 6
	if vpWidth < 40 {
		vpWidth = 40
	}
	if vpHeight < 10 {
		vpHeight = 10
	}

	vp := viewport.New(vpWidth, vpHeight)
	vp.Style = lipgloss.NewStyle().Padding(0, 1)
	vp.SetContent(strings.TrimSpace(helpContent))

	return HelpOverlay{
		viewport: vp,
		width:    width,
		height:   height,
		visible:  true,
	}
}

// SetSize updates the help overlay dimensions.
func (h *HelpOverlay) SetSize(width, height int) {
	h.width = width
	h.height = height
	vpWidth := width - 6
	vpHeight := height - 6
	if vpWidth < 40 {
		vpWidth = 40
	}
	if vpHeight < 10 {
		vpHeight = 10
	}
	h.viewport.Width = vpWidth
	h.viewport.Height = vpHeight
}

// Show makes the help overlay visible.
func (h *HelpOverlay) Show() {
	h.visible = true
	h.viewport.GotoTop()
}

// Hide makes the help overlay invisible.
func (h *HelpOverlay) Hide() {
	h.visible = false
}

// IsVisible returns whether the help overlay is visible.
func (h HelpOverlay) IsVisible() bool {
	return h.visible
}

// Update handles keyboard input for the help overlay.
// Returns true if the overlay should be closed.
func (h HelpOverlay) Update(msg tea.Msg) (HelpOverlay, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "?", "esc", "escape", "q":
			// Signal to close help
			h.visible = false
			return h, nil, true
		case "j", "down":
			h.viewport.ScrollDown(1)
			return h, nil, false
		case "k", "up":
			h.viewport.ScrollUp(1)
			return h, nil, false
		case "ctrl+d":
			h.viewport.HalfPageDown()
			return h, nil, false
		case "ctrl+u":
			h.viewport.HalfPageUp()
			return h, nil, false
		case "g":
			h.viewport.GotoTop()
			return h, nil, false
		case "G":
			h.viewport.GotoBottom()
			return h, nil, false
		}
	}

	var cmd tea.Cmd
	h.viewport, cmd = h.viewport.Update(msg)
	return h, cmd, false
}

// Styles for the help overlay.
var helpBoxStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(pal.BorderFocus).
	Padding(1, 2)

// View renders the help overlay.
func (h HelpOverlay) View() string {
	if !h.visible {
		return ""
	}

	content := h.viewport.View()

	return helpBoxStyle.
		Width(h.width).
		Height(h.height).
		Align(lipgloss.Center, lipgloss.Center).
		Render(content)
}
