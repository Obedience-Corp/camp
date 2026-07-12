package org

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Obedience-Corp/camp/internal/config"
	tuistyles "github.com/Obedience-Corp/camp/internal/intent/tui"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
)

var orgPal = theme.TUI()

var (
	orgTitleStyle  = tuistyles.TitleStyle
	orgHelpStyle   = tuistyles.HelpStyle
	orgErrStyle    = tuistyles.ErrorStyle
	orgOkStyle     = tuistyles.SuccessStyle
	orgPaneFocused = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(orgPal.BorderFocus).Padding(0, 1)
	orgPaneBlurred = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(orgPal.Border).Padding(0, 1)
	orgSelStyle    = lipgloss.NewStyle().Foreground(orgPal.Accent).Bold(true)
	orgRowStyle    = lipgloss.NewStyle().Foreground(orgPal.TextPrimary)
	orgMutedStyle  = lipgloss.NewStyle().Foreground(orgPal.TextMuted)
	orgHereStyle   = lipgloss.NewStyle().Foreground(orgPal.Success).Bold(true)
	orgCountStyle  = lipgloss.NewStyle().Foreground(orgPal.TextSecondary)
	orgActiveStyle = lipgloss.NewStyle().Foreground(orgPal.Success)
	orgBadgeActive = lipgloss.NewStyle().Foreground(orgPal.Success)
	orgBadgeOff    = lipgloss.NewStyle().Foreground(orgPal.Warning)
	orgBadgeRef    = lipgloss.NewStyle().Foreground(orgPal.AccentAlt)
	orgHeaderBar   = lipgloss.NewStyle().Foreground(orgPal.TextMuted)
)

const (
	orgPaneOverheadW = 4 // L/R border + horizontal padding
	orgPaneOverheadH = 2 // T/B border
	orgPaneGapW      = 2 // spaces between dual panes
	orgMinBoxWidth   = 30
	orgMinBoxHeight  = 8
	orgMinFooterH    = 6
	orgRowPrefixW    = 2 // "> " / "  "
	orgHereMarkW     = 2 // "* " / "  "
	orgNameMinW      = 4
)

// orgLayout is the size-dependent shape of a frame. Zero cw/listRows means
// unbounded (size not yet known), matching camp list/festivals.
type orgLayout struct {
	cw         int
	dual       bool
	boxed      bool
	showFooter bool
	orgW       int
	memW       int
	listRows   int
}

func (m orgTUIModel) layout() orgLayout {
	wKnown, hKnown := m.width > 0, m.height > 0
	l := orgLayout{
		boxed:      (!wKnown || m.width >= orgMinBoxWidth) && (!hKnown || m.height >= orgMinBoxHeight),
		showFooter: !hKnown || m.height >= orgMinFooterH,
		dual:       !wKnown || m.width >= orgTUIMinWide,
	}
	if wKnown {
		l.cw = max(m.width, 1)
		if l.dual {
			usable := m.width - orgPaneGapW
			if l.boxed {
				usable -= 2 * orgPaneOverheadW
			}
			usable = max(usable, 2)
			// Orgs stay narrower; members get the remaining share.
			l.orgW = max(usable/3, orgNameMinW)
			if l.orgW > usable-1 {
				l.orgW = max(usable/2, 1)
			}
			l.memW = max(usable-l.orgW, 1)
		} else {
			pane := m.width
			if l.boxed {
				pane -= orgPaneOverheadW
			}
			pane = max(pane, 1)
			l.orgW, l.memW = pane, pane
		}
	}
	if hKnown {
		// topBar + optional status + optional footer; body fills the rest.
		chrome := 1 // top bar
		if l.showFooter {
			chrome++ // footer
			if m.status != "" {
				chrome++ // status line
			}
		}
		bodyH := max(m.height-chrome, 1)
		inner := bodyH
		if l.boxed {
			inner = max(bodyH-orgPaneOverheadH, 1)
		}
		// One line for the pane title; remaining rows are list capacity.
		l.listRows = max(inner-1, 1)
	}
	return l
}

