package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	tuistyles "github.com/Obedience-Corp/camp/internal/intent/tui"
	"github.com/Obedience-Corp/camp/internal/jobs"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
)

var jobsPal = theme.TUI()

var (
	jobsTitleStyle  = tuistyles.TitleStyle
	jobsHelpStyle   = tuistyles.HelpStyle
	jobsErrStyle    = tuistyles.ErrorStyle
	jobsOkStyle     = tuistyles.SuccessStyle
	jobsSelStyle    = lipgloss.NewStyle().Foreground(jobsPal.Accent).Bold(true)
	jobsPrimary     = lipgloss.NewStyle().Foreground(jobsPal.TextPrimary)
	jobsMutedStyle  = lipgloss.NewStyle().Foreground(jobsPal.TextMuted)
	jobsHeaderStyle = lipgloss.NewStyle().Foreground(jobsPal.TextMuted).Bold(true)
	jobsBox         = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(jobsPal.BorderFocus).Padding(0, 1)
	jobsFailedStyle = lipgloss.NewStyle().Foreground(jobsPal.Error).Bold(true)
	jobsStallStyle  = lipgloss.NewStyle().Foreground(jobsPal.Warning).Bold(true)
	jobsRunStyle    = lipgloss.NewStyle().Foreground(jobsPal.Success)
	jobsPendStyle   = lipgloss.NewStyle().Foreground(jobsPal.TextMuted)
	jobsOverlayBox  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(jobsPal.BorderFocus).Padding(1, 2)
)

const (
	jobsRowPrefix    = 4 // two leading spaces + two-column cursor
	jobsBoxOverhead  = 4
	jobsMinBoxWidth  = 40
	jobsMinBoxHeight = 10
	jobsMinFooterH   = 7

	jobsStateW   = 9
	jobsIDW      = 25
	jobsLaneW    = 18
	jobsKindW    = 12
	jobsCreatedW = 16
	jobsAgeW     = 6
)

type jobsLayout struct {
	cw         int
	boxed      bool
	showFooter bool
	listRows   int
}

func (m jobsTUIModel) layout() jobsLayout {
	wKnown, hKnown := m.width > 0, m.height > 0
	l := jobsLayout{
		boxed:      (!wKnown || m.width >= jobsMinBoxWidth) && (!hKnown || m.height >= jobsMinBoxHeight),
		showFooter: !hKnown || m.height >= jobsMinFooterH,
	}
	if wKnown {
		l.cw = m.width
		if l.boxed {
			l.cw -= jobsBoxOverhead
		}
		l.cw = max(l.cw, 1)
	}
	if hKnown {
		chrome := 2 // title + blank
		if l.boxed {
			chrome += 2
		}
		if l.showFooter {
			chrome += 2 // blank + help
			if m.status != "" {
				chrome++
			}
			if e, ok := m.selected(); ok && e.LastError != "" {
				chrome++
			}
		}
		l.listRows = max(m.height-chrome, 1)
	}
	return l
}

func (m jobsTUIModel) View() string {
	if m.quitting {
		return ""
	}
	if m.overlay != jobsOverlayNone {
		return m.overlayView()
	}

	lay := m.layout()
	lines := []string{m.topBar(), ""}
	lines = append(lines, m.bodyLines(lay)...)
	if lay.showFooter {
		lines = append(lines, "")
		if detail := m.detailLine(lay.cw); detail != "" {
			lines = append(lines, detail)
		}
		if s := m.statusLine(); s != "" {
			lines = append(lines, s)
		}
		lines = append(lines, m.footer(lay.cw))
	}
	return m.frame(lines, lay)
}

func (m jobsTUIModel) frame(lines []string, lay jobsLayout) string {
	budget := 0
	if m.height > 0 {
		budget = m.height
		if lay.boxed {
			budget = max(budget-2, 1)
		}
	}
	content := strings.Join(ui.CapFrame(lines, lay.cw, budget), "\n")
	if lay.boxed {
		return ui.FitFullscreenView(jobsBox.Render(content), m.height)
	}
	return ui.FitFullscreenView(content, m.height)
}

func (m jobsTUIModel) topBar() string {
	failed, running, pending, stalled := 0, 0, 0, 0
	for _, e := range m.entries {
		switch e.State {
		case "failed":
			failed++
		case "running":
			running++
			if e.Stalled {
				stalled++
			}
		default:
			pending++
		}
	}
	summary := fmt.Sprintf("%s  .  pending %d  running %d  failed %d",
		ui.CountLabel(len(m.entries), "job", "jobs"), pending, running, failed)
	if stalled > 0 {
		summary += fmt.Sprintf("  .  %d stalled", stalled)
	}
	if m.busy {
		summary += "  .  working…"
	}
	return jobsTitleStyle.Render("Jobs") + "  " + jobsMutedStyle.Render(summary)
}

