package notify

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Obedience-Corp/camp/internal/notice"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
)

var pal = theme.TUI()

var (
	titleStyle    = lipgloss.NewStyle().Foreground(pal.Accent).Bold(true)
	headerStyle   = lipgloss.NewStyle().Foreground(pal.TextMuted).Bold(true)
	primaryStyle  = lipgloss.NewStyle().Foreground(pal.TextPrimary)
	mutedStyle    = lipgloss.NewStyle().Foreground(pal.TextMuted)
	selectedStyle = lipgloss.NewStyle().Foreground(pal.Accent).Bold(true)
	fixStyle      = lipgloss.NewStyle().Foreground(pal.AccentAlt)
	okStyle       = lipgloss.NewStyle().Foreground(pal.Success)
	errStyle      = lipgloss.NewStyle().Foreground(pal.Error)
	frameBox      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(pal.BorderFocus).Padding(0, 1)
	overlayBox    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(pal.BorderFocus).Padding(1, 2)
)

const (
	boxOverhead  = 4
	minBoxWidth  = 40
	minBoxHeight = 10
	rowPrefix    = 4
	dateWidth    = 10
	labelWidth   = 10
	dateLayout   = "2006-01-02"
)

// View implements tea.Model.
func (m Model) View() string {
	if m.quitting {
		return ""
	}
	if m.showHelp {
		return m.helpView()
	}

	boxed := (m.width == 0 || m.width >= minBoxWidth) && (m.height == 0 || m.height >= minBoxHeight)
	cw := m.contentWidth(boxed)
	detail := m.detailLines(cw)
	footer := m.footerLines(cw)

	lines := []string{m.titleLine(), ""}
	lines = append(lines, m.windowedBody(cw, m.bodyBudget(boxed, len(detail)+len(footer)))...)
	lines = append(lines, detail...)
	lines = append(lines, footer...)

	budget := 0
	if m.height > 0 {
		budget = m.height
		if boxed {
			budget = max(budget-2, 1)
		}
	}
	content := strings.Join(ui.CapFrame(lines, cw, budget), "\n")
	if boxed {
		content = frameBox.Render(content)
	}
	return ui.FitFullscreenView(content, m.height)
}

func (m Model) contentWidth(boxed bool) int {
	if m.width <= 0 {
		return 0
	}
	if boxed {
		return max(m.width-boxOverhead, 1)
	}
	return m.width
}

func (m Model) bodyBudget(boxed bool, reserved int) int {
	if m.height <= 0 {
		return 0
	}
	chrome := 2 + reserved
	if boxed {
		chrome += 2
	}
	return max(m.height-chrome, 1)
}

func (m Model) titleLine() string {
	summary := fmt.Sprintf("%d live · %d dismissed", len(m.inv.Live), len(m.inv.Dismissed))
	if m.busy {
		summary += " · working…"
	}
	return titleStyle.Render("Notices") + "  " + mutedStyle.Render(summary)
}

func (m Model) windowedBody(cw, budget int) []string {
	lines, cursorLine := m.bodyLines(cw)
	if budget <= 0 || len(lines) <= budget {
		return lines
	}
	start, end := ui.WindowRange(cursorLine, len(lines), budget)
	return lines[start:end]
}

func (m Model) bodyLines(cw int) ([]string, int) {
	if m.inv.Empty() {
		return []string{
			primaryStyle.Render("No notices."),
			mutedStyle.Render("camp lists one here when it detects state you may want to fix."),
		}, 0
	}

	rows := m.rows()
	live := len(m.inv.Live)
	lines := []string{headerStyle.Render(fmt.Sprintf("LIVE (%d)", live))}
	if live == 0 {
		lines = append(lines, mutedStyle.Render("  none"))
	}
	cursorLine := 0
	for i, r := range rows {
		if i == live {
			lines = append(lines, "", headerStyle.Render(fmt.Sprintf("DISMISSED (%d)", len(m.inv.Dismissed))))
		}
		if i == m.cursor {
			cursorLine = len(lines)
		}
		lines = append(lines, m.rowLine(r, i == m.cursor, cw))
	}
	if len(m.inv.Dismissed) == 0 {
		lines = append(lines, "", headerStyle.Render("DISMISSED (0)"), mutedStyle.Render("  none"))
	}
	return lines, cursorLine
}

