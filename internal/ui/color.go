package ui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/Obedience-Corp/camp/internal/ui/theme"
)

// SetNoColor configures lipgloss to disable colors when requested
func SetNoColor(noColor bool) {
	theme.SetNoColor(noColor)
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