func (m jobsTUIModel) bodyLines(lay jobsLayout) []string {
	if len(m.entries) == 0 {
		return []string{jobsMutedStyle.Render("No deferred commits queued.")}
	}

	header := m.headerLine(lay.cw)
	budget := lay.listRows
	if budget > 0 {
		budget-- // reserve header
	}
	total := len(m.entries)
	if budget <= 0 || total <= budget {
		out := []string{header}
		out = append(out, m.renderRange(0, total, lay.cw)...)
		return out
	}

	showIndicator := total > budget && budget >= 2
	rows := budget
	if showIndicator {
		rows = budget - 1
	}
	start, end := ui.WindowRange(m.cursor, total, rows)
	out := []string{header}
	out = append(out, m.renderRange(start, end, lay.cw)...)
	if showIndicator {
		out = append(out, jobsMutedStyle.Render(fmt.Sprintf("  [%d-%d of %d]", start+1, end, total)))
	}
	return out
}

func (m jobsTUIModel) renderRange(start, end, cw int) []string {
	now := time.Now()
	out := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		out = append(out, m.rowLine(m.entries[i], i == m.cursor, now, cw))
	}
	return out
}

func (m jobsTUIModel) headerLine(cw int) string {
	prefix := "  " + "  "
	cols := jobsColumns(cw - jobsRowPrefix)
	line := prefix + padTrunc("STATE", cols.stateW) + "  " + padTrunc("ID", cols.idW)
	if cols.laneW > 0 {
		line += "  " + padTrunc("LANE", cols.laneW)
	}
	if cols.kindW > 0 {
		line += "  " + padTrunc("KIND", cols.kindW)
	}
	if cols.createdOn {
		line += "  " + padTrunc("CREATED", cols.createdW)
	}
	if cols.ageOn {
		line += "  " + padTrunc("AGE", cols.ageW)
	}
	if cols.whatW > 0 {
		line += "  " + padTrunc("WHAT", cols.whatW)
	}
	return jobsHeaderStyle.Render(line)
}

type jobsCols struct {
	stateW, idW, laneW, kindW, createdW, ageW, whatW int
	createdOn, ageOn                                 bool
}

// jobsColumns collapses optional columns from the right as width shrinks.
// STATE and ID always show; WHAT shrinks first, then AGE, then CREATED.
func jobsColumns(rem int) jobsCols {
	c := jobsCols{
		stateW: jobsStateW, idW: jobsIDW, laneW: jobsLaneW, kindW: jobsKindW,
		createdW: jobsCreatedW, ageW: jobsAgeW,
		createdOn: true, ageOn: true,
	}
	if rem <= 0 {
		c.whatW = 24
		return c
	}
	fixed := c.stateW + 2 + c.idW + 2 + c.laneW + 2 + c.kindW
	if rem < fixed+2+8 {
		// Drop lane/kind first on very narrow terminals.
		c.laneW, c.kindW = 0, 0
		c.createdOn, c.ageOn = false, false
		c.whatW = max(rem-c.stateW-2-c.idW, 0)
		return c
	}
	rest := rem - fixed
	needCreated := 2 + c.createdW
	needAge := 2 + c.ageW
	// Prefer keeping CREATED+AGE over a long WHAT: the absolute enqueue time
	// is why this browser exists, and AGE is the relative twin. WHAT truncates.
	switch {
	case rest < needCreated+2+8:
		c.createdOn, c.ageOn = false, false
		c.whatW = max(rest-2, 0)
	case rest < needCreated+needAge+2+8:
		c.ageOn = false
		c.whatW = max(rest-needCreated-2, 0)
	default:
		c.whatW = max(rest-needCreated-needAge-2, 0)
	}
	return c
}

func (m jobsTUIModel) rowLine(e jobs.Entry, selected bool, now time.Time, cw int) string {
	prefix := "  " + ui.CursorGlyph(selected)
	cols := jobsColumns(cw - jobsRowPrefix)
	state := e.State
	if e.Stalled {
		state = "stalled"
	}
	stateCell := styleJobsState(padTrunc(state, cols.stateW), state)
	idCell := styleJobsCell(padTrunc(e.ID, cols.idW), selected)
	laneCell := ""
	if cols.laneW > 0 {
		laneCell = "  " + styleJobsCell(padTrunc(e.Lane, cols.laneW), selected)
	}
	kindCell := ""
	if cols.kindW > 0 {
		kindCell = "  " + styleJobsCell(padTrunc(string(e.Kind), cols.kindW), selected)
	}
	row := prefix + stateCell + "  " + idCell + laneCell + kindCell
	if cols.createdOn {
		row += "  " + styleJobsMuted(padTrunc(jobs.FormatCreated(e.CreatedAt), cols.createdW), selected)
	}
	if cols.ageOn {
		row += "  " + styleJobsMuted(padTrunc(jobs.ShortDuration(e.Age(now)), cols.ageW), selected)
	}
	if cols.whatW > 0 {
		what := jobsWhat(e, m.superseded[e.ID])
		row += "  " + styleJobsCell(padTrunc(what, cols.whatW), selected)
	}
	return row
}

