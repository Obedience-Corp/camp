package tui

import "github.com/charmbracelet/lipgloss"

// Style definitions for the Intent Explorer TUI.
var (
	// Title styling
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205"))

	// Group header styles
	groupHeaderStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("110"))

	groupHeaderSelectedStyle = lipgloss.NewStyle().
					Background(lipgloss.Color("237")).
					Bold(true).
					Foreground(lipgloss.Color("110"))

	// Intent row styles
	intentRowStyle = lipgloss.NewStyle().
			PaddingLeft(4)

	intentRowSelectedStyle = lipgloss.NewStyle().
				PaddingLeft(4).
				Background(lipgloss.Color("237")).
				Bold(true)

	// Intent field styles
	intentTitleStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("255"))

	intentTypeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))

	intentDateStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	intentConceptStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("214"))

	// Help bar style
	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	// Error style
	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	// Cursor indicator
	cursorIndicator = "›"
	noCursor        = " "
)