// styleMemberStatus renders a campaign lifecycle status as a colored badge.
func styleMemberStatus(status string) string {
	switch status {
	case config.StatusActive:
		return orgBadgeActive.Render(status)
	case config.StatusInactive:
		return orgBadgeOff.Render(status)
	case config.StatusReference:
		return orgBadgeRef.Render(status)
	default:
		return orgMutedStyle.Render(status)
	}
}

func (m orgTUIModel) View() string {
	if m.quitting {
		return ""
	}
	if len(m.orgs) == 0 {
		return m.emptyView()
	}
	if m.overlay != overlayNone {
		return m.overlayView()
	}

	lay := m.layout()
	lines := []string{m.topBar(lay.cw)}
	lines = append(lines, m.bodyLines(lay)...)
	if lay.showFooter {
		if s := m.statusLine(); s != "" {
			lines = append(lines, s)
		}
		lines = append(lines, m.footer(lay.cw))
	}
	return m.frame(lines, lay)
}

func (m orgTUIModel) emptyView() string {
	lay := m.layout()
	lines := []string{
		orgTitleStyle.Render("Orgs"),
		"",
		orgMutedStyle.Render("No campaigns registered yet. Run camp init or camp register."),
	}
	if lay.showFooter {
		lines = append(lines, "", orgHelpStyle.Render("q: quit"))
	}
	return m.frame(lines, lay)
}

// frame hard-caps line count and width so a short/narrow split can never be
// overpainted. Dual-pane body lines are pre-joined; this is the final guard.
func (m orgTUIModel) frame(lines []string, lay orgLayout) string {
	if m.height > 0 {
		budget := max(m.height, 1)
		if len(lines) > budget {
			lines = lines[:budget]
		}
	}
	// Body lines from dual panes may already span full terminal width; clamp
	// everything to the outer canvas when known.
	cw := lay.cw
	if cw <= 0 && m.width > 0 {
		cw = m.width
	}
	return strings.Join(ui.ClampLines(lines, cw), "\n") + "\n"
}

func (m orgTUIModel) bodyLines(lay orgLayout) []string {
	if lay.dual {
		left := m.renderOrgPane(lay)
		right := m.renderMemberPane(lay)
		joined := lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", orgPaneGapW), right)
		return strings.Split(joined, "\n")
	}
	if m.pane == paneMembers {
		return strings.Split(m.renderMemberPane(lay), "\n")
	}
	return strings.Split(m.renderOrgPane(lay), "\n")
}

func (m orgTUIModel) topBar(cw int) string {
	title := orgTitleStyle.Render("Campaign Orgs")
	meta := orgHeaderBar.Render(fmt.Sprintf("%s . %s",
		ui.CountLabel(len(m.orgs), "org", "orgs"),
		ui.CountLabel(m.totalCampaigns(), "campaign", "campaigns")))
	line := title + "  " + meta
	if cw > 0 {
		return ui.ClampWidth(line, cw)
	}
	return line
}

func (m orgTUIModel) totalCampaigns() int {
	n := 0
	for _, o := range m.orgs {
		n += o.Campaigns
	}
	return n
}

func (m orgTUIModel) renderOrgPane(lay orgLayout) string {
	cw := lay.orgW
	lines := []string{ui.ClampWidth(orgTitleStyle.Render("Orgs"), cw)}
	lines = append(lines, m.orgListLines(lay)...)
	return m.finishPane(paneOrgs, lines, cw, lay)
}

func (m orgTUIModel) renderMemberPane(lay orgLayout) string {
	cw := lay.memW
	title := "Members"
	if m.focusedOrg != "" {
		title = fmt.Sprintf("Members of %q", m.focusedOrg)
	}
	lines := []string{ui.ClampWidth(orgTitleStyle.Render(title), cw)}
	lines = append(lines, m.memberListLines(lay)...)
	return m.finishPane(paneMembers, lines, cw, lay)
}

