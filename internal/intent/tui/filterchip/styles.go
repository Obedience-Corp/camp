package filterchip

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/obediencecorp/camp/internal/ui/theme"
)

var pal = theme.TUI()

// Chip styles
var (
	// Base chip style
	chipStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(pal.Border).
			Padding(0, 1)

	// Focused chip (has keyboard focus)
	chipFocusedStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(pal.BorderFocus).
				Padding(0, 1).
				Bold(true)

	// Active chip (has a non-default selection)
	chipActiveStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(pal.Accent).
			Padding(0, 1).
			Foreground(pal.Accent)

	// Dropdown container
	dropdownStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(pal.BorderFocus).
			Padding(0, 1)

	// Regular dropdown option
	optionStyle = lipgloss.NewStyle().
			Foreground(pal.TextSecondary)

	// Selected/highlighted option in dropdown
	optionSelectedStyle = lipgloss.NewStyle().
				Background(pal.BgSelected).
				Foreground(pal.TextPrimary).
				Bold(true)

	// Current selection indicator
	optionCurrentStyle = lipgloss.NewStyle().
				Foreground(pal.Accent)

	// Number prefix for quick-select
	numberStyle = lipgloss.NewStyle().
			Foreground(pal.TextMuted)

	// Label style
	labelStyle = lipgloss.NewStyle().
			Foreground(pal.TextSecondary)

	// Value style
	valueStyle = lipgloss.NewStyle().
			Foreground(pal.TextPrimary)

	// Dropdown indicator arrow
	arrowStyle = lipgloss.NewStyle().
			Foreground(pal.TextMuted)
)
