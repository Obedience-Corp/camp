package ui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// SetNoColor configures lipgloss to disable colors when requested
func SetNoColor(noColor bool) {
	if noColor {
		lipgloss.SetColorProfile(termenv.Ascii)
		return
	}
	lipgloss.SetColorProfile(termenv.EnvColorProfile())
}

// HasColorSupport returns true if the terminal supports colors
func HasColorSupport() bool {
	return termenv.EnvColorProfile() != termenv.Ascii
}
