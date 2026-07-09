package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Obedience-Corp/camp/internal/config"
	tuistyles "github.com/Obedience-Corp/camp/internal/intent/tui"
	"github.com/Obedience-Corp/camp/internal/pathutil"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
)

var listPal = theme.TUI()

var (
	listTitleStyle = tuistyles.TitleStyle
	listHelpStyle  = tuistyles.HelpStyle
	listErrStyle   = tuistyles.ErrorStyle
	listOkStyle    = tuistyles.SuccessStyle
	listOrgHeader  = lipgloss.NewStyle().Foreground(listPal.AccentAlt).Bold(true)
	listSelStyle   = lipgloss.NewStyle().Foreground(listPal.Accent).Bold(true)
	listNameStyle  = lipgloss.NewStyle().Foreground(listPal.TextPrimary)
	listMutedStyle = lipgloss.NewStyle().Foreground(listPal.TextMuted)
	listBadgeOn    = lipgloss.NewStyle().Foreground(listPal.Success)
	listBadgeOff   = lipgloss.NewStyle().Foreground(listPal.Warning)
	listBadgeRef   = lipgloss.NewStyle().Foreground(listPal.AccentAlt)
	listBox        = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(listPal.BorderFocus).Padding(0, 1)
)

const (
	listNameMax   = 22
	listNameMin   = 6
	listPathMin   = 8
	listStatusW   = 10
	listRowPrefix = 4 // two leading spaces plus a two-column cursor

	listBoxOverhead  = 4 // rounded border plus horizontal padding
	listMinBoxWidth  = 30
	listMinBoxHeight = 8
	listMinFooterH   = 6
)

func listStatusCell(status string) string {
	padded := fmt.Sprintf("%-10s", status)
	switch status {
	case config.StatusActive:
		return listBadgeOn.Render(padded)
	case config.StatusInactive:
		return listBadgeOff.Render(padded)
	case config.StatusReference:
		return listBadgeRef.Render(padded)
	default:
		return listMutedStyle.Render(padded)
	}
}

// listLayout captures the size-dependent shape of a render. cw and listRows of
// zero or less mean "unbounded" (size not yet known), preserving the full-size
// layout for tests and the first frame before a WindowSizeMsg arrives.
type listLayout struct {
	cw         int
	boxed      bool
	showFooter bool
	listRows   int
}

