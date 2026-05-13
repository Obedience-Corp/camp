package tui

import (
	"time"

	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
	"github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/charmbracelet/lipgloss"
)

// pal is the TUI color palette for adaptive theming.
var pal = theme.TUI()

// Header/footer chrome
var (
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	footerStyle = lipgloss.NewStyle().Foreground(pal.TextMuted)
)

// Row styles
var (
	rowSelectedStyle = lipgloss.NewStyle().Background(pal.BgSelected).Bold(true)
	rowTitleStyle    = lipgloss.NewStyle().Foreground(pal.TextPrimary)
)

// Preview pane styles
var (
	previewTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	previewSepStyle   = lipgloss.NewStyle().Foreground(pal.Border)
	previewLabelStyle = lipgloss.NewStyle().Foreground(pal.TextMuted)
	previewValueStyle = lipgloss.NewStyle().Foreground(pal.TextSecondary)
	previewHelpStyle  = lipgloss.NewStyle().Foreground(pal.TextDim)
)

// Recency styles
var (
	recencyRecentStyle = lipgloss.NewStyle().Foreground(pal.TextSecondary)
	recencyOldStyle    = lipgloss.NewStyle().Foreground(pal.TextDim)
)

// Status message styles
var (
	statusSuccessStyle = lipgloss.NewStyle().Foreground(pal.Success)
	statusErrorStyle   = lipgloss.NewStyle().Foreground(pal.Error)
)

// Filter pill styles
var (
	filterActiveStyle = lipgloss.NewStyle().Foreground(pal.Accent).Bold(true)
)

// Help screen styles
var (
	helpTitleStyle   = lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	helpSectionStyle = lipgloss.NewStyle().Bold(true).Foreground(pal.AccentAlt)
	helpKeyStyle     = lipgloss.NewStyle().Foreground(pal.TextSecondary)
	helpDescStyle    = lipgloss.NewStyle().Foreground(pal.TextMuted)
)

// Priority badge styles
var (
	priorityHighStyle   = lipgloss.NewStyle().Bold(true).Foreground(pal.Error)
	priorityMediumStyle = lipgloss.NewStyle().Bold(true).Foreground(pal.Warning)
	priorityLowStyle    = lipgloss.NewStyle().Foreground(pal.TextDim)
)

// Execution metadata badge styles (added in WW0001/005.01).
var (
	executionBlockedStyle      = lipgloss.NewStyle().Bold(true).Foreground(pal.Error)
	executionRiskCriticalStyle = lipgloss.NewStyle().Bold(true).Foreground(pal.Error)
	executionRiskHighStyle     = lipgloss.NewStyle().Foreground(pal.Warning)
	executionAutonomyStyle     = lipgloss.NewStyle().Foreground(pal.TextDim)
)

// Empty state
var (
	emptyMsgStyle = lipgloss.NewStyle().Foreground(pal.Warning)
)

// workflowStyle returns the style for a workflow type badge.
func workflowStyle(wfType workitem.WorkflowType) lipgloss.Style {
	return ui.GetWorkflowTypeStyle(string(wfType))
}

// stageStyle returns the style for a lifecycle stage badge.
func stageStyle(stage string) lipgloss.Style {
	return ui.GetLifecycleStageStyle(stage)
}

// recencyStyle returns the appropriate style based on how old a timestamp is.
func recencyStyle(t time.Time) lipgloss.Style {
	if t.IsZero() {
		return recencyOldStyle
	}
	if time.Since(t) < 24*time.Hour {
		return recencyRecentStyle
	}
	return recencyOldStyle
}
