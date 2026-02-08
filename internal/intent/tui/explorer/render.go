package explorer

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/obediencecorp/camp/internal/intent"
	"github.com/obediencecorp/camp/internal/intent/tui"
)

// renderIntentRow renders a single intent row with proper formatting.
// Layout is responsive based on terminal width.
func (m *Model) renderIntentRow(i *intent.Intent, isSelected bool, maxTitleWidth int) string {
	cursor := tui.NoCursor
	if isSelected {
		cursor = tui.CursorIndicator
	}

	// Checkbox for multi-select mode
	checkbox := ""
	if m.multiSelectMode {
		if m.selectedIntents[i.ID] {
			checkbox = tui.CheckboxCheckedStyle.Render("[x] ")
		} else {
			checkbox = tui.CheckboxUncheckedStyle.Render("[ ] ")
		}
	}

	// Truncate title if needed (account for checkbox width in multi-select)
	effectiveTitleWidth := maxTitleWidth
	if m.multiSelectMode {
		effectiveTitleWidth -= 3 // checkbox takes space
	}
	title := i.Title
	if len(title) > effectiveTitleWidth {
		title = title[:effectiveTitleWidth-3] + "..."
	}

	// Format date
	date := formatRelativeTime(i.CreatedAt)

	// Build row parts
	titlePart := tui.IntentTitleStyle.Render(title)
	typePart := tui.IntentTypeStyle.Render(fmt.Sprintf("[%s]", i.Type))
	datePart := tui.IntentDateStyle.Render(date)

	var row string

	switch m.layoutMode {
	case layoutNarrow:
		// Minimal: cursor, checkbox, title, type, date (no concept)
		row = fmt.Sprintf("  %s %s%s  %s  %s", cursor, checkbox, titlePart, typePart, datePart)

	case layoutNormal:
		// Normal: cursor, checkbox, title, type, date, concept (truncated)
		conceptName := "-"
		if i.Concept != "" {
			conceptName = i.ConceptName()
			if len(conceptName) > 15 {
				conceptName = conceptName[:12] + "..."
			}
		}
		conceptPart := tui.IntentConceptStyle.Render(conceptName)
		row = fmt.Sprintf("  %s %s%s  %s  %s  %s", cursor, checkbox, titlePart, typePart, datePart, conceptPart)

	case layoutWide:
		// Wide: cursor, checkbox, title, type, date, full concept path
		concept := "-"
		if i.Concept != "" {
			if m.fullConceptPaths {
				concept = i.Concept
			} else {
				concept = i.ConceptName()
			}
		}
		conceptPart := tui.IntentConceptStyle.Render(concept)
		row = fmt.Sprintf("  %s %s%s  %s  %s  %s", cursor, checkbox, titlePart, typePart, datePart, conceptPart)
	}

	if isSelected {
		return tui.IntentRowSelectedStyle.Render(row)
	}
	return tui.IntentRowStyle.Render(row)
}

// renderStatusBar renders the status bar adapted to terminal width.
func (m *Model) renderStatusBar() string {
	// Scroll indicator when list is scrollable
	scrollIndicator := ""
	if m.listHeight > 0 && m.totalVisualLines() > m.listHeight {
		total := m.totalVisualLines() - m.listHeight
		pct := 0
		if total > 0 {
			pct = m.scrollOffset * 100 / total
		}
		scrollIndicator = tui.HelpStyle.Render(fmt.Sprintf("[%d%%] ", pct))
	}

	// Add multi-select mode hints
	if m.multiSelectMode {
		count := len(m.selectedIntents)
		switch m.layoutMode {
		case layoutNarrow:
			return scrollIndicator + tui.HelpStyle.Render(fmt.Sprintf("Space: select . Ctrl-g: gather (%d) . Esc: cancel", count))
		default:
			return scrollIndicator + tui.HelpStyle.Render(fmt.Sprintf("Space: toggle select . Ctrl-g: gather %d intents . Esc: exit multi-select . ?: help", count))
		}
	}

	switch m.layoutMode {
	case layoutNarrow:
		return scrollIndicator + tui.HelpStyle.Render("j/k . v . / . tab: filter . n . ? . q")
	case layoutNormal:
		if m.shouldShowPreview() {
			return scrollIndicator + tui.HelpStyle.Render("j/k: nav . v: hide preview . tab: focus . /: search . n: new . q: quit")
		}
		return scrollIndicator + tui.HelpStyle.Render("j/k: nav . v: preview . /: search . tab: filter . Space: gather mode . q: quit")
	case layoutWide:
		if m.shouldShowPreview() {
			return scrollIndicator + tui.HelpStyle.Render("j/k: navigate . v: hide preview . tab: switch focus . /: search . f: full view . n: new . ?: help . q: quit")
		}
		return scrollIndicator + tui.HelpStyle.Render("j/k: navigate . v: preview . /: search . tab: filter . Space: gather mode . f: full . ?: help . q: quit")
	}
	return ""
}

