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

	listMinHeaderWindow = 3 // window lines needed to keep org headers while scrolling
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
	switch m.overlay {
	case listOverlayMove:
		return m.overlayView()
	case listOverlayOrgs:
		return m.orgPickerView()
	case listOverlayHelp:
		return m.helpView()
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
	budget := 0
	if m.height > 0 {
		budget = m.height
		if lay.boxed {
			budget = max(budget-2, 1)
		}
	}
	content := strings.Join(ui.CapFrame(lines, lay.cw, budget), "\n")
	if lay.boxed {
		return ui.FitFullscreenView(listBox.Render(content), m.height)
	}
	return ui.FitFullscreenView(content, m.height)
}

// bodyLines renders the campaign rows for the current window. Org headers are
// kept whenever there is room for at least three window lines: once the list
// must scroll, the window is centered on the cursor and its top line shows the
// org of the first row in view. Below that it degrades to a flat window. The
// selection is always on screen.
func (m listTUIModel) bodyLines(lay listLayout) []string {
	total := len(m.visible)
	if total == 0 {
		return []string{listMutedStyle.Render(m.emptyMessage())}
	}

	lines := m.bodyLineList()
	budget := lay.listRows
	if budget <= 0 || len(lines) <= budget {
		return m.renderLines(lines, lay.cw)
	}
	if budget-1 >= listMinHeaderWindow {
		if out, ok := m.headerWindow(lines, budget-1, lay.cw); ok {
			return out
		}
	}

	showIndicator := total > budget && budget >= 2
	rows := budget
	if showIndicator {
		rows = budget - 1
	}
	start, end := ui.WindowRange(m.cursor, total, rows)
	out := make([]string, 0, rows+1)
	for i := start; i < end; i++ {
		out = append(out, m.rowLine(m.visible[i], i == m.cursor, lay.cw))
	}
	if showIndicator {
		out = append(out, listIndicator(start, end, total))
	}
	return out
}

// listBodyLine is an org header or the index of a row in m.visible.
type listBodyLine struct {
	header bool
	org    string
	row    int
}

func (m listTUIModel) bodyLineList() []listBodyLine {
	out := make([]listBodyLine, 0, len(m.visible)+8)
	prevOrg := ""
	for i, e := range m.visible {
		if i == 0 || e.Org != prevOrg {
			out = append(out, listBodyLine{header: true, org: e.Org})
			prevOrg = e.Org
		}
		out = append(out, listBodyLine{org: e.Org, row: i})
	}
	return out
}

// headerWindow shows at most capacity lines containing the cursor plus a
// camp-count indicator. The top line is always an org header: a camp row there
// is replaced by its org's header (sticky), or dropped when the next org's
// header follows it. A trailing header with no rows is dropped. Of the windows
// that keep the cursor on screen it picks the one drawing the most lines, then
// the one with the cursor nearest the middle.
func (m listTUIModel) headerWindow(lines []listBodyLine, capacity, cw int) ([]string, bool) {
	cur := 0
	for i, l := range lines {
		if !l.header && l.row == m.cursor {
			cur = i
			break
		}
	}
	var window []listBodyLine
	bestOff := 0
	for start := max(cur-capacity+1, 0); start <= cur; start++ {
		w, pos, ok := headerWindowAt(lines, start, min(start+capacity, len(lines)), cur)
		if !ok {
			continue
		}
		off := pos - len(w)/2
		if off < 0 {
			off = -off
		}
		if window == nil || len(w) > len(window) || (len(w) == len(window) && off < bestOff) {
			window, bestOff = w, off
		}
	}
	if window == nil {
		return nil, false
	}

	out := m.renderLines(window, cw)
	first, last := -1, -1
	for _, l := range window {
		if l.header {
			continue
		}
		if first < 0 {
			first = l.row
		}
		last = l.row
	}
	return append(out, listIndicator(first, last+1, len(m.visible))), true
}

// headerWindowAt normalizes lines[start:end] and returns it with the cursor's
// position in it; ok is false when the cursor line would not be drawn.
func headerWindowAt(lines []listBodyLine, start, end, cur int) ([]listBodyLine, int, bool) {
	w := append([]listBodyLine(nil), lines[start:end]...)
	pos := cur - start
	if !w[0].header {
		if pos == 0 {
			return nil, 0, false
		}
		if w[1].header {
			w = w[1:]
			pos--
		} else {
			w[0] = listBodyLine{header: true, org: w[0].org}
		}
	}
	if w[len(w)-1].header {
		w = w[:len(w)-1]
	}
	return w, pos, true
}

func (m listTUIModel) renderLines(lines []listBodyLine, cw int) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if l.header {
			out = append(out, listOrgHeader.Render(orgLabel(l.org)))
			continue
		}
		out = append(out, m.rowLine(m.visible[l.row], l.row == m.cursor, cw))
	}
	return out
}

func (m listTUIModel) emptyMessage() string {
	camps := "camps"
	if m.activeOnly {
		camps = "active camps"
	}
	if m.orgFilter != "" {
		return fmt.Sprintf("no %s in org %q", camps, m.orgFilter)
	}
	return "no " + camps + " to show"
}

func listIndicator(start, end, total int) string {
	return listMutedStyle.Render(fmt.Sprintf("  [%d-%d of %d]", start+1, end, total))
}

func (m listTUIModel) rowLine(e campaignEntry, selected bool, cw int) string {
	prefix := "  " + ui.CursorGlyph(selected)
	displayName := e.Name
	if isRemoteListEntry(e) {
		displayName = e.Machine + " · " + e.Name
	}
	if cw <= 0 {
		name := styleName(fmt.Sprintf("%-*s", listNameMax, displayName), selected)
		return prefix + name + "  " + listStatusCell(e.Status) + "  " +
			listMutedStyle.Render(pathutil.AbbreviateHome(e.Path))
	}

	rem := cw - listRowPrefix
	if rem < 1 {
		return prefix
	}

	nameW, statusOn, pathW := listColumns(rem)
	row := prefix + styleName(fmt.Sprintf("%-*s", nameW, ui.Truncate(displayName, nameW)), selected)
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

func (m listTUIModel) topBar() string {
	mode := "all"
	if m.activeOnly {
		mode = "active only"
	}
	if m.remoteOn {
		mode += " + remotes"
	} else if m.remoteLoading {
		mode += " · loading remotes…"
	}
	count := ui.CountLabel(len(m.all), "camp", "camps")
	if len(m.visible) != len(m.all) {
		count = fmt.Sprintf("%d of %s", len(m.visible), count)
	}
	info := fmt.Sprintf("%s  .  showing: %s", count, mode)
	if m.orgFilter != "" {
		info += "  .  org: " + m.orgFilter
	}
	return listTitleStyle.Render("Camps") + "  " + listMutedStyle.Render(info)
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