func styleJobsState(s, state string) string {
	switch state {
	case "failed":
		return jobsFailedStyle.Render(s)
	case "stalled":
		return jobsStallStyle.Render(s)
	case "running":
		return jobsRunStyle.Render(s)
	default:
		return jobsPendStyle.Render(s)
	}
}

func styleJobsCell(s string, selected bool) string {
	if selected {
		return jobsSelStyle.Render(s)
	}
	return jobsPrimary.Render(s)
}

func styleJobsMuted(s string, selected bool) string {
	if selected {
		return jobsSelStyle.Render(s)
	}
	return jobsMutedStyle.Render(s)
}

func padTrunc(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = ui.Truncate(s, width)
	if lipgloss.Width(s) >= width {
		return s
	}
	return fmt.Sprintf("%-*s", width, s)
}

func (m jobsTUIModel) detailLine(cw int) string {
	e, ok := m.selected()
	if !ok || e.LastError == "" {
		return ""
	}
	msg := "error: " + e.LastError
	if cw > 0 {
		msg = ui.Truncate(msg, cw)
	}
	return jobsMutedStyle.Render(msg)
}

func (m jobsTUIModel) statusLine() string {
	if m.status == "" {
		return ""
	}
	if m.statusErr {
		return jobsErrStyle.Render(m.status)
	}
	return jobsOkStyle.Render(m.status)
}

func (m jobsTUIModel) footer(cw int) string {
	help := m.helpText()
	return jobsHelpStyle.Render(ui.CollapseHelp(cw, help, compactJobsHelp(help), "q quit"))
}

func (m jobsTUIModel) helpText() string {
	parts := []string{"j/k: move", "u: refresh", "y: copy id", "q: quit"}
	e, ok := m.selected()
	if !ok {
		if failed, _, _, _ := jobsActionCounts(m.entries, m.superseded); failed > 0 {
			parts = append([]string{"R: retry all", "D: drop all"}, parts...)
		}
		return strings.Join(parts, " . ")
	}
	switch {
	case e.State == "failed" && !m.superseded[e.ID]:
		parts = append([]string{"r: retry", "d: drop", "R: retry all", "D: drop all"}, parts...)
	case e.State == "failed":
		parts = append([]string{"d: drop", "D: drop all"}, parts...)
	case e.Stalled && e.Stuck:
		parts = append([]string{"s: serve", "x: drop running"}, parts...)
	case e.Stalled:
		parts = append([]string{"x: drop running"}, parts...)
	case e.State == "pending":
		parts = append([]string{"s: serve"}, parts...)
	}
	return strings.Join(parts, " . ")
}

func compactJobsHelp(full string) string {
	// Keep the action verbs; drop the "move/refresh/copy" chrome first.
	keep := make([]string, 0, 4)
	for _, part := range strings.Split(full, " . ") {
		switch {
		case strings.HasPrefix(part, "j/k"), strings.HasPrefix(part, "u:"), strings.HasPrefix(part, "y:"):
			continue
		default:
			keep = append(keep, part)
		}
	}
	if len(keep) == 0 {
		return "q quit"
	}
	return strings.Join(keep, " . ")
}

func (m jobsTUIModel) overlayView() string {
	var title, body, help string
	switch m.overlay {
	case jobsOverlayConfirmDrop:
		title = "Drop job"
		body = "Give up on " + m.confirmID + "?\nFiles stay on disk for your next commit."
		help = "y: drop . n/esc: cancel"
	case jobsOverlayConfirmDropRunning:
		title = "Drop running job"
		body = "Stop the worker and drop " + m.confirmID + "?\nFiles stay on disk for your next commit."
		help = "y: drop . n/esc: cancel"
	case jobsOverlayConfirmDropAll:
		title = "Drop all failed"
		body = "Give up on every failed job?\nFiles stay on disk for your next commit."
		help = "y: drop all . n/esc: cancel"
	case jobsOverlayConfirmRetryAll:
		title = "Retry all failed"
		body = "Requeue every failed job and start workers?"
		help = "y: retry all . n/esc: cancel"
	default:
		return ""
	}
	content := jobsTitleStyle.Render(title) + "\n\n" +
		jobsPrimary.Render(body) + "\n\n" +
		jobsHelpStyle.Render(help)
	box := jobsOverlayBox.Render(content)
	if m.width > 0 && m.height > 0 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
	}
	return box
}
