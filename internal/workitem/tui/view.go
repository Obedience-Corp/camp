package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/charmbracelet/lipgloss"
)

const (
	minPreviewWidth = 35
	minListWidth    = 40
)

func isWideLayout(width int) bool {
	return width >= minListWidth+minPreviewWidth+1
}

func (m Model) View() string {
	if !m.ready {
		return "Loading..."
	}
	if m.helpVisible {
		return m.renderHelp()
	}

	if isWideLayout(m.width) && m.showPreview {
		return m.renderWideLayout()
	}

	if m.previewOverlay && len(m.filteredItems) > 0 {
		return m.renderPreviewOverlay()
	}

	return m.renderListOnly()
}

func (m Model) renderWideLayout() string {
	listWidth := max(minListWidth, m.width*60/100)
	previewWidth := m.width - listWidth - 1
	contentHeight := m.viewportHeight() // header + footer + separator

	left := m.renderList(listWidth, contentHeight)
	right := renderPreview(m.currentItem(), previewWidth, contentHeight)

	sep := lipgloss.NewStyle().Foreground(pal.Border).Render("│")
	content := lipgloss.JoinHorizontal(lipgloss.Top, left, sep, right)
	return m.renderHeader() + "\n" + content + "\n" + m.renderFooter()
}

func (m Model) renderListOnly() string {
	contentHeight := m.viewportHeight()
	list := m.renderList(m.width, contentHeight)
	return m.renderHeader() + "\n" + list + "\n" + m.renderFooter()
}

func (m Model) renderPreviewOverlay() string {
	contentHeight := m.viewportHeight()
	preview := renderPreview(m.currentItem(), m.width, contentHeight)
	return m.renderHeader() + "\n" + preview + "\n" + m.renderFooter()
}

func (m Model) renderHeader() string {
	title := headerStyle.Render("camp workitem")
	var filters []string
	if m.typeFilter != "" {
		filters = append(filters, filterActiveStyle.Render("type:"+m.typeFilter))
	}
	if m.searchQuery != "" {
		filters = append(filters, filterActiveStyle.Render("search:"+m.searchQuery))
	}
	if len(filters) > 0 {
		title += "  " + strings.Join(filters, " ")
	}
	if m.searchMode {
		title += "  " + footerStyle.Render("/"+m.searchInput.Value())
	}
	return title
}

func (m Model) renderFooter() string {
	if m.searchMode {
		return footerStyle.Render("type search query, Enter to confirm, Esc to cancel")
	}
	if m.statusMsg != "" {
		if m.statusIsError {
			return statusErrorStyle.Render(m.statusMsg)
		}
		return statusSuccessStyle.Render(m.statusMsg)
	}
	if m.isPriorityMode() {
		return footerStyle.Render("priority: h high  m medium  l low  0 clear  Esc cancel")
	}
	count := fmt.Sprintf("%d items", len(m.filteredItems))
	keys := "j/k move  / search  1-4 filter  0 all  P priority  tab preview  r refresh  ? help  q quit"
	if len(keys)+len(count)+2 > m.width {
		keys = "j/k / 1-4 P tab r ? q"
	}
	return footerStyle.Render(fmt.Sprintf("%s  %s", count, keys))
}

func (m Model) renderList(width, height int) string {
	if len(m.filteredItems) == 0 {
		return m.renderEmpty(width, height)
	}

	var b strings.Builder
	end := min(m.scrollOffset+height, len(m.filteredItems))
	for i := m.scrollOffset; i < end; i++ {
		row := renderRow(m.filteredItems[i], width, i == m.cursor)
		b.WriteString(row)
		if i < end-1 {
			b.WriteString("\n")
		}
	}

	// Pad remaining lines
	rendered := end - m.scrollOffset
	for i := rendered; i < height; i++ {
		b.WriteString("\n")
	}
	return b.String()
}

