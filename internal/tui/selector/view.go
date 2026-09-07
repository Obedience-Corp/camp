package selector

import (
	"strings"

	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
)

var pal = theme.TUI()

func (m Model) View() string {
	var b strings.Builder
	if m.opts.Title != "" {
		b.WriteString(titleStyle.Render(m.opts.Title))
		b.WriteString("\n")
	}
	b.WriteString(m.wheelView())
	if m.mode == ModeFilter {
		b.WriteString(m.queryRow())
		b.WriteString("\n")
	}
	help := strings.TrimSpace(m.opts.Help)
	if help != "" {
		b.WriteString(helpStyle.Render(ui.CollapseHelp(m.width, help)))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) wheelView() string {
	if len(m.items) == 0 {
		return helpStyle.Render("(no items)") + "\n"
	}
	if len(m.visible) == 0 {
		return helpStyle.Render(`no matches for "`+m.query+`"`) + "\n"
	}
	start, end := visibleRange(len(m.visible), m.cursor, m.visibleCount())
	width := m.wheelWidth()
	var b strings.Builder
	for i := start; i < end; i++ {
		label := m.visible[i].Label
		prefix := "  "
		style := normalStyle.Width(width)
		if i == m.cursor {
			prefix = "> "
			style = selectedStyle.Width(width)
		}
		b.WriteString(style.Render(prefix + label))
		b.WriteString("\n")
	}
	return b.String()
}

func (m Model) queryRow() string {
	return helpStyle.Render("filter: " + m.query)
}

func (m Model) visibleCount() int {
	if m.opts.Visible <= 0 {
		return defaultVisible
	}
	return m.opts.Visible
}

func (m Model) wheelWidth() int {
	maxWidth := 40
	for _, it := range m.visible {
		if w := len(it.Label) + 2; w > maxWidth {
			maxWidth = w
		}
	}
	if maxWidth > 80 {
		maxWidth = 80
	}
	if m.width > 0 && m.width < maxWidth {
		return max(m.width, 8)
	}
	return maxWidth
}

// visibleRange pins the window to the tail when the cursor is in the last
// `visible` rows so the recency winner stays on the last visible line.
func visibleRange(n, selected, visible int) (int, int) {
	if n <= visible {
		return 0, n
	}
	if selected >= n-visible {
		return n - visible, n
	}
	half := visible / 2
	start := selected - half
	if start < 0 {
		start = 0
	}
	end := start + visible
	if end > n {
		end = n
		start = n - visible
	}
	return start, end
}

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(pal.Accent)
	helpStyle = lipgloss.NewStyle().
			Foreground(pal.TextMuted)
	normalStyle   = lipgloss.NewStyle()
	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(pal.Accent).
			Background(pal.BgSelected)
)
