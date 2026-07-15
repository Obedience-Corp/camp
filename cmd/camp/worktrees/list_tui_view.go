package worktrees

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	tuistyles "github.com/Obedience-Corp/camp/internal/intent/tui"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
)

var wtPal = theme.TUI()

var (
	wtTitleStyle = tuistyles.TitleStyle
	wtHelpStyle  = tuistyles.HelpStyle
	wtErrStyle   = tuistyles.ErrorStyle
	wtOkStyle    = tuistyles.SuccessStyle
	wtProjHeader = lipgloss.NewStyle().Foreground(wtPal.AccentAlt).Bold(true)
	wtSelStyle   = lipgloss.NewStyle().Foreground(wtPal.Accent).Bold(true)
	wtNameStyle  = lipgloss.NewStyle().Foreground(wtPal.TextPrimary)
	wtMutedStyle = lipgloss.NewStyle().Foreground(wtPal.TextMuted)
	wtBadgeOff   = lipgloss.NewStyle().Foreground(wtPal.Warning)
	wtBox        = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(wtPal.BorderFocus).Padding(0, 1)
)

const (
	wtNameMax   = 24
	wtNameMin   = 8
	wtBranchMax = 26
	wtBranchMin = 6
	wtStatusW   = 6  // "stale"
	wtAccessedW = 14 // "12 minutes ago"
	wtRowPrefix = 4  // two leading spaces plus a two-column cursor

	wtBoxOverhead  = 4
	wtMinBoxWidth  = 30
	wtMinBoxHeight = 8
	wtMinFooterH   = 6
)

// wtLayout captures the size-dependent shape of a render. cw and listRows of
// zero or less mean "unbounded" (size not yet known), preserving the full-size
// layout for tests and the first frame before a WindowSizeMsg arrives.
type wtLayout struct {
	cw         int
	boxed      bool
	showFooter bool
	listRows   int
}

func (m wtListModel) layout() wtLayout {
	wKnown, hKnown := m.width > 0, m.height > 0
	l := wtLayout{
		boxed:      (!wKnown || m.width >= wtMinBoxWidth) && (!hKnown || m.height >= wtMinBoxHeight),
		showFooter: !hKnown || m.height >= wtMinFooterH,
	}
	if wKnown {
		l.cw = m.width
		if l.boxed {
			l.cw -= wtBoxOverhead
		}
		l.cw = max(l.cw, 1)
	}
	if hKnown {
		chrome := 2 // title plus blank separator
		if l.boxed {
			chrome += 2 // top and bottom border
		}
		if l.showFooter {
			chrome += 2 // blank plus footer
			if m.status != "" {
				chrome++
			}
		}
		l.listRows = max(m.height-chrome, 1)
	}
	return l
}

func (m wtListModel) View() string {
	if m.quitting {
		return ""
	}
	lay := m.layout()
	lines := []string{m.topBar(), ""}
	lines = append(lines, m.bodyLines(lay)...)
	if lay.showFooter {
		lines = append(lines, "")
		if s := m.statusLine(); s != "" {
			lines = append(lines, s)
		}
		lines = append(lines, m.footer(lay.cw))
	}
	return m.frame(lines, lay)
}

// frame clamps content to the terminal, hard-capping the line count so a tall
// render can never overflow a short window, then wraps it in the border when
// the size allows.
func (m wtListModel) frame(lines []string, lay wtLayout) string {
	budget := 0
	if m.height > 0 {
		budget = m.height
		if lay.boxed {
			budget = max(budget-2, 1)
		}
	}
	content := strings.Join(ui.CapFrame(lines, lay.cw, budget), "\n")
	if lay.boxed {
		return ui.FitFullscreenView(wtBox.Render(content), m.height)
	}
	return ui.FitFullscreenView(content, m.height)
}

// bodyLines renders the worktree rows for the current window. Project headers
// are kept while the whole list fits the height budget; once it must scroll it
// degrades to a flat, cursor-centered window with a position indicator so the
// selection always stays on screen.
func (m wtListModel) bodyLines(lay wtLayout) []string {
	total := len(m.visible)
	if total == 0 {
		return []string{wtMutedStyle.Render("no worktrees to show")}
	}

	budget := lay.listRows
	if budget <= 0 || total+m.distinctProjects() <= budget {
		return m.renderRange(0, total, true, lay.cw)
	}

	showIndicator := total > budget && budget >= 2
	rows := budget
	if showIndicator {
		rows = budget - 1
	}
	start, end := ui.WindowRange(m.cursor, total, rows)
	out := m.renderRange(start, end, false, lay.cw)
	if showIndicator {
		out = append(out, wtMutedStyle.Render(fmt.Sprintf("  [%d-%d of %d]", start+1, end, total)))
	}
	return out
}

