package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/obediencecorp/camp/internal/intent"
)

// Styles for the intent viewer.
var (
	viewerBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(pal.BorderFocus).
			Padding(0, 1)

	viewerTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(pal.Accent)

	viewerBadgeStyle = lipgloss.NewStyle().
				Foreground(pal.TextSecondary)

	viewerMetaStyle = lipgloss.NewStyle().
			Foreground(pal.TextMuted)

	viewerFooterStyle = lipgloss.NewStyle().
				Foreground(pal.TextMuted)
)

// View implements tea.Model.
func (m IntentViewerModel) View() string {
	if !m.ready {
		return "Loading..."
	}

	// Handle overlays
	if m.showConfirm {
		return m.viewWithConfirmOverlay()
	}
	if m.moveOverlay {
		return m.viewWithMoveOverlay()
	}
	if m.showGatherTitle {
		return m.viewWithGatherTitleOverlay()
	}
	if m.gatherOverlay {
		return m.viewWithGatherSimilarOverlay()
	}

	var b strings.Builder

	// Header
	b.WriteString(m.renderHeader())
	b.WriteString("\n")

	// Separator
	separator := lipgloss.NewStyle().
		Foreground(pal.Border).
		Render(strings.Repeat("─", m.width-2))
	b.WriteString(separator)
	b.WriteString("\n")

	// Content viewport
	b.WriteString(m.viewport.View())
	b.WriteString("\n")

	// Footer
	b.WriteString(separator)
	b.WriteString("\n")
	b.WriteString(m.renderFooter())

	return viewerBoxStyle.
		Width(m.width).
		Height(m.height).
		Render(b.String())
}

// renderHeader renders the header with intent metadata.
func (m IntentViewerModel) renderHeader() string {
	// DEBUG: Show path and content info
	debugInfo := fmt.Sprintf("[DEBUG: Path=%s ContentLen=%d ViewportH=%d]",
		m.intent.Path, len(m.content), m.viewport.Height)

	// Title line
	title := viewerTitleStyle.Render(m.intent.Title + "\n" + debugInfo)

	// Metadata line
	typeBadge := viewerBadgeStyle.Render(fmt.Sprintf("[%s]", m.intent.Type))
	statusBadge := m.renderStatusBadge(m.intent.Status)
	concept := viewerMetaStyle.Render(m.intent.ConceptName())
	date := viewerMetaStyle.Render(formatRelativeTime(m.intent.CreatedAt))

	// Adjust based on width
	if m.width < 60 {
		// Minimal header for narrow terminals
		return title
	}

	meta := fmt.Sprintf("Type: %s  Status: %s  Concept: %s  Created: %s",
		typeBadge, statusBadge, concept, date)

	return lipgloss.JoinVertical(lipgloss.Left, title, meta)
}

// renderStatusBadge renders a colored status badge.
func (m IntentViewerModel) renderStatusBadge(s intent.Status) string {
	return renderStatusBadge(s)
}

// renderStatusBadge renders a colored status badge (shared helper).
func renderStatusBadge(s intent.Status) string {
	var color lipgloss.TerminalColor
	switch s {
	case intent.StatusInbox:
		color = pal.Warning // Orange
	case intent.StatusActive:
		color = pal.Success // Green
	case intent.StatusReady:
		color = pal.AccentAlt // Blue
	case intent.StatusDone:
		color = pal.TextMuted // Gray
	case intent.StatusKilled:
		color = pal.Error // Red
	default:
		color = pal.TextMuted
	}
	return lipgloss.NewStyle().Foreground(color).Render(s.String())
}

