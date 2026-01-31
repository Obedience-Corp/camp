package filterchip

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Bar manages a horizontal row of filter chips.
type Bar struct {
	Chips       []Chip // The filter chips
	FocusedChip int    // Which chip has focus (-1 = none/bar not focused)
	Width       int    // Available width for layout
}

// NewBar creates a new filter bar with the given chips.
func NewBar(chips ...Chip) Bar {
	return Bar{
		Chips:       chips,
		FocusedChip: -1,
	}
}

// SetWidth sets the available width for layout.
func (b *Bar) SetWidth(width int) {
	b.Width = width
}

// Focus gives the filter bar keyboard focus.
// Focuses the first chip.
func (b *Bar) Focus() {
	if len(b.Chips) > 0 {
		b.FocusedChip = 0
		b.Chips[0].Focus()
	}
}

// Blur removes keyboard focus from the filter bar.
func (b *Bar) Blur() {
	if b.FocusedChip >= 0 && b.FocusedChip < len(b.Chips) {
		b.Chips[b.FocusedChip].Blur()
	}
	b.FocusedChip = -1
}

// IsFocused returns true if the bar has focus.
func (b Bar) IsFocused() bool {
	return b.FocusedChip >= 0
}

// HasOpenDropdown returns true if any chip has an open dropdown.
func (b Bar) HasOpenDropdown() bool {
	for _, chip := range b.Chips {
		if chip.Open {
			return true
		}
	}
	return false
}

// FocusNext moves focus to the next chip.
func (b *Bar) FocusNext() {
	if len(b.Chips) == 0 {
		return
	}

	// Blur current chip
	if b.FocusedChip >= 0 && b.FocusedChip < len(b.Chips) {
		b.Chips[b.FocusedChip].Blur()
	}

	// Move to next
	b.FocusedChip++
	if b.FocusedChip >= len(b.Chips) {
		b.FocusedChip = 0
	}

	b.Chips[b.FocusedChip].Focus()
}

// FocusPrev moves focus to the previous chip.
func (b *Bar) FocusPrev() {
	if len(b.Chips) == 0 {
		return
	}

	// Blur current chip
	if b.FocusedChip >= 0 && b.FocusedChip < len(b.Chips) {
		b.Chips[b.FocusedChip].Blur()
	}

	// Move to previous
	b.FocusedChip--
	if b.FocusedChip < 0 {
		b.FocusedChip = len(b.Chips) - 1
	}

	b.Chips[b.FocusedChip].Focus()
}

// Chip returns a pointer to the chip at the given index.
func (b *Bar) Chip(index int) *Chip {
	if index >= 0 && index < len(b.Chips) {
		return &b.Chips[index]
	}
	return nil
}

// ChipByLabel returns a pointer to the chip with the given label.
func (b *Bar) ChipByLabel(label string) *Chip {
	for i := range b.Chips {
		if b.Chips[i].Label == label {
			return &b.Chips[i]
		}
	}
	return nil
}

// Update handles keyboard input for the filter bar.
func (b Bar) Update(msg tea.Msg) (Bar, tea.Cmd) {
	if b.FocusedChip < 0 || b.FocusedChip >= len(b.Chips) {
		return b, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		key := msg.String()
		chip := &b.Chips[b.FocusedChip]

		// If a dropdown is open, let the chip handle most keys
		if chip.Open {
			switch key {
			case "tab":
				// Tab closes dropdown and moves to next chip
				chip.CloseDropdown()
				b.FocusNext()
				return b, nil
			case "shift+tab":
				// Shift+Tab closes dropdown and moves to previous chip
				chip.CloseDropdown()
				b.FocusPrev()
				return b, nil
			default:
				// Let chip handle dropdown navigation
				newChip, cmd := chip.Update(msg)
				b.Chips[b.FocusedChip] = newChip
				return b, cmd
			}
		}

		// Dropdown is closed - handle bar-level navigation
		switch key {
		case "tab":
			b.FocusNext()
			return b, nil
		case "shift+tab":
			b.FocusPrev()
			return b, nil
		case "left", "h":
			b.FocusPrev()
			return b, nil
		case "right", "l":
			b.FocusNext()
			return b, nil
		default:
			// Pass to focused chip (enter/space to open dropdown)
			newChip, cmd := chip.Update(msg)
			b.Chips[b.FocusedChip] = newChip
			return b, cmd
		}
	}

	return b, nil
}

// View renders the filter bar.
func (b Bar) View() string {
	if len(b.Chips) == 0 {
		return ""
	}

	// Find if any chip has an open dropdown
	var openDropdownIdx = -1
	for i, chip := range b.Chips {
		if chip.Open {
			openDropdownIdx = i
			break
		}
	}

	// Render chips horizontally
	var chipViews []string
	for i, chip := range b.Chips {
		// If this chip has dropdown open, we render it specially
		if i == openDropdownIdx {
			chipViews = append(chipViews, chip.View())
		} else {
			// For non-open chips, render just the chip (no dropdown)
			// Create a copy with Open=false for rendering
			closedChip := chip
			closedChip.Open = false
			chipViews = append(chipViews, closedChip.View())
		}
	}

	// If a dropdown is open, we need to render differently
	// to show the dropdown below the appropriate chip
	if openDropdownIdx >= 0 {
		// Split views into before open, the open one, and after
		var before, after []string
		var openView string

		for i, view := range chipViews {
			if i < openDropdownIdx {
				// For chips before, strip any vertical content
				lines := strings.Split(view, "\n")
				before = append(before, lines[0])
			} else if i == openDropdownIdx {
				openView = view
			} else {
				// For chips after, strip any vertical content
				lines := strings.Split(view, "\n")
				after = append(after, lines[0])
			}
		}

		// Build the top row (all chips, but open one is just first line)
		openLines := strings.Split(openView, "\n")
		topParts := append(before, openLines[0])
		topParts = append(topParts, after...)
		topRow := lipgloss.JoinHorizontal(lipgloss.Top, intersperse(topParts, "  ")...)

		// If there's a dropdown (more lines), render it below
		if len(openLines) > 1 {
			dropdown := strings.Join(openLines[1:], "\n")
			// Calculate offset for dropdown positioning
			offset := 0
			for i := 0; i < openDropdownIdx; i++ {
				offset += lipgloss.Width(before[i]) + 2 // +2 for gap
			}
			// Pad dropdown to align under its chip
			if offset > 0 {
				dropdownLines := strings.Split(dropdown, "\n")
				paddedLines := make([]string, len(dropdownLines))
				for i, line := range dropdownLines {
					paddedLines[i] = strings.Repeat(" ", offset) + line
				}
				dropdown = strings.Join(paddedLines, "\n")
			}
			return topRow + "\n" + dropdown
		}

		return topRow
	}

	// No dropdown open - simple horizontal join
	return lipgloss.JoinHorizontal(lipgloss.Top, intersperse(chipViews, "  ")...)
}

// intersperse adds a separator between items.
func intersperse(items []string, sep string) []string {
	if len(items) <= 1 {
		return items
	}
	result := make([]string, len(items)*2-1)
	for i, item := range items {
		result[i*2] = item
		if i < len(items)-1 {
			result[i*2+1] = sep
		}
	}
	return result
}

// HasActiveFilters returns true if any chip has a non-default selection.
func (b Bar) HasActiveFilters() bool {
	for _, chip := range b.Chips {
		if chip.IsActive() {
			return true
		}
	}
	return false
}

// ClearAll resets all chips to their default (index 0) selection.
func (b *Bar) ClearAll() {
	for i := range b.Chips {
		b.Chips[i].SetSelected(0)
	}
}