func (m Model) renderEmpty(_, height int) string {
	var b strings.Builder
	b.WriteString("\n  ")
	b.WriteString(emptyMsgStyle.Render("No work items found."))
	b.WriteString("\n\n")
	b.WriteString(footerStyle.Render("  Scanned:"))
	b.WriteString("\n")
	b.WriteString(footerStyle.Render("    .campaign/intents/{inbox,active,ready}"))
	b.WriteString("\n")
	b.WriteString(footerStyle.Render("    workflow/design/"))
	b.WriteString("\n")
	b.WriteString(footerStyle.Render("    workflow/explore/"))
	b.WriteString("\n")
	b.WriteString(footerStyle.Render("    festivals/{planning,ready,active}"))
	b.WriteString("\n")
	if m.typeFilter != "" {
		b.WriteString(fmt.Sprintf("\n  Filter active: %s\n", filterActiveStyle.Render("type="+m.typeFilter)))
	}
	if m.searchQuery != "" {
		b.WriteString(fmt.Sprintf("  Search: %s\n", filterActiveStyle.Render(m.searchQuery)))
	}

	// Pad
	lines := strings.Count(b.String(), "\n") + 1
	for i := lines; i < height; i++ {
		b.WriteString("\n")
	}
	return b.String()
}

func priorityBadge(p string) (string, lipgloss.Style) {
	switch p {
	case "high":
		return "H ", priorityHighStyle
	case "medium":
		return "M ", priorityMediumStyle
	case "low":
		return "L ", priorityLowStyle
	default:
		return "", lipgloss.NewStyle()
	}
}

func renderRow(item workitem.WorkItem, width int, selected bool) string {
	// Pad plain strings first, then apply color to avoid ANSI width issues
	wfType := padRight(string(item.WorkflowType), 9)
	stage := padRight(item.LifecycleStage, 9)
	rec := formatRecency(item.SortTimestamp)

	badgeText, badgeStyle := priorityBadge(item.ManualPriority)
	badgeWidth := len(badgeText)

	titleWidth := width - 9 - 9 - len(rec) - 4 - badgeWidth
	if titleWidth < 10 {
		titleWidth = 10
	}
	title := truncate(item.Title, titleWidth)
	title = padRight(title, titleWidth)

	// Apply styles to already-padded segments
	styledType := workflowStyle(item.WorkflowType).Render(wfType)
	styledStage := stageStyle(item.LifecycleStage).Render(stage)
	styledBadge := ""
	if badgeText != "" {
		styledBadge = badgeStyle.Render(badgeText)
	}
	styledTitle := rowTitleStyle.Render(title)
	styledRecency := recencyStyle(item.SortTimestamp).Render(rec)

	row := fmt.Sprintf(" %s %s %s%s %s", styledType, styledStage, styledBadge, styledTitle, styledRecency)
	if selected {
		return rowSelectedStyle.Width(width).Render(row)
	}
	return row
}

func formatRecency(t time.Time) string {
	if t.IsZero() {
		return "  -"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dw", int(d.Hours()/(24*7)))
	}
}

