package main

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/Obedience-Corp/camp/internal/approvals"
	tuistyles "github.com/Obedience-Corp/camp/internal/intent/tui"
	"github.com/Obedience-Corp/camp/internal/jobs"
	"github.com/Obedience-Corp/camp/internal/ui"
)

// The browser is built from what camp's other two-pane browsers use: the
// shared title style and focus cursor, the machine browser's pane borders and
// display-width wrapping, and the jobs palette. What is its own is the use of
// colour for the decisions: green and red mark approve and reject, warning
// marks a request that cannot be decided here.
var (
	apHeading = jobsTitleStyle
	apTitle   = lipgloss.NewStyle().Foreground(jobsPal.TextPrimary).Bold(true)
	apText    = jobsPrimary
	apMuted   = jobsMutedStyle
	apCommand = lipgloss.NewStyle().Foreground(jobsPal.AccentAlt).Bold(true)
	apApprove = lipgloss.NewStyle().Foreground(jobsPal.Success).Bold(true)
	apReject  = lipgloss.NewStyle().Foreground(jobsPal.Error).Bold(true)
	apWarn    = jobsStallStyle
)

const (
	approvalsReadingWidth = 92
	// approvalsPaneRows are the rows a pane adds to its body: two borders and
	// the title line.
	approvalsPaneRows = 3
)

func approvalCursor(selected bool) string {
	if selected {
		return tuistyles.FocusCursor() + " "
	}
	return tuistyles.EmptyCursor() + " "
}

type approvalsLayoutMode int

const (
	// approvalsWide puts the list on the left and the request on the right.
	approvalsWide approvalsLayoutMode = iota
	// approvalsStacked puts a short list above the request.
	approvalsStacked
	// approvalsCompact drops the boxes for very small terminals.
	approvalsCompact
)

type approvalsLayout struct {
	w, h           int // h == 0: height unknown, nothing is capped
	mode           approvalsLayoutMode
	listW, detailW int
	listH, detailH int // pane heights with borders; 0 means natural
	notices        []string
}

// approvalsChromeRows are the rows outside the panes: title, status, keys.
const approvalsChromeRows = 3

func (m approvalsTUIModel) layout() approvalsLayout {
	l := approvalsLayout{w: m.width, h: max(m.height, 0)}
	if l.w <= 0 {
		l.w = 100
	}
	l.notices = approvalsNotices(m.snap)
	l.listW, l.detailW = l.w, l.w
	if l.h == 0 {
		l.mode = approvalsStacked
		return l
	}
	avail := l.h - approvalsChromeRows - len(l.notices)
	// Panes are as tall as the longest request needs and no taller, so one
	// short request is not two mostly empty boxes. Sizing to the longest keeps
	// the frame still while the selection moves.
	need := 0
	for _, row := range m.snap.rows {
		need = max(need, len(approvalRequestLines(row, time.Time{}, l.w)))
	}
	switch {
	case l.w < 60 || avail < 8:
		l.mode = approvalsCompact
	case l.w >= 100:
		l.mode = approvalsWide
		l.listW = min(max(l.w/3, 34), 48)
		l.detailW = l.w - l.listW - 1
		for _, row := range m.snap.rows {
			need = max(need, len(approvalRequestLines(row, time.Time{}, l.detailW-4)))
		}
		need = max(need, 3*len(m.snap.rows)-1)
		l.detailH = min(avail, max(need+approvalsPaneRows, 8))
		l.listH = l.detailH
	default:
		l.mode = approvalsStacked
		l.listH = min(max(len(m.snap.rows), 1), 4) + approvalsPaneRows
		l.detailH = min(avail-l.listH, max(need+approvalsPaneRows, 6))
		if l.detailH < 5 {
			l.mode = approvalsCompact
		}
	}
	return l
}

// requestViewport is how many request lines fit, and how many there are.
func (m approvalsTUIModel) requestViewport() (visible, total int) {
	row, ok := m.selected()
	if !ok {
		return 0, 0
	}
	l := m.layout()
	if l.mode == approvalsCompact || l.detailH == 0 {
		return 0, 0
	}
	return l.detailH - approvalsPaneRows, len(approvalRequestLines(row, time.Now(), l.detailW-4))
}

func (m approvalsTUIModel) View() string {
	if m.quitting {
		return ""
	}
	l := m.layout()
	var lines []string
	switch m.mode {
	case approvalsConfirm:
		lines = m.confirmLines(l.w, l.h)
	case approvalsReason:
		lines = m.reasonLines(l.w, l.h)
	default:
		lines = m.browseLines(l)
	}
	return ui.FitFullscreenView(strings.Join(ui.CapFrame(lines, l.w, l.h), "\n"), l.h)
}

