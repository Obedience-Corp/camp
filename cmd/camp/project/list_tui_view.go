package project

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	tuistyles "github.com/Obedience-Corp/camp/internal/intent/tui"
	"github.com/Obedience-Corp/camp/internal/pathutil"
	projectsvc "github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
)

var projPal = theme.TUI()

var (
	projTitleStyle  = tuistyles.TitleStyle
	projHelpStyle   = tuistyles.HelpStyle
	projErrStyle    = tuistyles.ErrorStyle
	projOkStyle     = tuistyles.SuccessStyle
	projHeaderStyle = lipgloss.NewStyle().Foreground(projPal.AccentAlt).Bold(true)
	projSelStyle    = lipgloss.NewStyle().Foreground(projPal.Accent).Bold(true)
	projNameStyle   = lipgloss.NewStyle().Foreground(projPal.TextPrimary)
	projMutedStyle  = lipgloss.NewStyle().Foreground(projPal.TextMuted)
	projBox         = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(projPal.BorderFocus).Padding(0, 1)
)

const (
	projNameMax   = 28
	projNameMin   = 8
	projTypeMax   = 10
	projTypeMin   = 2
	projSourceMax = 9
	projSourceMin = 3
	projPathMin   = 8
	projRowPrefix = 4

	projBoxOverhead  = 4
	projMinBoxWidth  = 30
	projMinBoxHeight = 8
	projMinFooterH   = 6
)

type projLayout struct {
	cw         int
	boxed      bool
	showFooter bool
	listRows   int
}

func (m projListModel) layout() projLayout {
	wKnown, hKnown := m.width > 0, m.height > 0
	l := projLayout{
		boxed:      (!wKnown || m.width >= projMinBoxWidth) && (!hKnown || m.height >= projMinBoxHeight),
		showFooter: !hKnown || m.height >= projMinFooterH,
	}
	if wKnown {
		l.cw = m.width
		if l.boxed {
			l.cw -= projBoxOverhead
		}
		l.cw = max(l.cw, 1)
	}
	if hKnown {
		chrome := 2
		if l.boxed {
			chrome += 2
		}
		if l.showFooter {
			chrome += 2
			chrome += m.footerExtra()
		}
		l.listRows = max(m.height-chrome, 1)
	}
	return l
}

func (m projListModel) footerExtra() int {
	extra := 0
	if m.status != "" {
		extra++
	}
	if m.overlay == projOverlaySearch {
		extra++
	}
	if len(m.visible) > 0 {
		extra++
		if m.showURLLine() {
			extra++
		}
	}
	return extra
}

func (m projListModel) showURLLine() bool {
	if len(m.visible) == 0 {
		return false
	}
	if m.visible[m.cursor].URL == "" {
		return false
	}
	return m.width == 0 || m.width >= 50
}

func (m projListModel) View() string {
	if m.quitting {
		return ""
	}
	lay := m.layout()
	lines := []string{m.topBar(), ""}
	lines = append(lines, m.bodyLines(lay)...)
	if lay.showFooter {
		lines = append(lines, "")
		lines = append(lines, m.footerLines(lay.cw)...)
	}
	return m.frame(lines, lay)
}

func (m projListModel) frame(lines []string, lay projLayout) string {
	budget := 0
	if m.height > 0 {
		budget = m.height
		if lay.boxed {
			budget = max(budget-2, 1)
		}
	}
	content := strings.Join(ui.CapFrame(lines, lay.cw, budget), "\n")
	if lay.boxed {
		return ui.FitFullscreenView(projBox.Render(content), m.height)
	}
	return ui.FitFullscreenView(content, m.height)
}