// totalVisualLines returns the total number of visual lines in the list.
func (m *Model) totalVisualLines() int {
	lines := 0
	for _, group := range m.groups {
		lines++ // group header
		if group.Expanded {
			lines += len(group.Intents)
		}
	}
	return lines
}

// renderFilterBarComponent renders the interactive filter bar component.
func (m *Model) renderFilterBarComponent() string {
	return m.filterBar.View()
}

// renderActiveFilters renders a summary of active filters (pills below filter bar).
// Returns empty string if no filters are active and not in multi-select mode.
func (m *Model) renderActiveFilters() string {
	var pills []string

	// Check for concept filter (not in the filter bar chips)
	if m.conceptFilterPath != "" {
		conceptName := m.conceptFilterPath
		// Show just the last part for brevity
		parts := strings.Split(m.conceptFilterPath, "/")
		if len(parts) > 0 {
			conceptName = parts[len(parts)-1]
		}
		pills = append(pills, tui.FilterPillStyle.Render(fmt.Sprintf("concept:%s", conceptName)))
	}

	// Check for active search
	if m.searchInput.Value() != "" && m.focus != focusSearch {
		query := m.searchInput.Value()
		if len(query) > 15 {
			query = query[:12] + "..."
		}
		pills = append(pills, tui.FilterPillStyle.Render(fmt.Sprintf("search:%s", query)))
	}

	// Build the active filters summary
	var parts []string

	// Selection count badge (always show when in multi-select)
	if m.multiSelectMode {
		count := len(m.selectedIntents)
		parts = append(parts, tui.SelectionCountStyle.Render(fmt.Sprintf("%d selected", count)))
	}

	// Filter pills (for things not in the filter bar)
	if len(pills) > 0 {
		parts = append(parts, strings.Join(pills, " "))
	}

	if len(parts) == 0 {
		return ""
	}

	return strings.Join(parts, "  ")
}

// viewActionMenu renders the main view with action menu overlay.
func (m *Model) viewActionMenu() string {
	var b strings.Builder
	b.WriteString(tui.TitleStyle.Render("Intent Explorer"))
	b.WriteString("\n\n")

	if selected := m.SelectedIntent(); selected != nil {
		b.WriteString("Selected: " + selected.Title + "\n\n")
	}

	b.WriteString(m.actionMenu.View())
	b.WriteString("\n\n")
	b.WriteString(tui.HelpStyle.Render("j/k: navigate . Enter: select . Esc: cancel"))

	return b.String()
}

// viewConceptFilter renders the concept filter picker.
func (m *Model) viewConceptFilter() string {
	var b strings.Builder

	b.WriteString(tui.TitleStyle.Render("Filter by Concept"))
	b.WriteString("\n\n")
	b.WriteString(m.conceptFilterPicker.View())
	b.WriteString("\n")
	b.WriteString(tui.HelpStyle.Render("Esc: cancel"))

	return b.String()
}

// viewHelp renders the help overlay.
func (m *Model) viewHelp() string {
	return m.helpOverlay.View()
}

// viewGatherDialog renders the gather dialog overlay.
func (m *Model) viewGatherDialog() string {
	var b strings.Builder

	b.WriteString(tui.TitleStyle.Render("Gather Intents"))
	b.WriteString("\n\n")
	b.WriteString(m.gatherDialog.View())

	return b.String()
}