func (m orgTUIModel) finishPane(p orgPane, lines []string, cw int, lay orgLayout) string {
	// Pad to a stable dual-pane height so JoinHorizontal lines up cleanly.
	if lay.listRows > 0 {
		want := lay.listRows + 1 // title + listRows
		for len(lines) < want {
			lines = append(lines, "")
		}
		if len(lines) > want {
			lines = lines[:want]
		}
	}
	content := strings.Join(ui.ClampLines(lines, cw), "\n")
	if !lay.boxed {
		return content
	}
	return m.paneStyle(p).Render(content)
}

func (m orgTUIModel) orgListLines(lay orgLayout) []string {
	total := len(m.orgs)
	if total == 0 {
		return []string{orgMutedStyle.Render("no orgs")}
	}
	budget := lay.listRows
	if budget <= 0 {
		return m.renderOrgRange(0, total, lay.orgW)
	}
	showInd := total > budget && budget >= 2
	rows := budget
	if showInd {
		rows = budget - 1
	}
	start, end := ui.WindowRange(m.orgCursor, total, rows)
	out := m.renderOrgRange(start, end, lay.orgW)
	if showInd {
		out = append(out, orgMutedStyle.Render(fmt.Sprintf("[%d-%d of %d]", start+1, end, total)))
	}
	return out
}

func (m orgTUIModel) memberListLines(lay orgLayout) []string {
	total := len(m.members)
	if total == 0 {
		return []string{orgMutedStyle.Render("no campaigns in this org")}
	}
	budget := lay.listRows
	if budget <= 0 {
		return m.renderMemberRange(0, total, lay.memW)
	}
	showInd := total > budget && budget >= 2
	rows := budget
	if showInd {
		rows = budget - 1
	}
	start, end := ui.WindowRange(m.memCursor, total, rows)
	out := m.renderMemberRange(start, end, lay.memW)
	if showInd {
		out = append(out, orgMutedStyle.Render(fmt.Sprintf("[%d-%d of %d]", start+1, end, total)))
	}
	return out
}

func (m orgTUIModel) renderOrgRange(start, end, cw int) []string {
	out := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		out = append(out, m.orgRow(i, cw))
	}
	return out
}

func (m orgTUIModel) renderMemberRange(start, end, cw int) []string {
	out := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		out = append(out, m.memberRow(i, cw))
	}
	return out
}

func (m orgTUIModel) orgRow(i, cw int) string {
	o := m.orgs[i]
	selected := i == m.orgCursor
	focused := selected && m.pane == paneOrgs
	prefix := ui.CursorGlyph(focused || selected)

	if cw <= 0 {
		name := styleOrgName(fmt.Sprintf("%-16s", o.Org), focused, selected)
		counts := orgCountStyle.Render(fmt.Sprintf("%d", o.Campaigns)) + " " +
			orgActiveStyle.Render(fmt.Sprintf("(%d active)", o.Active))
		return prefix + name + "  " + counts
	}

	rem := cw - orgRowPrefixW
	if rem < 1 {
		return ui.ClampWidth(prefix, cw)
	}

	// Prefer name; append counts only when they fit after a minimum name width.
	countPlain := fmt.Sprintf("%d (%d active)", o.Campaigns, o.Active)
	countW := lipgloss.Width(countPlain)
	nameW := rem
	showCounts := false
	if rem >= orgNameMinW+1+countW {
		showCounts = true
		nameW = rem - 1 - countW
	}
	name := styleOrgName(ui.Truncate(o.Org, nameW), focused, selected)
	// Pad plain runes before styling width is hard; clamp the finished row.
	row := prefix + name
	if showCounts {
		row += " " + orgCountStyle.Render(fmt.Sprintf("%d", o.Campaigns)) +
			" " + orgActiveStyle.Render(fmt.Sprintf("(%d active)", o.Active))
	}
	return ui.ClampWidth(row, cw)
}