func (m approvalsTUIModel) browseLines(l approvalsLayout) []string {
	now := time.Now()
	summary := "nothing waiting on you"
	if n := len(m.snap.rows); n > 0 {
		summary = fmt.Sprintf("%d waiting on you", n)
	}
	if m.busy {
		summary += " · working…"
	}
	lines := []string{" " + apHeading.Render("Approvals") + "  " + apMuted.Render(summary)}
	for _, note := range l.notices {
		lines = append(lines, " "+apWarn.Render(ui.Truncate(note, l.w-1)))
	}

	row, ok := m.selected()
	var body []string
	switch {
	case !ok:
		body = []string{"", " " + apMuted.Render("Nothing is waiting on your approval.")}
	case l.mode == approvalsWide:
		left := m.listPane(l.listW, l.listH, true, now)
		right := m.requestPane(row, l.detailW, l.detailH, now)
		for i := range left {
			body = append(body, left[i]+" "+right[i])
		}
	case l.mode == approvalsStacked:
		body = append(m.listPane(l.listW, l.listH, false, now), m.requestPane(row, l.detailW, l.detailH, now)...)
	default:
		body = m.compactBody(row, l, now)
	}
	if l.h > 0 {
		// The status line and the keys always get the two rows after the body.
		if room := max(l.h-len(lines)-2, 0); len(body) > room {
			body = body[:room]
		}
	}
	lines = append(lines, body...)

	status := ""
	if m.status != "" {
		style := apMuted
		if m.statusErr {
			style = jobsFailedStyle
		}
		status = " " + style.Render(ui.Truncate(m.status, l.w-1))
	}
	return append(lines, status, m.keyBar(l.w))
}

func (m approvalsTUIModel) compactBody(row approvalRow, l approvalsLayout, now time.Time) []string {
	start, end := ui.WindowRange(m.cursor, len(m.snap.rows), 3)
	var body []string
	for i := start; i < end; i++ {
		body = append(body, approvalRowLine(m.snap.rows[i], i == m.cursor, l.w, now))
	}
	body = append(body, "", apHeading.Render("Request "+row.ID))
	return append(body, approvalRequestLines(row, now, l.w)...)
}

// listPane renders the waiting requests. roomy gives each one a second line.
func (m approvalsTUIModel) listPane(w, h int, roomy bool, now time.Time) []string {
	inner := w - 4
	total := len(m.snap.rows)
	capacity := total
	if h > 0 {
		capacity = h - approvalsPaneRows
		if roomy {
			capacity = (capacity + 1) / 3
		}
		capacity = max(capacity, 1)
	}
	start, end := ui.WindowRange(m.cursor, total, capacity)
	var body []string
	for i := start; i < end; i++ {
		r, selected := m.snap.rows[i], i == m.cursor
		if !roomy {
			body = append(body, approvalRowLine(r, selected, inner, now))
			continue
		}
		if i > start {
			body = append(body, "")
		}
		title := apText
		if selected {
			title = apTitle
		}
		meta := apMuted.Render(approvalRowMeta(r, now))
		if !r.ready.Actionable {
			meta = apWarn.Render("not actionable here")
		}
		body = append(body, approvalCursor(selected)+title.Render(ui.Truncate(r.Title, inner-2)), approvalCursor(false)+meta)
	}
	foot := ""
	if end-start < total {
		foot = fmt.Sprintf("%d–%d of %d", start+1, end, total)
	}
	return approvalsPane(fmt.Sprintf("Waiting (%d)", total), foot, body, w, h, true)
}

func approvalRowMeta(r approvalRow, now time.Time) string {
	return fmt.Sprintf("%s · step %d · %s ago", approvalSourceLabel(r.SourceKind), r.Step,
		jobs.ShortDuration(approvalAge(r.Pending, now)))
}

// approvalRowLine is one request on one line: title left, source and age right.
func approvalRowLine(r approvalRow, selected bool, w int, now time.Time) string {
	meta := approvalSourceLabel(r.SourceKind) + " " + jobs.ShortDuration(approvalAge(r.Pending, now))
	metaStyle := apMuted
	if !r.ready.Actionable {
		meta, metaStyle = "not actionable", apWarn
	}
	glyph, titleStyle := approvalCursor(selected), apText
	if selected {
		titleStyle = apTitle
	}
	room := w - 2 - 2 - lipgloss.Width(meta)
	if room < 8 {
		return glyph + titleStyle.Render(ui.Truncate(r.Title, max(w-2, 1)))
	}
	title := ui.Truncate(r.Title, room)
	pad := strings.Repeat(" ", room-lipgloss.Width(title)+2)
	return glyph + titleStyle.Render(title) + pad + metaStyle.Render(meta)
}