func renderPreview(item workitem.WorkItem, width, height int) string {
	if item.Key == "" {
		return strings.Repeat("\n", height)
	}

	var b strings.Builder
	titleStyle := previewTitleStyle.Width(max(width-1, 20))
	b.WriteString(titleStyle.Render(item.Title))
	b.WriteString("\n")
	sep := strings.Repeat("─", min(width-1, 40))
	b.WriteString(previewSepStyle.Render(sep))
	b.WriteString("\n\n")

	stage := item.LifecycleStage
	if stage == "" {
		stage = "—"
	}

	// Truncate the path value to fit the preview width minus the label.
	maxValueWidth := max(width-12, 10)

	b.WriteString(fmt.Sprintf("%s  %s\n",
		previewLabelStyle.Render("type:"),
		workflowStyle(item.WorkflowType).Render(string(item.WorkflowType))))
	b.WriteString(fmt.Sprintf("%s %s\n",
		previewLabelStyle.Render("stage:"),
		stageStyle(stage).Render(stage)))
	if item.ManualPriority != "" {
		_, style := priorityBadge(item.ManualPriority)
		b.WriteString(fmt.Sprintf("%s %s\n",
			previewLabelStyle.Render("priority:"),
			style.Render(item.ManualPriority)))
	}
	if item.WorkflowType == workitem.WorkflowTypeIntent {
		if srcPrio, ok := item.SourceMetadata["priority"]; ok {
			if prioStr, ok := srcPrio.(string); ok && prioStr != "" {
				b.WriteString(fmt.Sprintf("%s %s\n",
					previewLabelStyle.Render("intent priority:"),
					previewValueStyle.Render(prioStr)))
			}
		}
	}
	b.WriteString(fmt.Sprintf("%s %s\n",
		previewLabelStyle.Render("updated:"),
		previewValueStyle.Render(item.UpdatedAt.Format("2006-01-02 15:04"))))
	b.WriteString(fmt.Sprintf("%s %s\n",
		previewLabelStyle.Render("created:"),
		previewValueStyle.Render(item.CreatedAt.Format("2006-01-02 15:04"))))
	b.WriteString(fmt.Sprintf("%s  %s\n",
		previewLabelStyle.Render("path:"),
		previewValueStyle.Render(truncate(item.RelativePath, maxValueWidth))))
	if item.PrimaryDoc != "" {
		b.WriteString(fmt.Sprintf("%s %s\n",
			previewLabelStyle.Render("primary:"),
			previewValueStyle.Render(truncate(filepath.Base(item.PrimaryDoc), maxValueWidth))))
	}

	if item.Summary != "" {
		b.WriteString("\n")
		summaryStyle := lipgloss.NewStyle().
			Foreground(pal.TextPrimary).
			Width(max(width-2, 20))
		b.WriteString(summaryStyle.Render(item.Summary))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(previewHelpStyle.Render("Enter open/jump  e edit  o open  y copy"))

	// Pad remaining height
	lines := strings.Count(b.String(), "\n") + 1
	for i := lines; i < height; i++ {
		b.WriteString("\n")
	}
	return b.String()
}

func (m Model) renderHelp() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString("  " + helpTitleStyle.Render("WORKITEM DASHBOARD HELP"))
	b.WriteString("\n")
	b.WriteString("  " + previewSepStyle.Render("───────────────────────"))
	b.WriteString("\n\n")

	sections := []struct {
		title string
		keys  [][2]string
	}{
		{"Navigation", [][2]string{
			{"j / ↓", "Move cursor down"},
			{"k / ↑", "Move cursor up"},
			{"g g", "Jump to top"},
			{"G", "Jump to bottom"},
		}},
		{"Search & Filter", [][2]string{
			{"/", "Start search"},
			{"Esc", "Clear search / close overlay"},
			{"0", "Show all types"},
			{"1", "Filter: intent"},
			{"2", "Filter: design"},
			{"3", "Filter: explore"},
			{"4", "Filter: festival"},
		}},
		{"Actions", [][2]string{
			{"Enter", "Open intents or jump to directories"},
			{"e", "Open primary doc in $EDITOR"},
			{"o", "Open with system handler"},
			{"y", "Copy absolute path"},
			{"Tab / p", "Toggle preview pane"},
			{"r", "Refresh (re-scan)"},
		}},
		{"Priority", [][2]string{
			{"P", "Assign manual priority to selected item"},
			{"h / 1", "Set high priority (in priority mode)"},
			{"m / 2", "Set medium priority (in priority mode)"},
			{"l / 3", "Set low priority (in priority mode)"},
			{"0", "Clear manual priority (in priority mode)"},
			{"Esc", "Cancel priority mode"},
		}},
		{"Other", [][2]string{
			{"?", "Toggle this help"},
			{"q", "Quit"},
		}},
	}

	for _, sec := range sections {
		b.WriteString("  " + helpSectionStyle.Render(sec.title))
		b.WriteString("\n")
		for _, kv := range sec.keys {
			b.WriteString(fmt.Sprintf("    %s  %s\n",
				helpKeyStyle.Render(padRight(kv[0], 14)),
				helpDescStyle.Render(kv[1])))
		}
		b.WriteString("\n")
	}

	b.WriteString("  " + previewHelpStyle.Render("Press ? or Esc to close this help."))
	b.WriteString("\n")
	return b.String()
}