func (m projListModel) bodyLines(lay projLayout) []string {
	total := len(m.visible)
	if total == 0 {
		return []string{m.emptyBody()}
	}

	headers := m.groupBy != groupByNone
	budget := lay.listRows
	headerCount := 0
	if headers {
		headerCount = m.distinctGroups()
	}
	if budget <= 0 || total+headerCount <= budget {
		return m.renderRange(0, total, headers, lay.cw)
	}

	showIndicator := total > budget && budget >= 2
	rows := budget
	if showIndicator {
		rows--
	}
	sticky := headers && rows >= 2
	if sticky {
		rows--
	}
	start, end := ui.WindowRange(m.cursor, total, rows)
	var out []string
	if sticky {
		out = append(out, m.groupHeader(groupKey(m.groupBy, m.visible[m.cursor])))
	}
	out = append(out, m.renderRange(start, end, false, lay.cw)...)
	if showIndicator {
		out = append(out, projMutedStyle.Render(fmt.Sprintf("  [%d-%d of %d]", start+1, end, total)))
	}
	return out
}

func (m projListModel) emptyBody() string {
	if len(m.all) == 0 {
		return projMutedStyle.Render("no projects found  ·  camp project add <url>")
	}
	return projMutedStyle.Render("no projects match")
}

func (m projListModel) renderRange(start, end int, headers bool, cw int) []string {
	var out []string
	prev := ""
	for i := start; i < end; i++ {
		e := m.visible[i]
		key := groupKey(m.groupBy, e)
		if headers && key != prev {
			out = append(out, m.groupHeader(key))
			prev = key
		}
		out = append(out, m.rowLine(e, i == m.cursor, cw))
	}
	return out
}

func (m projListModel) groupHeader(key string) string {
	style := projHeaderStyle
	if m.groupBy == groupByType {
		style = typeStyle(key).Bold(true)
		if key == "other" {
			style = projHeaderStyle
		}
	}
	return style.Render(displayGroup(m.groupBy, key) + "  ·  " + fmt.Sprintf("%d", m.groupCount(key)))
}

func (m projListModel) groupCount(key string) int {
	n := 0
	for _, e := range m.visible {
		if groupKey(m.groupBy, e) == key {
			n++
		}
	}
	return n
}

func displayGroup(groupBy projGroupBy, key string) string {
	switch groupBy {
	case groupByType:
		switch key {
		case projectsvc.TypeGo:
			return "Go"
		case projectsvc.TypeRust:
			return "Rust"
		case projectsvc.TypeTypeScript:
			return "TypeScript"
		case "javascript":
			return "JavaScript"
		case projectsvc.TypePython:
			return "Python"
		default:
			return "Other"
		}
	case groupBySource:
		switch key {
		case "linked":
			return "Linked"
		case "campaign":
			return "Campaign"
		default:
			return "Submodule"
		}
	default:
		return key
	}
}

func (m projListModel) rowLine(e projectListItem, selected bool, cw int) string {
	prefix := "  " + ui.CursorGlyph(selected)
	if cw <= 0 {
		return prefix + styleProjName(fmt.Sprintf("%-*s", projNameMax, e.Name), selected) +
			"  " + typeBadge(e.Type, projTypeMin) +
			"  " + sourceBadge(e.Source, projSourceMin) +
			"  " + projMutedStyle.Render(e.RelPath)
	}

	rem := cw - projRowPrefix
	if rem < 1 {
		return prefix
	}
	nameW, typeW, sourceW, pathW := projColumns(rem)
	row := prefix + styleProjName(fmt.Sprintf("%-*s", nameW, ui.Truncate(e.Name, nameW)), selected)
	if typeW > 0 {
		row += "  " + typeBadge(e.Type, typeW)
	}
	if sourceW > 0 {
		row += "  " + sourceBadge(e.Source, sourceW)
	}
	if pathW > 0 {
		row += "  " + projMutedStyle.Render(ui.Truncate(e.RelPath, pathW))
	}
	return row
}