func (m approvalsTUIModel) requestPane(row approvalRow, w, h int, now time.Time) []string {
	body := approvalRequestLines(row, now, w-4)
	foot := ""
	if visible := h - approvalsPaneRows; h > 0 && len(body) > visible {
		top := min(max(m.scroll, 0), len(body)-visible)
		foot = fmt.Sprintf("%d–%d of %d lines", top+1, top+visible, len(body))
		body = body[top : top+visible]
	}
	return approvalsPane("Request "+row.ID, foot, body, w, h, false)
}

// A label is "Goal:" or "Note 1:" opening a line, not "See https://…".
var approvalLabelRe = regexp.MustCompile(`^([A-Z][A-Za-z0-9 /-]{0,24}:)(?: |$)`)

// approvalRequestLines is the request as the browser shows it: what is being
// asked first, where it came from in two quiet lines, then the body.
func approvalRequestLines(row approvalRow, now time.Time, w int) []string {
	// A full-width pane on a large terminal is too long a line to read.
	w = min(w, approvalsReadingWidth)
	who := row.RequestedBy.Name
	if who == "" {
		who = string(row.RequestedBy.Type)
	} else {
		who += " (" + string(row.RequestedBy.Type) + ")"
	}
	from := fmt.Sprintf("%s · step %d %s · requested %s ago by %s", approvalSourceLabel(row.SourceKind),
		row.Step, row.StepName, jobs.ShortDuration(approvalAge(row.Pending, now)), who)
	if row.Writer != "" {
		from += " on " + row.Writer
	}
	var lines []string
	for _, l := range approvalWrap(row.Title, w) {
		lines = append(lines, apTitle.Render(l))
	}
	for _, l := range append(approvalWrap(from, w), approvalWrap(row.Cwd, w)...) {
		lines = append(lines, apMuted.Render(l))
	}
	if !row.ready.Actionable {
		lines = append(lines, "")
		for _, l := range approvalWrap("Not actionable here: "+row.ready.Reason, w) {
			lines = append(lines, apWarn.Render(l))
		}
	}
	// A tab has no width until the terminal expands it past the pane edge.
	body := strings.ReplaceAll(strings.TrimSpace(row.Body), "\t", "    ")
	if body == "" {
		return lines
	}
	lines = append(lines, "")
	for _, raw := range strings.Split(body, "\n") {
		label := ""
		if m := approvalLabelRe.FindStringSubmatch(raw); m != nil {
			label = m[1]
		}
		for i, l := range approvalWrap(raw, w) {
			if i == 0 && label != "" && strings.HasPrefix(l, label) {
				lines = append(lines, apTitle.Render(label)+apText.Render(l[len(label):]))
				continue
			}
			lines = append(lines, apText.Render(l))
		}
	}
	return lines
}

// approvalWrap wraps at word boundaries, keeping a line's indent, and then
// splits anything still too wide (a path, a URL) with the machine browser's
// display-width wrap, so no text is ever cut off at the pane edge.
func approvalWrap(s string, w int) []string {
	if w <= 0 {
		return []string{s}
	}
	var out []string
	for _, line := range wrapApprovalText(s, w) {
		if lipgloss.Width(line) <= w {
			out = append(out, line)
			continue
		}
		out = append(out, hardWrapDisplayWidth(line, w)...)
	}
	return out
}

// approvalsPane draws a pane in the machine browser's style, exactly w wide.
// Its first row is the title, with note (a count, a scroll range) at the
// right. With h > 0 it is exactly h tall; otherwise as tall as its body.
func approvalsPane(title, note string, body []string, w, h int, focused bool) []string {
	inner := max(w-4, 1)
	head := apHeading.Render(ui.Truncate(title, inner))
	if gap := inner - lipgloss.Width(head) - lipgloss.Width(note); note != "" && gap >= 2 {
		head += strings.Repeat(" ", gap) + apMuted.Render(note)
	}
	lines := append([]string{head}, body...)
	if h > 0 {
		rows := max(h-2, 1)
		if len(lines) > rows {
			lines = lines[:rows]
		}
		for len(lines) < rows {
			lines = append(lines, "")
		}
	}
	style := machinePaneBlurred
	if focused {
		style = machinePaneFocused
	}
	return strings.Split(style.Width(max(w-2, 1)).Render(strings.Join(ui.ClampLines(lines, inner), "\n")), "\n")
}

// approvalsKeyGap separates key hints the way camp's other browsers do.
var approvalsKeyGap = apMuted.Render(" · ")

func approvalKeyHint(key, label string, style lipgloss.Style) string {
	return style.Render(key) + " " + apMuted.Render(label)
}

