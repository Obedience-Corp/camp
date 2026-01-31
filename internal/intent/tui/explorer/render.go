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
	// Add multi-select mode hints
	if m.multiSelectMode {
		count := len(m.selectedIntents)
		switch m.layoutMode {
		case layoutNarrow:
			return tui.HelpStyle.Render(fmt.Sprintf("Space: select . Ctrl-g: gather (%d) . Esc: cancel", count))
		default:
			return tui.HelpStyle.Render(fmt.Sprintf("Space: toggle select . Ctrl-g: gather %d intents . Esc: exit multi-select . ?: help", count))
		}
	}

	switch m.layoutMode {
	case layoutNarrow:
		// Minimal hints
		return tui.HelpStyle.Render("j/k . v . / . n . ? . q")
	case layoutNormal:
		if m.shouldShowPreview() {
			return tui.HelpStyle.Render("j/k: nav . v: hide preview . tab: focus . /: search . n: new . q: quit")
		}
		return tui.HelpStyle.Render("j/k: nav . v: preview . /: search . t: type . s: status . Space: select . q: quit")
	case layoutWide:
		if m.shouldShowPreview() {
			return tui.HelpStyle.Render("j/k: navigate . v: hide preview . tab: switch focus . /: search . f: full view . n: new . ?: help . q: quit")
		}
		return tui.HelpStyle.Render("j/k: navigate . v: preview . /: search . t: type . s: status . Space: select . f: full . ?: help . q: quit")
	}
	return ""
}

// renderFilterBar renders the active filter pills and selection count.
// Returns empty string if no filters are active and not in multi-select mode.
func (m *Model) renderFilterBar() string {
	var pills []string

	// Check active filters
	typeValue := m.typeWheel.SelectedValue()
	if typeValue != "" && typeValue != "All" {
		pills = append(pills, tui.FilterPillStyle.Render(fmt.Sprintf("type:%s x", strings.ToLower(typeValue))))
	}

	statusValue := m.statusWheel.SelectedValue()
	if statusValue != "" && statusValue != "All" {
		pills = append(pills, tui.FilterPillStyle.Render(fmt.Sprintf("status:%s x", strings.ToLower(statusValue))))
	}

	if m.conceptFilterPath != "" {
		conceptName := m.conceptFilterPath
		// Show just the last part for brevity
		parts := strings.Split(m.conceptFilterPath, "/")
		if len(parts) > 0 {
			conceptName = parts[len(parts)-1]
		}
		pills = append(pills, tui.FilterPillStyle.Render(fmt.Sprintf("concept:%s x", conceptName)))
	}

	if m.searchInput.Value() != "" && m.focus != focusSearch {
		query := m.searchInput.Value()
		if len(query) > 15 {
			query = query[:12] + "..."
		}
		pills = append(pills, tui.FilterPillStyle.Render(fmt.Sprintf("search:%s x", query)))
	}

	// Build the filter bar
	var parts []string

	// Selection count badge (always show when in multi-select)
	if m.multiSelectMode {
		count := len(m.selectedIntents)
		parts = append(parts, tui.SelectionCountStyle.Render(fmt.Sprintf("%d selected", count)))
	}

	// Filter pills
	if len(pills) > 0 {
		parts = append(parts, strings.Join(pills, " "))
		parts = append(parts, tui.HelpStyle.Render("[Esc: clear]"))
	}

	if len(parts) == 0 {
		return ""
	}

	return "Active: " + strings.Join(parts, "  ")
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

	// Filter bar - show active filters as pills
	filterBar := m.renderFilterBar()
	if filterBar != "" {
		b.WriteString(filterBar)
		b.WriteString("\n")
	}

	// Only show filter wheels when actively filtering
	if m.focus == focusTypeFilter || m.focus == focusStatusFilter {
		// Type filter indicator
		typeValue := m.typeWheel.SelectedValue()
		if m.focus == focusTypeFilter {
			b.WriteString(tui.HelpStyle.Render("Type: "))
			b.WriteString(tui.IntentConceptStyle.Render("[" + typeValue + "]"))
			b.WriteString(" ")
			b.WriteString(tui.HelpStyle.Render("(j/k to change, enter to select)"))
		}

		// Status filter indicator
		statusValue := m.statusWheel.SelectedValue()
		if m.focus == focusStatusFilter {
			b.WriteString(tui.HelpStyle.Render("Status: "))
			b.WriteString(tui.IntentConceptStyle.Render("[" + statusValue + "]"))
			b.WriteString(" ")
			b.WriteString(tui.HelpStyle.Render("(j/k to change, enter to select)"))
		}
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

	// Build the list view
	var listBuilder strings.Builder

	// Handle empty state
	if len(m.filteredIntents) == 0 {
		if m.searchInput.Value() != "" || m.typeWheel.SelectedValue() != "All" ||
			m.statusWheel.SelectedValue() != "All" || m.conceptFilterPath != "" {
			listBuilder.WriteString(tui.HelpStyle.Render("\n  No intents match current filters.\n  Press Escape to clear filters.\n"))
		} else {
			listBuilder.WriteString(tui.HelpStyle.Render("\n  No intents found.\n  Press 'n' to create one.\n"))
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
			listBuilder.WriteString(tui.GroupHeaderSelectedStyle.Render(header))
		} else {
			listBuilder.WriteString(tui.GroupHeaderStyle.Render(header))
		}
		listBuilder.WriteString("\n")

		// Render items if expanded
		if group.Expanded {
			for ii, i := range group.Intents {
				isSelected := gi == m.cursorGroup && ii == m.cursorItem && !m.previewFocused
				listBuilder.WriteString(m.renderIntentRow(i, isSelected, titleWidth))
				listBuilder.WriteString("\n")
			}
		}
	}

	// Combine list and preview if preview is visible
	if m.shouldShowPreview() {
		listView := listBuilder.String()
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
		b.WriteString(listBuilder.String())
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
