package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/priority"
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
	if values := m.activeTypes(); len(values) > 0 {
		filters = append(filters, filterActiveStyle.Render("type:"+strings.Join(values, ",")))
	}
	if values := m.activeCategories(); len(values) > 0 {
		filters = append(filters, filterActiveStyle.Render("category:"+strings.Join(values, ",")))
	}
	if values := m.activeStatuses(); len(values) > 0 {
		filters = append(filters, filterActiveStyle.Render("status:"+strings.Join(values, ",")))
	}
	if len(m.initialFilters.Groups) > 0 {
		filters = append(filters, filterActiveStyle.Render("group:"+strings.Join(m.initialFilters.Groups, ",")))
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

func (m Model) activeTypes() []string {
	if m.typeFilter != "" {
		return []string{m.typeFilter}
	}
	return m.initialFilters.Types
}

func (m Model) activeCategories() []string {
	if m.categoryFilter != "" {
		return []string{m.categoryFilter}
	}
	return m.initialFilters.Categories
}

func (m Model) activeStatuses() []string {
	if m.statusFilter != "" {
		return []string{m.statusFilter}
	}
	values := append([]string(nil), m.initialFilters.Statuses...)
	for _, stage := range m.initialFilters.LifecycleStages {
		values = append(values, "lifecycle="+stage)
	}
	for _, stage := range m.initialFilters.AttentionStages {
		values = append(values, "attention="+stage)
	}
	return values
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
	if m.isStageMode() {
		return footerStyle.Render("stage: c current  s next  a active  p parked  0 clear  Esc cancel")
	}
	if m.isFilterMode() {
		return m.renderFilterChips()
	}
	if m.isStatusMode() {
		return m.renderStatusFilter()
	}
	count := fmt.Sprintf("%d items", len(m.filteredItems))
	keys := "j/k move  / search  f type  s status  c category  0 all  S set-stage  P priority  tab preview  r refresh  ? help  q quit"
	if len(keys)+len(count)+2 > m.width {
		keys = "j/k / f filter s status c category P tab r ? q"
	}
	return footerStyle.Render(fmt.Sprintf("%s  %s", count, keys))
}

func (m Model) renderStatusFilter() string {
	labels := make([]string, len(m.statusOptions))
	for i, status := range m.statusOptions {
		if status == "" {
			labels[i] = "all"
		} else {
			labels[i] = status
		}
	}
	parts := make([]string, len(labels))
	for i, label := range labels {
		if i == m.statusIndex {
			parts[i] = "[" + label + "]"
		} else {
			parts[i] = label
		}
	}
	row := "status: " + strings.Join(parts, "  ")
	return footerStyle.Render(truncate(row, max(m.width, 1)))
}

const (
	filterChipPrefix    = "filter: "
	filterModeHint      = "j/k step  Enter apply  Esc cancel"
	chipSeparatorWidth  = 2 // width of the "  " join between parts
	chipBracketWidth    = 2 // "[" + "]" around the active chip
	chipEllipsisWidth   = 1 // rendered width of "…"
	minChipLabelColumns = 4
)

// renderFilterChips renders the filter-mode footer: one chip per type with
// its item count, windowed around the active chip when the row overflows.
func (m Model) renderFilterChips() string {
	counts, total := m.visibleTypeCounts()
	labels := make([]string, len(m.filterOptions))
	for i, opt := range m.filterOptions {
		if opt == "" {
			labels[i] = fmt.Sprintf("all %d", total)
		} else {
			labels[i] = fmt.Sprintf("%s %d", opt, counts[opt])
		}
	}

	avail := m.width - len(filterChipPrefix)
	// Cap label widths so even a bracketed single chip flanked by two
	// ellipses fits. Narrower than that, fall back to a plain row truncated
	// before styling so the footer can never overflow the terminal.
	maxLabel := avail - chipBracketWidth - 2*(chipEllipsisWidth+chipSeparatorWidth)
	if maxLabel < minChipLabelColumns {
		row := filterChipPrefix + "[" + labels[m.filterIndex] + "]"
		return footerStyle.Render(truncate(row, max(m.width, 1)))
	}
	for i := range labels {
		labels[i] = truncate(labels[i], maxLabel)
	}
	start, end := chipWindow(labels, m.filterIndex, avail)

	var parts []string
	if start > 0 {
		parts = append(parts, footerStyle.Render("…"))
	}
	for i := start; i < end; i++ {
		if i == m.filterIndex {
			parts = append(parts, filterActiveStyle.Render("["+labels[i]+"]"))
		} else {
			parts = append(parts, footerStyle.Render(labels[i]))
		}
	}
	if end < len(labels) {
		parts = append(parts, footerStyle.Render("…"))
	}
	row := footerStyle.Render(filterChipPrefix) + strings.Join(parts, "  ")

	// Teach the mode keys like the priority/stage footers do; the chip row
	// wins the space when both cannot fit.
	rowWidth := len(filterChipPrefix) + chipRowWidth(labels, m.filterIndex, start, end)
	if rowWidth+chipSeparatorWidth+len(filterModeHint) <= m.width {
		row += "  " + footerStyle.Render(filterModeHint)
	}
	return row
}

// chipRowWidth returns the rendered width of the chip row for the window
// [start, end): labels, active brackets, ellipsis markers on truncated
// sides, and the separators joining every part. Label widths use byte
// length, which never understates terminal columns.
func chipRowWidth(labels []string, active, start, end int) int {
	width := 0
	parts := 0
	if start > 0 {
		width += chipEllipsisWidth
		parts++
	}
	for i := start; i < end; i++ {
		width += len(labels[i])
		if i == active {
			width += chipBracketWidth
		}
		parts++
	}
	if end < len(labels) {
		width += chipEllipsisWidth
		parts++
	}
	if parts > 1 {
		width += chipSeparatorWidth * (parts - 1)
	}
	return width
}

// chipWindow returns the [start, end) chip range that fits in avail
// columns, expanded outward from the active chip. Every expansion is
// checked against the exact rendered width of the candidate window.
func chipWindow(labels []string, active, avail int) (int, int) {
	if len(labels) == 0 {
		return 0, 0
	}
	if active < 0 || active >= len(labels) {
		active = 0
	}
	start, end := active, active+1
	for start > 0 || end < len(labels) {
		extended := false
		if end < len(labels) && chipRowWidth(labels, active, start, end+1) <= avail {
			end++
			extended = true
		}
		if start > 0 && chipRowWidth(labels, active, start-1, end) <= avail {
			start--
			extended = true
		}
		if !extended {
			break
		}
	}
	return start, end
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
	b.WriteString(footerStyle.Render("    festivals/{planning,ready,active,ritual,chains}"))
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
	wfType := padRight(string(item.WorkflowType), 9)
	statusText, statusStyle := rowStatus(item)
	status := padRight(statusText, 7)
	rec := formatRecency(item.SortTimestamp)

	badgeText, badgeStyle := priorityBadge(item.ManualPriority)
	badgeWidth := len(badgeText)

	group := truncate(item.Group, 12)
	groupWidth := len(group)
	if group != "" {
		groupWidth += 1
	}
	titleWidth := width - 9 - 7 - groupWidth - len(rec) - 5 - badgeWidth
	if titleWidth < 10 {
		group = ""
		titleWidth = width - 9 - 7 - len(rec) - 5 - badgeWidth
	}
	if titleWidth < 10 {
		titleWidth += badgeWidth
		badgeText = ""
		if titleWidth < 10 {
			titleWidth = 10
		}
	}
	title := truncate(item.Title, titleWidth)
	title = padRight(title, titleWidth)

	styledType := workflowStyle(item.WorkflowType).Render(wfType)
	styledStatus := statusStyle.Render(status)
	styledBadge := ""
	if badgeText != "" {
		styledBadge = badgeStyle.Render(badgeText)
	}
	styledGroup := ""
	if group != "" {
		styledGroup = previewValueStyle.Render(group + " ")
	}
	styledTitle := rowTitleStyle.Render(title)
	styledRecency := recencyStyle(item.SortTimestamp).Render(rec)

	row := fmt.Sprintf(" %s %s %s%s%s %s", styledType, styledStatus, styledBadge, styledGroup, styledTitle, styledRecency)
	if selected {
		return rowSelectedStyle.Width(width).Render(row)
	}
	return row
}

func rowStatus(item workitem.WorkItem) (string, lipgloss.Style) {
	if priority.EligibleForAttention(item) {
		return shortAttention(item.AttentionStage), attentionStyle(item.AttentionStage)
	}
	return shortLifecycle(item.LifecycleStage), stageStyle(string(item.LifecycleStage))
}

func shortAttention(stage string) string {
	switch stage {
	case "current":
		return "cur"
	case "next":
		return "nxt"
	case "active":
		return "act"
	case "parked":
		return "prk"
	default:
		return "-"
	}
}

func shortLifecycle(stage workitem.LifecycleStage) string {
	switch stage {
	case workitem.LifecycleStageInbox:
		return "inbox"
	case workitem.LifecycleStageActive:
		return "active"
	case workitem.LifecycleStageReady:
		return "ready"
	case workitem.LifecycleStagePlanning:
		return "plan"
	case workitem.LifecycleStageRitual:
		return "ritual"
	case workitem.LifecycleStageChains:
		return "chains"
	case workitem.LifecycleStageNone, "":
		// Match DisplayStatus / --status none filter token.
		return "none"
	default:
		return string(stage)
	}
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

	stage := string(item.LifecycleStage)
	if stage == "" {
		stage = "—"
	}

	// Truncate the path value to fit the preview width minus the label.
	maxValueWidth := max(width-12, 10)

	b.WriteString(fmt.Sprintf("%s  %s\n",
		previewLabelStyle.Render("type:"),
		workflowStyle(item.WorkflowType).Render(string(item.WorkflowType))))
	b.WriteString(fmt.Sprintf("%s %s\n",
		previewLabelStyle.Render("lifecycle:"),
		stageStyle(stage).Render(stage)))
	attention := item.AttentionStage
	if attention == "" {
		attention = "none"
	}
	if item.AttentionStageSource != "" && item.AttentionStageSource != "none" {
		attention += " (" + item.AttentionStageSource + ")"
	}
	fmt.Fprintf(&b, "%s %s\n",
		previewLabelStyle.Render("attention:"),
		attentionStyle(item.AttentionStage).Render(attention))
	if item.Group != "" {
		fmt.Fprintf(&b, "%s %s\n",
			previewLabelStyle.Render("group:"),
			previewValueStyle.Render(item.Group))
	}
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

	if item.StableID != "" {
		b.WriteString(fmt.Sprintf("%s %s\n",
			previewLabelStyle.Render("stable id:"),
			previewValueStyle.Render(truncate(item.StableID, maxValueWidth))))
	}
	if item.WorkflowMeta != nil && (item.WorkflowMeta.WorkflowID != "" || item.WorkflowMeta.TotalSteps > 0 || item.WorkflowMeta.RunStatus != "") {
		b.WriteString("\n")
		b.WriteString(previewLabelStyle.Render("WORKFLOW"))
		b.WriteString("\n")
		if item.WorkflowMeta.WorkflowID != "" {
			b.WriteString(fmt.Sprintf("  id        %s\n", previewValueStyle.Render(item.WorkflowMeta.WorkflowID)))
		}
		if item.WorkflowMeta.ActiveRunID != "" {
			b.WriteString(fmt.Sprintf("  active    %s\n", previewValueStyle.Render(item.WorkflowMeta.ActiveRunID)))
		}
		if item.WorkflowMeta.TotalSteps > 0 {
			progress := fmt.Sprintf("Step %d of %d", item.WorkflowMeta.CurrentStep, item.WorkflowMeta.TotalSteps)
			if item.WorkflowMeta.Blocked {
				progress += " (blocked)"
			}
			b.WriteString(fmt.Sprintf("  progress  %s\n", previewValueStyle.Render(progress)))
		}
		if item.WorkflowMeta.RunStatus != "" {
			b.WriteString(fmt.Sprintf("  status    %s\n", previewValueStyle.Render(item.WorkflowMeta.RunStatus)))
		}
		if item.WorkflowMeta.DocHashChanged {
			b.WriteString(fmt.Sprintf("  %s\n", previewLabelStyle.Render("⚠ workflow doc changed since run started")))
		}
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
			{"f", "Filter by type (j/k or arrows step, Enter apply, Esc cancel)"},
			{"0", "Clear all filters"},
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
			{"S", "Assign attention stage to selected item"},
			{"c/s/a/p", "Set current/next/active/parked (in stage mode)"},
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