// keyBar puts the two decisions first, then reading, then moving around.
func (m approvalsTUIModel) keyBar(w int) string {
	row, ok := m.selected()
	if !ok {
		return " " + approvalKeyHint("r", "refresh", apTitle) + approvalsKeyGap + approvalKeyHint("q", "quit", apTitle)
	}
	decide := approvalKeyHint("a", "approve", apApprove) + approvalsKeyGap + approvalKeyHint("x", "reject", apReject)
	decideShort := decide
	if !row.ready.Actionable {
		decide, decideShort = apWarn.Render("not actionable here"), apWarn.Render("not actionable")
	}
	scroll := ""
	if visible, total := m.requestViewport(); total > visible && visible > 0 {
		scroll = approvalsKeyGap + approvalKeyHint("pgup/pgdn", "scroll", apTitle)
	}
	full := decide + approvalsKeyGap + approvalKeyHint("o", "open document", apTitle) + approvalsKeyGap +
		approvalKeyHint("↑↓", "select", apTitle) + scroll + approvalsKeyGap +
		approvalKeyHint("r", "refresh", apTitle) + approvalsKeyGap + approvalKeyHint("q", "quit", apTitle)
	short := decideShort + approvalsKeyGap + approvalKeyHint("o", "open", apTitle) + approvalsKeyGap + approvalKeyHint("q", "quit", apTitle)
	return " " + ui.CollapseHelp(w-1, full, short, decideShort)
}

// approvalsDialog frames a confirm or reason screen. When the frame does not
// fit the terminal it shows only what must be read before answering, so the
// command, its directory and the keys are never the rows that get cut.
func approvalsDialog(title string, body, essential []string, keys string, w, h int) []string {
	framed := w >= 44
	if framed && h > 0 && len(body)+7 > h {
		framed = false
	}
	if !framed {
		lines := append([]string{apHeading.Render(title)}, essential...)
		if h > 0 && len(lines) > h-1 {
			lines = lines[:max(h-1, 0)]
		}
		return append(lines, keys)
	}
	lines := approvalsPane(title, "", append(append([]string{""}, body...), ""), min(w, 100), 0, true)
	return append(lines, "", " "+keys)
}

func approvalsDialogWidth(w int) int {
	if w < 44 {
		return max(w, 1)
	}
	return min(w, 100) - 4
}

func (m approvalsTUIModel) reasonLines(w, h int) []string {
	inner := approvalsDialogWidth(w)
	var body []string
	for _, l := range approvalWrap(m.pendingRow.Title, inner) {
		body = append(body, apTitle.Render(l))
	}
	body = append(body, "")
	for _, l := range approvalWrap("Why are you rejecting it? The agent reads this to revise the work.", inner) {
		body = append(body, apText.Render(l))
	}
	body = append(body, "")
	var input []string
	for i, l := range approvalWrap(m.reason+"█", max(inner-2, 1)) {
		prefix := "  "
		if i == 0 {
			prefix = apReject.Render("> ")
		}
		input = append(input, prefix+apText.Render(l))
	}
	body = append(body, input...)
	keys := approvalKeyHint("enter", "continue", apTitle) + approvalsKeyGap + approvalKeyHint("esc", "cancel", apTitle)
	return approvalsDialog("Reject "+m.pendingRow.ID, body, input, keys, w, h)
}

func (m approvalsTUIModel) confirmLines(w, h int) []string {
	verb, runStyle := "Approve", apApprove
	note := "The command will ask you to confirm before anything is recorded."
	if m.pendingDecision == approvals.Reject {
		verb, runStyle = "Reject", apReject
		note = "The step stays blocked until the revised work is submitted again."
	}
	inner := approvalsDialogWidth(w)
	var body []string
	for _, l := range approvalWrap(m.pendingRow.Title, inner) {
		body = append(body, apTitle.Render(l))
	}
	body = append(body, "", apText.Render("camp will hand your terminal to this command:"), "")
	// The command and its directory are shown whole, wrapped and never cut.
	var command []string
	for i, l := range approvalWrap(shellQuoteArgv(m.pendingArgv), max(inner-2, 1)) {
		prefix := "  "
		if i == 0 {
			prefix = apMuted.Render("$ ")
		}
		command = append(command, prefix+apCommand.Render(l))
	}
	for _, l := range approvalWrap("in "+m.pendingRow.ready.Dir, max(inner-2, 1)) {
		command = append(command, "  "+apMuted.Render(l))
	}
	body = append(append(body, command...), "")
	for _, l := range approvalWrap(note, inner) {
		body = append(body, apText.Render(l))
	}
	keys := approvalKeyHint("y", "run it", runStyle) + approvalsKeyGap + approvalKeyHint("n", "cancel", apTitle)
	return approvalsDialog(verb+" "+m.pendingRow.ID+"?", body, command, keys, w, h)
}