func (m listTUIModel) layout() listLayout {
	wKnown, hKnown := m.width > 0, m.height > 0
	l := listLayout{
		boxed:      (!wKnown || m.width >= listMinBoxWidth) && (!hKnown || m.height >= listMinBoxHeight),
		showFooter: !hKnown || m.height >= listMinFooterH,
	}
	if wKnown {
		l.cw = m.width
		if l.boxed {
			l.cw -= listBoxOverhead
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

func (m listTUIModel) View() string {
	if m.quitting {
		return ""
	}
	if m.overlay != listOverlayNone {
		return m.overlayView()
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

// frame clamps content to the terminal, hard-capping the line count as a final
// guard so an unexpectedly tall render can never overflow a short window, then
// wraps it in the border when the size allows.
func (m listTUIModel) frame(lines []string, lay listLayout) string {
	if m.height > 0 {
		budget := m.height
		if lay.boxed {
			budget -= 2
		}
		budget = max(budget, 1)
		if len(lines) > budget {
			lines = lines[:budget]
		}
	}
	content := strings.Join(ui.ClampLines(lines, lay.cw), "\n")
	if lay.boxed {
		return listBox.Render(content) + "\n"
	}
	return content + "\n"
}

// bodyLines renders the campaign rows for the current window. Org headers are
// kept while the whole list fits the height budget; once the list must scroll
// it degrades to a flat, cursor-centered window with a position indicator so
// the selection always stays on screen.
func (m listTUIModel) bodyLines(lay listLayout) []string {
	total := len(m.visible)
	if total == 0 {
		return []string{listMutedStyle.Render("no campaigns to show")}
	}

	budget := lay.listRows
	if budget <= 0 || total+m.distinctOrgs() <= budget {
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
		out = append(out, listMutedStyle.Render(fmt.Sprintf("  [%d-%d of %d]", start+1, end, total)))
	}
	return out
}

func (m listTUIModel) renderRange(start, end int, headers bool, cw int) []string {
	var out []string
	prevOrg := ""
	for i := start; i < end; i++ {
		e := m.visible[i]
		if headers && e.Org != prevOrg {
			out = append(out, listOrgHeader.Render(e.Org))
			prevOrg = e.Org
		}
		out = append(out, m.rowLine(e, i == m.cursor, cw))
	}
	return out
}

func (m listTUIModel) rowLine(e campaignEntry, selected bool, cw int) string {
	prefix := "  " + ui.CursorGlyph(selected)
	if cw <= 0 {
		name := styleName(fmt.Sprintf("%-*s", listNameMax, e.Name), selected)
		return prefix + name + "  " + listStatusCell(e.Status) + "  " +
			listMutedStyle.Render(pathutil.AbbreviateHome(e.Path))
	}

	rem := cw - listRowPrefix
	if rem < 1 {
		return prefix
	}

	nameW, statusOn, pathW := listColumns(rem)
	row := prefix + styleName(fmt.Sprintf("%-*s", nameW, ui.Truncate(e.Name, nameW)), selected)
	if statusOn {
		row += "  " + listStatusCell(e.Status)
	}
	if pathW > 0 {
		row += "  " + listMutedStyle.Render(ui.Truncate(pathutil.AbbreviateHome(e.Path), pathW))
	}
	return row
}

// listColumns splits the width remaining after the row prefix into a name
// column, an optional status column, and an optional path column, dropping the
// rightmost columns first as space runs out.
func listColumns(rem int) (nameW int, statusOn bool, pathW int) {
	if rem < listStatusW+2+listNameMin {
		return min(rem, listNameMax), false, 0
	}
	nameBudget := rem - listStatusW - 2
	if nameBudget < listNameMin+2+listPathMin {
		return min(nameBudget, listNameMax), true, 0
	}
	nameW = min(nameBudget-2-listPathMin, listNameMax)
	return nameW, true, nameBudget - 2 - nameW
}

func (m listTUIModel) distinctOrgs() int {
	seen := map[string]bool{}
	for _, e := range m.visible {
		seen[e.Org] = true
	}
	return len(seen)
}

func (m listTUIModel) topBar() string {
	mode := "all"
	if m.activeOnly {
		mode = "active only"
	}
	return listTitleStyle.Render("Campaigns") + "  " +
		listMutedStyle.Render(fmt.Sprintf("%s  .  showing: %s", ui.CountLabel(len(m.all), "campaign", "campaigns"), mode))
}

func (m listTUIModel) footer(cw int) string {
	help := "g: go . j/k: move . s: status . m: org . y: copy . f: filter . q: quit"
	if cw > 0 && lipgloss.Width(help) > cw {
		help = "j/k move . g go . f filter . q quit"
	}
	if cw > 0 && lipgloss.Width(help) > cw {
		help = "q quit"
	}
	return listHelpStyle.Render(help)
}

func (m listTUIModel) statusLine() string {
	if m.status == "" {
		return ""
	}
	if m.statusErr {
		return listErrStyle.Render(m.status)
	}
	return listOkStyle.Render(m.status)
}

func (m listTUIModel) overlayView() string {
	lay := m.layout()
	title := "Move to org:"
	if len(m.visible) > 0 && m.cursor < len(m.visible) {
		title = fmt.Sprintf("Move %q to org:", m.visible[m.cursor].Name)
	}
	lines := []string{
		listTitleStyle.Render(title),
		"",
		m.input.View(),
		"",
		listMutedStyle.Render("existing orgs: " + m.orgNamesCSV()),
		"",
		listHelpStyle.Render("enter: confirm . esc: cancel"),
	}
	return m.frame(lines, lay)
}

func styleName(s string, selected bool) string {
	if selected {
		return listSelStyle.Render(s)
	}
	return listNameStyle.Render(s)
}