func projColumns(rem int) (nameW, typeW, sourceW, pathW int) {
	const gap = 2
	if rem < projNameMin {
		return max(rem, 1), 0, 0, 0
	}
	reserved := projNameMin
	hasType := rem >= reserved+gap+projTypeMin
	if hasType {
		reserved += gap + projTypeMin
	}
	hasSource := rem >= reserved+gap+projSourceMin
	if hasSource {
		reserved += gap + projSourceMin
	}
	hasPath := rem >= reserved+gap+projPathMin

	fixed := 0
	if hasType {
		fixed += gap + projTypeMin
		typeW = projTypeMin
	}
	if hasSource {
		fixed += gap + projSourceMin
		sourceW = projSourceMin
	}
	if hasPath {
		fixed += gap + projPathMin
		pathW = projPathMin
	}
	flex := rem - fixed
	nameW = min(flex, projNameMax)
	if nameW < projNameMin {
		nameW = projNameMin
	}
	flex -= nameW
	if hasType {
		extra := min(flex, projTypeMax-projTypeMin)
		typeW += extra
		flex -= extra
	}
	if hasSource {
		extra := min(flex, projSourceMax-projSourceMin)
		sourceW += extra
		flex -= extra
	}
	if hasPath {
		pathW += flex
	}
	return nameW, typeW, sourceW, pathW
}

func typeBadge(t string, width int) string {
	label := typeLabel(t)
	if width > 0 {
		label = fmt.Sprintf("%-*s", width, ui.Truncate(label, width))
	}
	return typeStyle(t).Render(label)
}

func sourceBadge(s string, width int) string {
	label := sourceLabel(s)
	if width > 0 {
		label = fmt.Sprintf("%-*s", width, ui.Truncate(label, width))
	}
	return projMutedStyle.Render(label)
}

func typeStyle(t string) lipgloss.Style {
	switch t {
	case projectsvc.TypeGo:
		return lipgloss.NewStyle().Foreground(projPal.AccentAlt)
	case projectsvc.TypeRust:
		return lipgloss.NewStyle().Foreground(projPal.Error)
	case projectsvc.TypeTypeScript, "javascript":
		return lipgloss.NewStyle().Foreground(projPal.Warning)
	case projectsvc.TypePython:
		return lipgloss.NewStyle().Foreground(projPal.Success)
	default:
		return projMutedStyle
	}
}

func (m projListModel) topBar() string {
	mode := "grouped by " + m.groupBy.label()
	if m.query != "" {
		mode += "  ·  /" + m.query
	}
	return projTitleStyle.Render("Projects") + "  " +
		projMutedStyle.Render(fmt.Sprintf("%s  ·  %s", ui.CountLabel(len(m.all), "project", "projects"), mode))
}

func (m projListModel) footerLines(cw int) []string {
	var lines []string
	if s := m.statusLine(); s != "" {
		lines = append(lines, s)
	}
	if len(m.visible) > 0 {
		lines = append(lines, m.detailLines(cw)...)
	}
	if m.overlay == projOverlaySearch {
		lines = append(lines, m.input.View())
		lines = append(lines, projHelpStyle.Render("enter: apply  ·  esc: clear  ·  ↑↓ move"))
		return lines
	}
	lines = append(lines, m.footer(cw))
	return lines
}

func (m projListModel) detailLines(cw int) []string {
	e := m.visible[m.cursor]
	path := pathutil.AbbreviateHome(e.AbsPath)
	if cw > 0 {
		path = ui.Truncate(path, cw)
	}
	lines := []string{projMutedStyle.Render(path)}
	if m.showURLLine() {
		url := e.URL
		if cw > 0 {
			url = ui.Truncate(url, cw)
		}
		lines = append(lines, projMutedStyle.Render(url))
	}
	return lines
}

func (m projListModel) footer(cw int) string {
	help := ui.CollapseHelp(cw,
		"g: go  ·  j/k: move  ·  /: search  ·  f: group  ·  y: copy  ·  q: quit",
		"j/k move  ·  g go  ·  / search  ·  f group  ·  q quit",
		"q quit",
	)
	return projHelpStyle.Render(help)
}

func (m projListModel) statusLine() string {
	if m.status == "" {
		return ""
	}
	if m.statusErr {
		return projErrStyle.Render(m.status)
	}
	return projOkStyle.Render(m.status)
}

func styleProjName(s string, selected bool) string {
	if selected {
		return projSelStyle.Render(s)
	}
	return projNameStyle.Render(s)
}
