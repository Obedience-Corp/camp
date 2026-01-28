package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/obediencecorp/camp/internal/ui/theme"
)

// pal is the TUI color palette for adaptive theming.
var pal = theme.TUI()

// Style definitions for the Intent Explorer TUI.
var (
	// Title styling
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(pal.Accent)

	// Group header styles
	groupHeaderStyle = lipgloss.NewStyle().
				Foreground(pal.AccentAlt)

	groupHeaderSelectedStyle = lipgloss.NewStyle().
					Background(pal.BgSelected).
					Bold(true).
					Foreground(pal.AccentAlt)

	// Intent row styles
	intentRowStyle = lipgloss.NewStyle().
			PaddingLeft(4)

	intentRowSelectedStyle = lipgloss.NewStyle().
				PaddingLeft(4).
				Background(pal.BgSelected).
				Bold(true)

	// Intent field styles
	intentTitleStyle = lipgloss.NewStyle().
				Foreground(pal.TextPrimary)

	intentTypeStyle = lipgloss.NewStyle().
			Foreground(pal.TextSecondary)

	intentDateStyle = lipgloss.NewStyle().
			Foreground(pal.TextMuted)

	intentConceptStyle = lipgloss.NewStyle().
				Foreground(pal.Warning)

	// Help bar style
	helpStyle = lipgloss.NewStyle().
			Foreground(pal.TextMuted)

	// Error style
	errorStyle = lipgloss.NewStyle().
			Foreground(pal.Error)

	// Cursor indicator
	cursorIndicator = "›"
	noCursor        = " "
)