// buildMainView renders the main explorer view with groups and intents.
func (m *Model) buildMainView() string {
	var b strings.Builder

	// Title with optional selection count
	b.WriteString(tui.TitleStyle.Render("Intent Explorer"))
	if m.multiSelectMode && len(m.selectedIntents) > 0 {
		b.WriteString("  ")
		b.WriteString(tui.SelectionCountStyle.Render(fmt.Sprintf("%d selected", len(m.selectedIntents))))
	}
	if m.shouldShowPreview() && m.previewFocused {
		b.WriteString(tui.HelpStyle.Render(" [preview focused]"))
	}
	b.WriteString("\n")

	// Search input (only show when in focus or has value)
	if m.focus == focusSearch || m.searchInput.Value() != "" {
		b.WriteString(m.searchInput.View())
		if m.focus == focusSearch {
			b.WriteString("  ")
			b.WriteString(tui.HelpStyle.Render("(enter to search, esc to cancel)"))
		}
		b.WriteString("\n")
	}

	// Always show the filter bar chips
	b.WriteString(m.renderFilterBarComponent())
	b.WriteString("\n")

	// Show active filters summary (concept, search) if any
	activeFilters := m.renderActiveFilters()
	if activeFilters != "" {
		b.WriteString(activeFilters)
		b.WriteString("\n")
	}

	b.WriteString("\n")

	// Calculate widths based on preview visibility and layout mode
	listWidth := m.width
	if m.shouldShowPreview() {
		switch m.layoutMode {
		case layoutWide:
			listWidth = m.width * 50 / 100
		default:
			listWidth = m.width * 60 / 100
		}
		listWidth = max(listWidth, 40)
	}

	// Calculate available width for title within the list
	titleWidth := listWidth - 35
	if m.layoutMode == layoutNarrow {
		titleWidth = m.width - 28 // cursor + type + date
	}
	titleWidth = max(titleWidth, 20)

	// Build list lines
	var listLines []string

	// Handle empty state
	if len(m.filteredIntents) == 0 {
		if m.hasActiveFilters() {
			listLines = append(listLines,
				"",
				tui.HelpStyle.Render("  No intents match current filters."),
				tui.HelpStyle.Render("  Press Escape to clear filters."),
			)
		} else {
			listLines = append(listLines,
				"",
				tui.HelpStyle.Render("  No intents found."),
				tui.HelpStyle.Render("  Press 'n' to create one."),
			)
		}
	}

	for gi, group := range m.groups {
		// Group header
		indicator := ">"
		if group.Expanded {
			indicator = "v"
		}

		isGroupSelected := gi == m.cursorGroup && m.cursorItem == -1
		cursor := tui.NoCursor
		if isGroupSelected && !m.previewFocused {
			cursor = tui.CursorIndicator
		}

		header := fmt.Sprintf("%s %s %s (%d)", cursor, indicator, group.Name, len(group.Intents))
		if isGroupSelected && !m.previewFocused {
			listLines = append(listLines, tui.GroupHeaderSelectedStyle.Render(header))
		} else {
			listLines = append(listLines, tui.GroupHeaderStyle.Render(header))
		}

		// Render items if expanded
		if group.Expanded {
			for ii, i := range group.Intents {
				isSelected := gi == m.cursorGroup && ii == m.cursorItem && !m.previewFocused
				listLines = append(listLines, m.renderIntentRow(i, isSelected, titleWidth))
			}
		}
	}

	// Apply scroll windowing: show only lines visible within listHeight
	visibleLines := listLines
	if m.listHeight > 0 && len(listLines) > m.listHeight {
		// Clamp scrollOffset
		maxOffset := len(listLines) - m.listHeight
		if m.scrollOffset > maxOffset {
			m.scrollOffset = maxOffset
		}
		if m.scrollOffset < 0 {
			m.scrollOffset = 0
		}
		end := m.scrollOffset + m.listHeight
		visibleLines = listLines[m.scrollOffset:end]
	}

	// Pad with empty lines if content is shorter than viewport
	for len(visibleLines) < m.listHeight {
		visibleLines = append(visibleLines, "")
	}

	listView := strings.Join(visibleLines, "\n")

	// Combine list and preview if preview is visible
	if m.shouldShowPreview() {
		previewView := m.previewPane.View()

		// Join horizontally with gap
		combined := lipgloss.JoinHorizontal(
			lipgloss.Top,
			listView,
			"  ", // gap between list and preview
			previewView,
		)
		b.WriteString(combined)
	} else {
		b.WriteString(listView)
	}

	// Status bar - adaptive based on layout mode
	b.WriteString("\n")
	b.WriteString(m.renderStatusBar())

	if m.statusMessage != "" {
		b.WriteString("\n")
		b.WriteString(tui.ErrorStyle.Render(m.statusMessage))
	}

	return b.String()
}
