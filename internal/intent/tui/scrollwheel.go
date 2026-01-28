package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	// maxVisibleItems is the maximum number of items shown in the scroll wheel
	maxVisibleItems = 5
)

// ScrollWheelSelectedMsg is sent when the selection changes.
type ScrollWheelSelectedMsg struct {
	Index int
	Value string
}

// ScrollWheel is a reusable scroll wheel component for picker-style selection.
// It displays up to 5 items at a time with the selected item centered.
type ScrollWheel struct {
	items    []string
	selected int
	focused  bool
	width    int
}

// NewScrollWheel creates a new scroll wheel with the given items.
func NewScrollWheel(items []string) ScrollWheel {
	return ScrollWheel{
		items:    items,
		selected: 0,
		width:    20,
	}
}

// SetItems updates the items in the scroll wheel.
func (sw *ScrollWheel) SetItems(items []string) {
	sw.items = items
	if sw.selected >= len(items) {
		sw.selected = max(0, len(items)-1)
	}
}

// SetSelected sets the selected index.
func (sw *ScrollWheel) SetSelected(index int) {
	if index >= 0 && index < len(sw.items) {
		sw.selected = index
	}
}

// Selected returns the currently selected index.
func (sw ScrollWheel) Selected() int {
	return sw.selected
}

// SelectedValue returns the currently selected value, or empty string if no items.
func (sw ScrollWheel) SelectedValue() string {
	if len(sw.items) == 0 {
		return ""
	}
	return sw.items[sw.selected]
}

// SetWidth sets the width of the scroll wheel.
func (sw *ScrollWheel) SetWidth(width int) {
	sw.width = width
}

// Focus gives the scroll wheel focus.
func (sw *ScrollWheel) Focus() {
	sw.focused = true
}

// Blur removes focus from the scroll wheel.
func (sw *ScrollWheel) Blur() {
	sw.focused = false
}

// Focused returns whether the scroll wheel has focus.
func (sw ScrollWheel) Focused() bool {
	return sw.focused
}

// Init implements tea.Model.
func (sw ScrollWheel) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (sw ScrollWheel) Update(msg tea.Msg) (ScrollWheel, tea.Cmd) {
	if !sw.focused || len(sw.items) == 0 {
		return sw, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			if sw.selected < len(sw.items)-1 {
				sw.selected++
				return sw, sw.selectionCmd()
			}
		case "k", "up":
			if sw.selected > 0 {
				sw.selected--
				return sw, sw.selectionCmd()
			}
		}
	}

	return sw, nil
}

// selectionCmd returns a command that sends a selection message.
func (sw ScrollWheel) selectionCmd() tea.Cmd {
	return func() tea.Msg {
		return ScrollWheelSelectedMsg{
			Index: sw.selected,
			Value: sw.items[sw.selected],
		}
	}
}

// View implements tea.Model.
func (sw ScrollWheel) View() string {
	if len(sw.items) == 0 {
		return ""
	}

	// Calculate visible range centered on selected item
	start, end := sw.visibleRange()

	// Styles
	normalStyle := lipgloss.NewStyle().Width(sw.width)
	selectedStyle := lipgloss.NewStyle().
		Width(sw.width).
		Bold(true).
		Foreground(pal.Accent)

	if sw.focused {
		selectedStyle = selectedStyle.Background(pal.BgSelected)
	}

	// Build view
	var result string
	for i := start; i < end; i++ {
		item := sw.items[i]

		// Add selection indicator
		prefix := "  "
		if i == sw.selected {
			prefix = "> "
		}

		line := prefix + item
		if i == sw.selected {
			result += selectedStyle.Render(line) + "\n"
		} else {
			result += normalStyle.Render(line) + "\n"
		}
	}

	return result
}

// visibleRange returns the start and end indices of visible items.
func (sw ScrollWheel) visibleRange() (int, int) {
	n := len(sw.items)
	if n <= maxVisibleItems {
		return 0, n
	}

	// Try to center selected item
	halfVisible := maxVisibleItems / 2

	start := sw.selected - halfVisible
	if start < 0 {
		start = 0
	}

	end := start + maxVisibleItems
	if end > n {
		end = n
		start = n - maxVisibleItems
	}

	return start, end
}
