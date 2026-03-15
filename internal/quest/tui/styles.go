//go:build dev

package tui

import (
	"github.com/Obedience-Corp/camp/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
)

var pal = theme.TUI()

var (
	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(pal.Accent)

	HelpStyle = lipgloss.NewStyle().
			Foreground(pal.TextMuted)

	ErrorStyle = lipgloss.NewStyle().
			Foreground(pal.Error)

	SuccessStyle = lipgloss.NewStyle().
			Foreground(pal.Success)

	FieldLabelStyle = lipgloss.NewStyle().
			Foreground(pal.TextSecondary)

	FieldValueStyle = lipgloss.NewStyle().
			Foreground(pal.TextPrimary)
)