func (m Model) rowLine(r row, selected bool, cw int) string {
	prefix := "  " + ui.CursorGlyph(selected)
	style := primaryStyle
	if selected {
		style = selectedStyle
	}
	label := r.label()
	if r.section == sectionLive {
		return prefix + style.Render(fit(label, cw-rowPrefix))
	}
	labelW := cw - rowPrefix - 2 - dateWidth
	if cw <= 0 {
		labelW = 0
	}
	return prefix + style.Render(pad(fit(label, labelW), labelW)) + "  " + mutedStyle.Render(r.at.Local().Format(dateLayout))
}

func (m Model) detailLines(cw int) []string {
	r, ok := m.selected()
	if !ok || !m.showDetail {
		return nil
	}
	out := []string{"", mutedStyle.Render(strings.Repeat("─", max(cw, 20)))}
	switch {
	case r.message != "":
		out = append(out, wrap(r.message, cw)...)
	case r.summary != "":
		out = append(out, wrap(r.summary, cw)...)
	}
	if r.subject != "" {
		out = append(out, field("subject", primaryStyle.Render(r.subject)))
	}
	switch {
	case r.command != "":
		fix := fixStyle.Render(r.command)
		if notice.HasPlaceholder(r.command) {
			fix += mutedStyle.Render("  (fill in the <…> value)")
		}
		out = append(out, field("fix", fix))
	default:
		out = append(out, field("fix", mutedStyle.Render("not on record; camp keeps only the id and date of a dismissal")))
	}
	out = append(out, field("id", primaryStyle.Render(r.id)))
	if r.section == sectionDismissed {
		out = append(out, field("dismissed", primaryStyle.Render(r.at.Local().Format("2006-01-02 15:04 MST"))))
	}
	return out
}

func (m Model) footerLines(cw int) []string {
	out := []string{""}
	if m.status != "" {
		style := okStyle
		if m.statusErr {
			style = errStyle
		}
		if cw > 0 {
			style = style.Width(cw)
		}
		out = append(out, strings.Split(style.Render(m.status), "\n")...)
	}
	if m.inv.Empty() {
		return append(out, mutedStyle.Render("? help · q quit"))
	}
	details := "enter details"
	if m.showDetail {
		details = "enter hide details"
	}
	help := ui.CollapseHelp(cw,
		"j/k move · "+details+" · d dismiss · r restore · y copy fix · ? help · q quit",
		"d dismiss · r restore · y copy fix · ? help · q quit",
		"? help · q quit",
	)
	return append(out, mutedStyle.Render(help))
}

func (m Model) helpView() string {
	keys := [][2]string{
		{"j/k ↑/↓", "move"},
		{"g/G", "first / last notice"},
		{"enter", "show or hide the message, fix, and id"},
		{"d", "dismiss the selected live notice"},
		{"r", "restore the selected dismissed notice"},
		{"y or c", "copy the fix command"},
		{"?", "close this help"},
		{"q or esc", "quit"},
	}
	lines := []string{titleStyle.Render("Notice keys"), ""}
	for _, k := range keys {
		lines = append(lines, fixStyle.Render(pad(k[0], labelWidth))+primaryStyle.Render(k[1]))
	}
	lines = append(lines, "", mutedStyle.Render("Dismissals are saved in "+notice.DismissalRelPath+"."))
	box := overlayBox.Render(strings.Join(lines, "\n"))
	if m.width > 0 && m.height > 0 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
	}
	return box
}

func field(label, value string) string {
	return mutedStyle.Render(pad(label, labelWidth)) + value
}

func fit(s string, width int) string {
	if width <= 0 {
		return s
	}
	return ui.Truncate(s, width)
}

func pad(s string, width int) string {
	if gap := width - lipgloss.Width(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}

func wrap(s string, width int) []string {
	if width <= 0 {
		return []string{primaryStyle.Render(s)}
	}
	return strings.Split(primaryStyle.Width(width).Render(s), "\n")
}
