package festivals

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	tuistyles "github.com/Obedience-Corp/camp/internal/intent/tui"
	"github.com/Obedience-Corp/camp/internal/ui"
)

var (
	festTitleStyle = tuistyles.TitleStyle
	festHelpStyle  = tuistyles.HelpStyle
	festErrStyle   = tuistyles.ErrorStyle
	festOkStyle    = tuistyles.SuccessStyle
	festMutedStyle = lipgloss.NewStyle().Foreground(festPal.TextMuted)
	festBox        = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(festPal.BorderFocus).Padding(0, 1)
)

const (
	festRowPrefix    = 4 // two leading spaces + two-column cursor glyph
	festBoxOverhead  = 4
	festMinBoxWidth  = 30
	festMinBoxHeight = 8
	festMinFooterH   = 6
)

type festLayout struct {
	cw         int
	boxed      bool
	showFooter bool
	listRows   int
}

func (m festivalsTUIModel) layout() festLayout {
	wKnown, hKnown := m.width > 0, m.height > 0
	l := festLayout{
		boxed:      (!wKnown || m.width >= festMinBoxWidth) && (!hKnown || m.height >= festMinBoxHeight),
		showFooter: !hKnown || m.height >= festMinFooterH,
	}
	if wKnown {
		l.cw = m.width
		if l.boxed {
			l.cw -= festBoxOverhead
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
			if m.status != "" {
				chrome++
			}
		}
		l.listRows = max(m.height-chrome, 1)
	}
	return l
}

func (m festivalsTUIModel) View() string {
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

func (m festivalsTUIModel) frame(lines []string, lay festLayout) string {
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
		return ui.FitFullscreenView(festBox.Render(content), m.height)
	}
	return ui.FitFullscreenView(content, m.height)
}

func (m festivalsTUIModel) renderRange(start, end int, headers bool, cw int) []string {
	var out []string
	prevOrg, prevCampaign := "", ""
	for i := start; i < end; i++ {
		it := m.visible[i]
		if headers && it.Org != prevOrg {
			out = append(out, festOrgHeader.Render(it.Org))
			prevOrg, prevCampaign = it.Org, "" // org change forces a campaign header
		}
		if headers && it.Campaign != prevCampaign {
			out = append(out, "  "+festCampaignHeader.Render(it.Campaign))
			prevCampaign = it.Campaign
		}
		out = append(out, m.rowLine(it, i == m.cursor, cw))
	}
	return out
}

func (m festivalsTUIModel) rowLine(it festivalItem, selected bool, cw int) string {
	prefix := "  " + ui.CursorGlyph(selected)
	if cw <= 0 { // size unknown (first frame): render at full column width
		return prefix + festRow(it, festNameMax+2+festStatusW+2+festProgressW, selected)
	}
	rem := cw - festRowPrefix
	if rem < 1 {
		return prefix
	}
	return prefix + festRow(it, rem, selected)
}

func (m festivalsTUIModel) topBar() string {
	mode := "off"
	if m.activeOnly {
		mode = "on"
	}
	return festTitleStyle.Render("Festivals") + "  " +
		festMutedStyle.Render(fmt.Sprintf("%s across %s  .  active only: %s",
			ui.CountLabel(len(m.visible), "festival", "festivals"),
			ui.CountLabel(m.distinctCampaigns(), "campaign", "campaigns"), mode))
}

func (m festivalsTUIModel) footer(cw int) string {
	help := "g/enter: go . j/k: move . f: active-only . y: copy . q: quit"
	if cw > 0 && lipgloss.Width(help) > cw {
		help = "j/k move . g go . f filter . q quit"
	}
	if cw > 0 && lipgloss.Width(help) > cw {
		help = "q quit"
	}
	return festHelpStyle.Render(help)
}

func (m festivalsTUIModel) statusLine() string {
	if m.status == "" {
		return ""
	}
	if m.statusErr {
		return festErrStyle.Render(m.status)
	}
	return festOkStyle.Render(m.status)
}

func (m festivalsTUIModel) distinctOrgs() int {
	seen := map[string]bool{}
	for _, e := range m.visible {
		seen[e.Org] = true
	}
	return len(seen)
}

func (m festivalsTUIModel) distinctCampaigns() int {
	seen := map[string]bool{}
	for _, e := range m.visible {
		seen[e.Org+"/"+e.Campaign] = true
	}
	return len(seen)
}

func (m festivalsTUIModel) bodyLines(lay festLayout) []string {
	total := len(m.visible)
	if total == 0 {
		msg := "no festivals to show"
		if m.activeOnly {
			msg = "no active festivals"
		}
		return []string{festMutedStyle.Render(msg)}
	}

	budget := lay.listRows
	if budget <= 0 || total+m.distinctOrgs()+m.distinctCampaigns() <= budget {
		return m.renderRange(0, total, true, lay.cw)
	}

	// Scrolling: reserve one line for the breadcrumb and one for the indicator.
	rows := budget
	showChrome := budget >= 3
	if showChrome {
		rows = budget - 2
	}
	start, end := ui.WindowRange(m.cursor, total, rows)

	var out []string
	if showChrome {
		first := m.visible[start]
		out = append(out, festMutedStyle.Render("  "+first.Org+" / "+first.Campaign))
	}
	out = append(out, m.renderRange(start, end, false, lay.cw)...)
	if showChrome {
		out = append(out, festMutedStyle.Render(fmt.Sprintf("  [%d-%d of %d]", start+1, end, total)))
	}
	return out
}