func (m orgTUIModel) memberRow(i, cw int) string {
	mem := m.members[i]
	selected := i == m.memCursor
	focused := selected && m.pane == paneMembers
	prefix := ui.CursorGlyph(focused || selected)
	here := "  "
	if mem.ID == m.currentID && m.currentID != "" {
		here = orgHereStyle.Render("* ")
	}

	if cw <= 0 {
		name := styleOrgName(fmt.Sprintf("%-24s", mem.Name), focused, selected)
		return prefix + here + name + " " + styleMemberStatus(mem.Status)
	}

	rem := cw - orgRowPrefixW
	if rem < 1 {
		return ui.ClampWidth(prefix, cw)
	}
	// here mark always reserved when present as two columns of content budget.
	rem -= orgHereMarkW
	if rem < 1 {
		return ui.ClampWidth(prefix+here, cw)
	}

	statusPlain := mem.Status
	statusW := lipgloss.Width(statusPlain)
	nameW := rem
	showStatus := false
	if rem >= orgNameMinW+1+statusW {
		showStatus = true
		nameW = rem - 1 - statusW
	}
	name := styleOrgName(ui.Truncate(mem.Name, nameW), focused, selected)
	row := prefix + here + name
	if showStatus {
		row += " " + styleMemberStatus(mem.Status)
	}
	return ui.ClampWidth(row, cw)
}

func styleOrgName(s string, focused, selected bool) string {
	switch {
	case focused:
		return orgSelStyle.Render(s)
	case selected:
		return orgRowStyle.Render(s)
	default:
		return orgMutedStyle.Render(s)
	}
}

func (m orgTUIModel) paneStyle(p orgPane) lipgloss.Style {
	if m.pane == p {
		return orgPaneFocused
	}
	return orgPaneBlurred
}

func (m orgTUIModel) footer(cw int) string {
	var full, mid, short string
	if m.pane == paneOrgs {
		full = "j/k: orgs . l: members . n: new org . N: new campaign . x: delete (empty) . r: rename . q: quit"
		mid = "j/k orgs . l members . n/N new . x del . r ren . q"
		short = "j/k . l . q"
	} else {
		full = "j/k: members . h: orgs . m: move . c: create . d: default . q: quit"
		mid = "j/k members . h orgs . m move . c create . q"
		short = "j/k . h . q"
	}
	help := full
	if cw > 0 && lipgloss.Width(help) > cw {
		help = mid
	}
	if cw > 0 && lipgloss.Width(help) > cw {
		help = short
	}
	if cw > 0 && lipgloss.Width(help) > cw {
		help = "q"
	}
	return orgHelpStyle.Render(help)
}

func (m orgTUIModel) statusLine() string {
	if m.status == "" {
		return ""
	}
	if m.statusErr {
		return orgErrStyle.Render(m.status)
	}
	return orgOkStyle.Render(m.status)
}

func (m orgTUIModel) overlayView() string {
	lay := m.layout()
	var lines []string
	if m.overlay == overlayConfirmDelete {
		lines = []string{
			orgTitleStyle.Render(fmt.Sprintf("Delete empty org %q?", m.pendingDelete)),
			"",
			orgHelpStyle.Render("y/enter: delete . n/esc: cancel"),
		}
		return m.frame(lines, lay)
	}

	var prompt string
	var help string
	switch m.overlay {
	case overlayRename:
		prompt = fmt.Sprintf("Rename org %q to:", m.orgs[m.orgCursor].Org)
		help = "enter: confirm . esc: cancel"
	case overlayMove:
		prompt = fmt.Sprintf("Move %q to org:", m.members[m.memCursor].Name)
		help = "enter: confirm . esc: cancel"
	case overlayCreate:
		prompt = fmt.Sprintf("Create org and add %q:", m.members[m.memCursor].Name)
		help = "enter: confirm . esc: cancel"
	case overlayCreateEmpty:
		prompt = "New org name:"
		help = "enter: create . esc: cancel"
	case overlayNewCampaign:
		prompt = fmt.Sprintf("New campaign in org %q:", m.pendingOrg)
		help = "enter: create . esc: cancel"
	}
	lines = []string{
		orgTitleStyle.Render(prompt),
		"",
		m.input.View(),
		"",
		orgMutedStyle.Render("existing orgs: " + m.orgNamesCSV()),
		"",
		orgHelpStyle.Render(help),
	}
	return m.frame(lines, lay)
}
