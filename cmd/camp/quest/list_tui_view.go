//go:build dev

package quest

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	tuistyles "github.com/Obedience-Corp/camp/internal/intent/tui"
	"github.com/Obedience-Corp/camp/internal/quest"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
)

var questPal = theme.TUI()

var (
	questTitleStyle = tuistyles.TitleStyle
	questHelpStyle  = tuistyles.HelpStyle
	questErrStyle   = tuistyles.ErrorStyle
	questOkStyle    = tuistyles.SuccessStyle
	questSelStyle   = lipgloss.NewStyle().Foreground(questPal.Accent).Bold(true)
	questNameStyle  = lipgloss.NewStyle().Foreground(questPal.TextPrimary)
	questMutedStyle = lipgloss.NewStyle().Foreground(questPal.TextMuted)
	questBox        = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(questPal.BorderFocus).Padding(0, 1)
)

const (
	questNameMax      = 24
	questNameMin      = 8
	questStatusW      = 9 // "completed"
	questPathMin      = 8
	questRowPrefix    = 4 // two leading spaces plus a two-column cursor
	questBoxOverhead  = 4
	questMinBoxWidth  = 30
	questMinBoxHeight = 8
	questMinFooterH   = 6
)

// questLayout captures the size-dependent shape of a render. cw and listRows of
// zero or less mean "unbounded" (size not yet known), preserving the full-size
// layout for tests and the first frame before a WindowSizeMsg arrives.
type questLayout struct {
	cw         int
	boxed      bool
	showFooter bool
	listRows   int
}

func (m questListModel) layout() questLayout {
	wKnown, hKnown := m.width > 0, m.height > 0
	l := questLayout{
		boxed:      (!wKnown || m.width >= questMinBoxWidth) && (!hKnown || m.height >= questMinBoxHeight),
		showFooter: !hKnown || m.height >= questMinFooterH,
	}
	if wKnown {
		l.cw = m.width
		if l.boxed {
			l.cw -= questBoxOverhead
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

func (m questListModel) View() string {
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

func (m questListModel) frame(lines []string, lay questLayout) string {
	budget := 0
	if m.height > 0 {
		budget = m.height
		if lay.boxed {
			budget = max(budget-2, 1)
		}
	}
	content := strings.Join(ui.CapFrame(lines, lay.cw, budget), "\n")
	if lay.boxed {
		return ui.FitFullscreenView(questBox.Render(content), m.height)
	}
	return ui.FitFullscreenView(content, m.height)
}

func (m questListModel) bodyLines(lay questLayout) []string {
	total := len(m.visible)
	if total == 0 {
		return []string{m.emptyBody()}
	}

	budget := lay.listRows
	if budget <= 0 || total+m.distinctStatuses() <= budget {
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
		out = append(out, questMutedStyle.Render(fmt.Sprintf("  [%d-%d of %d]", start+1, end, total)))
	}
	return out
}

func (m questListModel) emptyBody() string {
	if len(m.all) == 0 {
		return questMutedStyle.Render("no quests found")
	}
	return questMutedStyle.Render("no quests match")
}

func (m questListModel) renderRange(start, end int, headers bool, cw int) []string {
	var out []string
	prev := quest.Status("")
	for i := start; i < end; i++ {
		e := m.visible[i]
		if headers && e.Status != prev {
			out = append(out, m.statusHeader(e.Status))
			prev = e.Status
		}
		out = append(out, m.rowLine(e, i == m.cursor, cw))
	}
	return out
}

func (m questListModel) statusHeader(status quest.Status) string {
	return ui.GetQuestStatusStyle(string(status)).Bold(true).Render(string(status))
}

func (m questListModel) rowLine(e questListItem, selected bool, cw int) string {
	prefix := "  " + ui.CursorGlyph(selected)
	if cw <= 0 {
		name := styleQuestName(fmt.Sprintf("%-*s", questNameMax, e.Name), selected)
		return prefix + name + "  " + questStatusCell(e.Status) + "  " +
			questMutedStyle.Render(e.RelPath)
	}

	rem := cw - questRowPrefix
	if rem < 1 {
		return prefix
	}

	nameW, statusOn, pathW := questColumns(rem)
	row := prefix + styleQuestName(fmt.Sprintf("%-*s", nameW, ui.Truncate(e.Name, nameW)), selected)
	if statusOn {
		row += "  " + questStatusCell(e.Status)
	}
	if pathW > 0 {
		row += "  " + questMutedStyle.Render(ui.Truncate(e.RelPath, pathW))
	}
	return row
}

// questColumns splits the width remaining after the row prefix into name,
// status, and path, dropping the rightmost optional columns first. NAME always
// shows. Matches camp list's column collapse.
func questColumns(rem int) (nameW int, statusOn bool, pathW int) {
	if rem < questStatusW+2+questNameMin {
		return min(rem, questNameMax), false, 0
	}
	nameBudget := rem - questStatusW - 2
	if nameBudget < questNameMin+2+questPathMin {
		return min(nameBudget, questNameMax), true, 0
	}
	nameW = min(nameBudget-2-questPathMin, questNameMax)
	return nameW, true, nameBudget - 2 - nameW
}

func questStatusCell(status quest.Status) string {
	padded := fmt.Sprintf("%-*s", questStatusW, string(status))
	return ui.GetQuestStatusStyle(string(status)).Render(padded)
}

func (m questListModel) topBar() string {
	return questTitleStyle.Render("Quests") + "  " +
		questMutedStyle.Render(fmt.Sprintf("%s  .  showing: %s", ui.CountLabel(len(m.all), "quest", "quests"), m.filter.label()))
}

func (m questListModel) footer(cw int) string {
	help := ui.CollapseHelp(cw,
		"g: go . j/k: move . y: copy . f: filter . q: quit",
		"j/k move . g go . f filter . q quit",
		"q quit",
	)
	return questHelpStyle.Render(help)
}

func (m questListModel) statusLine() string {
	if m.status == "" {
		return ""
	}
	if m.statusErr {
		return questErrStyle.Render(m.status)
	}
	return questOkStyle.Render(m.status)
}

func styleQuestName(s string, selected bool) string {
	if selected {
		return questSelStyle.Render(s)
	}
	return questNameStyle.Render(s)
}