func (m wtListModel) renderRange(start, end int, headers bool, cw int) []string {
	var out []string
	prevProject := ""
	for i := start; i < end; i++ {
		e := m.visible[i]
		if headers && e.Project != prevProject {
			out = append(out, wtProjHeader.Render(e.Project))
			prevProject = e.Project
		}
		out = append(out, m.rowLine(e, i == m.cursor, cw))
	}
	return out
}

func (m wtListModel) rowLine(e WorktreeListItem, selected bool, cw int) string {
	prefix := "  " + ui.CursorGlyph(selected)
	if cw <= 0 {
		name := styleWtName(fmt.Sprintf("%-*s", wtNameMax, e.Name), selected)
		return prefix + name + "  " +
			wtMutedStyle.Render(fmt.Sprintf("%-*s", wtBranchMin, e.Branch)) + "  " +
			wtStatusCell(e, wtStatusW) + "  " +
			wtMutedStyle.Render(e.LastAccessed)
	}

	rem := cw - wtRowPrefix
	if rem < 1 {
		return prefix
	}

	nameW, branchW, statusW, accessedW := wtColumns(rem)
	row := prefix + styleWtName(fmt.Sprintf("%-*s", nameW, ui.Truncate(e.Name, nameW)), selected)
	if branchW > 0 {
		row += "  " + wtMutedStyle.Render(fmt.Sprintf("%-*s", branchW, ui.Truncate(e.Branch, branchW)))
	}
	if statusW > 0 {
		row += "  " + wtStatusCell(e, statusW)
	}
	if accessedW > 0 {
		row += "  " + wtMutedStyle.Render(ui.Truncate(e.LastAccessed, accessedW))
	}
	return row
}

// wtColumns splits the width remaining after the row prefix into name, branch,
// status, and last-accessed columns, dropping the rightmost optional columns
// (accessed, then status, then branch) as space runs out. NAME always shows.
// Each column's minimum is reserved before the next is admitted, so a wide
// branch cannot starve the status or accessed columns.
func wtColumns(rem int) (nameW, branchW, statusW, accessedW int) {
	const gap = 2
	if rem < wtNameMin {
		return max(rem, 1), 0, 0, 0
	}
	reserved := wtNameMin
	hasBranch := rem >= reserved+gap+wtBranchMin
	if hasBranch {
		reserved += gap + wtBranchMin
	}
	hasStatus := rem >= reserved+gap+wtStatusW
	if hasStatus {
		reserved += gap + wtStatusW
		statusW = wtStatusW
	}
	hasAccessed := rem >= reserved+gap+wtAccessedW
	if hasAccessed {
		accessedW = wtAccessedW
	}

	fixed := 0
	if hasStatus {
		fixed += gap + wtStatusW
	}
	if hasAccessed {
		fixed += gap + wtAccessedW
	}
	nbBudget := rem - fixed // name (+ gap + branch)
	if !hasBranch {
		return min(nbBudget, wtNameMax), 0, statusW, accessedW
	}
	nameW = min(nbBudget-gap-wtBranchMin, wtNameMax)
	if nameW < wtNameMin {
		nameW = wtNameMin
	}
	branchW = nbBudget - gap - nameW
	if branchW > wtBranchMax {
		branchW = wtBranchMax
	}
	return nameW, branchW, statusW, accessedW
}

func wtStatusCell(e WorktreeListItem, width int) string {
	label, style := "ok", wtMutedStyle
	if e.Stale {
		label, style = "stale", wtBadgeOff
	}
	if width > 0 {
		return style.Render(fmt.Sprintf("%-*s", width, ui.Truncate(label, width)))
	}
	return style.Render(label)
}

func (m wtListModel) topBar() string {
	mode := "all"
	if m.staleOnly {
		mode = "stale only"
	}
	return wtTitleStyle.Render("Worktrees") + "  " +
		wtMutedStyle.Render(fmt.Sprintf("%s  .  showing: %s", ui.CountLabel(len(m.all), "worktree", "worktrees"), mode))
}

func (m wtListModel) footer(cw int) string {
	help := ui.CollapseHelp(cw,
		"g: go . j/k: move . y: copy . f: filter . q: quit",
		"j/k move . g go . f filter . q quit",
		"q quit",
	)
	return wtHelpStyle.Render(help)
}

func (m wtListModel) statusLine() string {
	if m.status == "" {
		return ""
	}
	if m.statusErr {
		return wtErrStyle.Render(m.status)
	}
	return wtOkStyle.Render(m.status)
}

func styleWtName(s string, selected bool) string {
	if selected {
		return wtSelStyle.Render(s)
	}
	return wtNameStyle.Render(s)
}
