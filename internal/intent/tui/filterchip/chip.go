package filterchip

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Chip represents a single filterable dropdown chip.
type Chip struct {
	Label       string   // Display label (e.g., "Type", "Status")
	Options     []string // Available options
	Selected    int      // Currently selected index
	Focused     bool     // Whether this chip has keyboard focus
	Open        bool     // Whether dropdown is expanded
	highlighted int      // Currently highlighted option in dropdown
}

// NewChip creates a new filter chip.
func NewChip(label string, options []string) Chip {
	return Chip{
		Label:       label,
		Options:     options,
		Selected:    0,
		highlighted: 0,
	}
}

// SelectedValue returns the currently selected option value.
func (c Chip) SelectedValue() string {
	if len(c.Options) == 0 {
		return ""
	}
	return c.Options[c.Selected]
}

// SetSelected sets the selected index.
func (c *Chip) SetSelected(index int) {
	if index >= 0 && index < len(c.Options) {
		c.Selected = index
		c.highlighted = index
	}
}

// Focus gives the chip keyboard focus.
func (c *Chip) Focus() {
	c.Focused = true
}

// Blur removes keyboard focus from the chip.
func (c *Chip) Blur() {
	c.Focused = false
	c.Open = false
}

// Toggle opens or closes the dropdown.
func (c *Chip) Toggle() {
	c.Open = !c.Open
	if c.Open {
		c.highlighted = c.Selected
	}
}

// CloseDropdown closes the dropdown without changing selection.
func (c *Chip) CloseDropdown() {
	c.Open = false
}

// SelectHighlighted selects the currently highlighted option.
func (c *Chip) SelectHighlighted() bool {
	if c.highlighted >= 0 && c.highlighted < len(c.Options) {
		changed := c.Selected != c.highlighted
		c.Selected = c.highlighted
		c.Open = false
		return changed
	}
	return false
}

// Update handles keyboard input for the chip.
func (c Chip) Update(msg tea.Msg) (Chip, tea.Cmd) {
	if !c.Focused {
		return c, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		key := msg.String()

		if c.Open {
			return c.handleDropdownKey(key)
		}

		// Chip is focused but dropdown closed
		switch key {
		case "enter", " ":
			c.Toggle()
			return c, nil
		}
	}

	return c, nil
}

// handleDropdownKey handles keys when the dropdown is open.
func (c Chip) handleDropdownKey(key string) (Chip, tea.Cmd) {
	switch key {
	case "j", "down":
		if c.highlighted < len(c.Options)-1 {
			c.highlighted++
		}
	case "k", "up":
		if c.highlighted > 0 {
			c.highlighted--
		}
	case "enter", " ":
		if c.SelectHighlighted() {
			return c, c.changedCmd()
		}
		return c, nil
	case "esc":
		c.CloseDropdown()
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		// Quick-select by number
		idx := int(key[0] - '1')
		if idx >= 0 && idx < len(c.Options) {
			c.highlighted = idx
			if c.SelectHighlighted() {
				return c, c.changedCmd()
			}
		}
	}

	return c, nil
}

// changedCmd returns a command that sends a FilterChangedMsg.
func (c Chip) changedCmd() tea.Cmd {
	return func() tea.Msg {
		return FilterChangedMsg{
			Label: c.Label,
			Value: c.SelectedValue(),
			Index: c.Selected,
		}
	}
}

// IsActive returns true if the chip has a non-default selection.
func (c Chip) IsActive() bool {
	return c.Selected != 0
}

// View renders the chip.
func (c Chip) View() string {
	// Build chip content: "Label: Value ▾"
	value := c.SelectedValue()
	arrow := " ▾"

	content := labelStyle.Render(c.Label+": ") + valueStyle.Render(value) + arrowStyle.Render(arrow)

	// Choose style based on state
	var style lipgloss.Style
	switch {
	case c.Focused:
		style = chipFocusedStyle
	case c.IsActive():
		style = chipActiveStyle
	default:
		style = chipStyle
	}

	chipView := style.Render(content)

	// If dropdown is open, render it below
	if c.Open && c.Focused {
		dropdown := c.renderDropdown()
		return lipgloss.JoinVertical(lipgloss.Left, chipView, dropdown)
	}

	return chipView
}

// renderDropdown renders the dropdown options.
func (c Chip) renderDropdown() string {
	var lines []string

	for i, opt := range c.Options {
		// Number prefix for quick-select (1-9 only)
		numPrefix := "  "
		if i < 9 {
			numPrefix = numberStyle.Render(fmt.Sprintf("%d ", i+1))
		}

		// Current selection marker
		currentMarker := "  "
		if i == c.Selected {
			currentMarker = optionCurrentStyle.Render("• ")
		}

		// Option text
		var optText string
		if i == c.highlighted {
			optText = optionSelectedStyle.Render(opt)
		} else {
			optText = optionStyle.Render(opt)
		}

		lines = append(lines, numPrefix+currentMarker+optText)
	}

	return dropdownStyle.Render(strings.Join(lines, "\n"))
}

// Width returns the rendered width of the chip (closed state).
func (c Chip) Width() int {
	// Calculate content width: "Label: Value ▾" + padding + border
	value := c.SelectedValue()
	contentLen := len(c.Label) + 2 + len(value) + 2 // ": " + " ▾"
	return contentLen + 4                            // padding (2) + border (2)
}