// renderFooter renders the footer with actions and scroll position.
func (m IntentViewerModel) renderFooter() string {
	// Actions - include gather if gather service is available
	actions := "[e]dit  [m]ove  [p]romote  [a]rchive  [d]elete  [o]pen  [O] reveal"
	if m.gatherSvc != nil {
		actions = "[e]dit  [g]ather  [m]ove  [p]romote  [a]rchive  [d]elete  [o]pen  [O] reveal"
	}

	// Scroll percentage
	scrollPct := int(m.viewport.ScrollPercent() * 100)
	scrollInfo := fmt.Sprintf("%d%%", scrollPct)

	// Position indicator (only if multiple siblings for navigation)
	var posInfo string
	if len(m.siblings) > 1 {
		posInfo = fmt.Sprintf("%d/%d │ ", m.currentIndex+1, len(m.siblings))
	}

	// Navigation hint - show arrow keys when navigation is available
	var navHint string
	if len(m.siblings) > 1 {
		navHint = "←/→: prev/next • q: back"
	} else {
		navHint = "q: back to list"
	}

	// Calculate spacing
	actionsWidth := lipgloss.Width(actions)
	scrollWidth := lipgloss.Width(scrollInfo)
	posWidth := lipgloss.Width(posInfo)
	navWidth := lipgloss.Width(navHint)
	padding := m.width - actionsWidth - scrollWidth - posWidth - navWidth - 10

	if padding < 0 {
		// Narrow terminal - minimal footer with position
		return viewerFooterStyle.Render(fmt.Sprintf("%s%s │ %s", posInfo, scrollInfo, navHint))
	}

	spacer := strings.Repeat(" ", padding)
	return viewerFooterStyle.Render(fmt.Sprintf("%s%s%s%s │ %s", actions, spacer, posInfo, scrollInfo, navHint))
}

// viewWithConfirmOverlay renders the view with confirmation dialog overlay.
func (m IntentViewerModel) viewWithConfirmOverlay() string {
	var b strings.Builder
	b.WriteString(viewerTitleStyle.Render("Intent Viewer"))
	b.WriteString("\n\n")
	b.WriteString(m.confirmDialog.View())
	return b.String()
}

// viewWithMoveOverlay renders the view with move status overlay.
func (m IntentViewerModel) viewWithMoveOverlay() string {
	var b strings.Builder

	b.WriteString(viewerTitleStyle.Render("Move Intent"))
	b.WriteString("\n\n")
	b.WriteString("Moving: " + m.intent.Title + "\n")
	b.WriteString("Current status: " + m.intent.Status.String() + "\n\n")
	b.WriteString("Select new status:\n")

	for i, opt := range moveStatusOptions {
		cursor := "  "
		if i == m.moveStatusIdx {
			cursor = "> "
		}
		marker := ""
		if m.intent.Status == opt.status {
			marker = " (current)"
		}
		b.WriteString(cursor + opt.name + marker + "\n")
	}

	b.WriteString("\n")
	b.WriteString(HelpStyle.Render("j/k: navigate • Enter: move • Esc: cancel"))

	return b.String()
}

// viewWithGatherSimilarOverlay renders the view with gather-similar selection overlay.
func (m IntentViewerModel) viewWithGatherSimilarOverlay() string {
	var b strings.Builder

	b.WriteString(viewerTitleStyle.Render("Gather Similar Intents"))
	b.WriteString("\n\n")
	b.WriteString("Current: " + m.intent.Title + "\n\n")
	b.WriteString("Select similar intents to gather:\n")

	if len(m.similarIntents) == 0 {
		b.WriteString(HelpStyle.Render("  No similar intents found.\n"))
	} else {
		for i, sim := range m.similarIntents {
			cursor := "  "
			if i == m.gatherCursorIdx {
				cursor = "> "
			}
			checkbox := "[ ]"
			if m.selectedSimilar[sim.Intent.ID] {
				checkbox = "[x]"
			}
			// Show title and similarity score
			title := sim.Intent.Title
			if len(title) > 40 {
				title = title[:37] + "..."
			}
			score := fmt.Sprintf("%.0f%%", sim.Score*100)
			b.WriteString(fmt.Sprintf("%s%s %s (%s)\n", cursor, checkbox, title, score))
		}
	}

	selectedCount := len(m.selectedSimilar)
	b.WriteString("\n")
	if selectedCount > 0 {
		b.WriteString(fmt.Sprintf("Selected: %d intent(s)\n", selectedCount))
	}
	b.WriteString("\n")
	b.WriteString(HelpStyle.Render("j/k: navigate • Space: toggle • Enter: proceed • Esc: cancel"))

	return b.String()
}

// viewWithGatherTitleOverlay renders the gather dialog for title input.
func (m IntentViewerModel) viewWithGatherTitleOverlay() string {
	var b strings.Builder

	b.WriteString(viewerTitleStyle.Render("Gather Intents"))
	b.WriteString("\n\n")
	b.WriteString(m.gatherDialog.View())

	return b.String()
}

// truncate truncates a string to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
