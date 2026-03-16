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
	contentHeight := m.height - 3 // header + footer + separator

	left := m.renderList(listWidth, contentHeight)
	right := renderPreview(m.currentItem(), previewWidth, contentHeight)

	content := lipgloss.JoinHorizontal(lipgloss.Top, left, "│", right)
	return m.renderHeader() + "\n" + content + "\n" + m.renderFooter()
}

func (m Model) renderListOnly() string {
	contentHeight := m.height - 3
	list := m.renderList(m.width, contentHeight)
	return m.renderHeader() + "\n" + list + "\n" + m.renderFooter()
}

func (m Model) renderPreviewOverlay() string {
	contentHeight := m.height - 3
	preview := renderPreview(m.currentItem(), m.width, contentHeight)
	return m.renderHeader() + "\n" + preview + "\n" + m.renderFooter()
}

func (m Model) renderHeader() string {
	title := "camp workitem"
	var filters []string
	if m.typeFilter != "" {
		filters = append(filters, "type:"+m.typeFilter)
	}
	if m.searchQuery != "" {
		filters = append(filters, "search:"+m.searchQuery)
	}
	if len(filters) > 0 {
		title += "  " + strings.Join(filters, " ")
	}
	if m.searchMode {
		title += "  /" + m.searchInput.Value()
	}
	return lipgloss.NewStyle().Bold(true).Render(title)
}

func (m Model) renderFooter() string {
	if m.searchMode {
		return "type search query, Enter to confirm, Esc to cancel"
	}
	count := fmt.Sprintf("%d items", len(m.filteredItems))
	keys := "j/k move  / search  1-4 filter  0 all  tab preview  r refresh  ? help  q quit"
	if len(keys)+len(count)+2 > m.width {
		keys = "j/k / 1-4 tab r ? q"
	}
	return fmt.Sprintf("%s  %s", count, keys)
}

func (m Model) renderList(width, height int) string {
	if len(m.filteredItems) == 0 {
		return m.renderEmpty(width, height)
	}

	var b strings.Builder
	for i, item := range m.filteredItems {
		if i >= height {
			break
		}
		row := renderRow(item, width, i == m.cursor)
		b.WriteString(row)
		if i < len(m.filteredItems)-1 && i < height-1 {
			b.WriteString("\n")
		}
	}

	// Pad remaining lines
	rendered := strings.Count(b.String(), "\n") + 1
	for i := rendered; i < height; i++ {
		b.WriteString("\n")
	}
	return b.String()
}

func (m Model) renderEmpty(width, height int) string {
	var b strings.Builder
	b.WriteString("\n  No work items found.\n\n")
	b.WriteString("  Scanned:\n")
	b.WriteString("    workflow/intents/{inbox,active,ready}\n")
	b.WriteString("    workflow/design/\n")
	b.WriteString("    workflow/explore/\n")
	b.WriteString("    festivals/{planning,ready,active}\n")
	if m.typeFilter != "" {
		b.WriteString(fmt.Sprintf("\n  Filter active: type=%s\n", m.typeFilter))
	}
	if m.searchQuery != "" {
		b.WriteString(fmt.Sprintf("  Search: %s\n", m.searchQuery))
	}

	// Pad
	lines := strings.Count(b.String(), "\n") + 1
	for i := lines; i < height; i++ {
		b.WriteString("\n")
	}
	return b.String()
}

func renderRow(item workitem.WorkItem, width int, selected bool) string {
	wfType := padRight(string(item.WorkflowType), 9)
	stage := padRight(item.LifecycleStage, 9)
	recency := formatRecency(item.SortTimestamp)

	titleWidth := width - 9 - 9 - len(recency) - 4
	if titleWidth < 10 {
		titleWidth = 10
	}
	title := truncate(item.Title, titleWidth)
	title = padRight(title, titleWidth)

	row := fmt.Sprintf(" %s %s %s %s", wfType, stage, title, recency)
	if selected {
		return lipgloss.NewStyle().Reverse(true).Render(row)
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
	b.WriteString(lipgloss.NewStyle().Bold(true).Render(item.Title))
	b.WriteString("\n")
	sep := strings.Repeat("─", min(width-1, 40))
	b.WriteString(sep)
	b.WriteString("\n\n")

	stage := item.LifecycleStage
	if stage == "" {
		stage = "—"
	}

	b.WriteString(fmt.Sprintf("type:       %s\n", item.WorkflowType))
	b.WriteString(fmt.Sprintf("stage:      %s\n", stage))
	b.WriteString(fmt.Sprintf("updated:    %s\n", item.UpdatedAt.Format("2006-01-02 15:04")))
	b.WriteString(fmt.Sprintf("created:    %s\n", item.CreatedAt.Format("2006-01-02 15:04")))
	b.WriteString(fmt.Sprintf("path:       %s\n", item.RelativePath))
	if item.PrimaryDoc != "" {
		b.WriteString(fmt.Sprintf("primary:    %s\n", filepath.Base(item.PrimaryDoc)))
	}

	if item.Summary != "" {
		b.WriteString("\n")
		// Wrap summary to width
		b.WriteString(item.Summary)
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().Faint(true).Render("Enter select  e edit  o open  y copy"))

	// Pad remaining height
	lines := strings.Count(b.String(), "\n") + 1
	for i := lines; i < height; i++ {
		b.WriteString("\n")
	}
	return b.String()
}

func (m Model) renderHelp() string {
	help := `
  WORKITEM DASHBOARD HELP
  ───────────────────────

  Navigation
    j / ↓         Move cursor down
    k / ↑         Move cursor up
    g g           Jump to top
    G             Jump to bottom

  Search & Filter
    /             Start search
    Esc           Clear search / close overlay
    0             Show all types
    1             Filter: intent
    2             Filter: design
    3             Filter: explore
    4             Filter: festival

  Actions
    Enter         Select item and exit
    e             Open primary doc in $EDITOR
    o             Open with system handler
    y             Copy absolute path
    Tab / p       Toggle preview pane
    r             Refresh (re-scan)

  Other
    ?             Toggle this help
    q             Quit

  Press ? or Esc to close this help.
`
	return help
}
