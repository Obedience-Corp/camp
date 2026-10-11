package dungeon

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Obedience-Corp/camp/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Obedience-Corp/camp/internal/dungeon/explore"
)

const exploreGutter = 2

// Styles are resolved once per terminal session from Camp's shared palette.
type exploreStyles struct {
	header, selected, title, muted, day, help, failure lipgloss.Style
}

func newExploreStyles(plain bool) exploreStyles {
	if plain {
		return exploreStyles{}
	}
	p := theme.TUI()
	return exploreStyles{
		header:   lipgloss.NewStyle().Foreground(p.Accent).Bold(true),
		selected: lipgloss.NewStyle().Foreground(p.AccentAlt).Background(p.BgSelected).Bold(true),
		title:    lipgloss.NewStyle().Foreground(p.TextPrimary).Bold(true),
		muted:    lipgloss.NewStyle().Foreground(p.TextMuted),
		day:      lipgloss.NewStyle().Foreground(p.Accent),
		help:     lipgloss.NewStyle().Foreground(p.TextSecondary),
		failure:  lipgloss.NewStyle().Foreground(p.Error),
	}
}

type helpHint struct {
	text string
	rank int
}

var (
	exploreFeedHelp = []helpHint{
		{"j/k move", 7}, {"enter read", 6}, {"g go", 2}, {"[ ] dungeon", 4},
		{"s status", 3}, {"space play", 1}, {"/ filter", 5}, {"q quit", 99},
	}
	exploreFullHelp = []helpHint{
		{"j/k move", 9}, {"enter read", 8}, {"g go", 4}, {"[ ] dungeon", 6}, {"s status", 5},
		{"space play", 3}, {"/ filter", 7}, {"y copy", 1}, {"r rescan", 2}, {"q quit", 99},
	}
	exploreReaderHelp = []helpHint{
		{"j/k scroll", 4}, {"enter open", 3}, {"g go", 1}, {"y copy", 2}, {"esc back", 99},
	}
)

func (m exploreModel) View() string {
	out := m.placeReplay(m.view())
	if m.protocol == explore.ProtocolKitty && !explore.HasKittyImage(out) {
		out = explore.KittyDelete(exploreImageID) + out
	}
	return out
}

// Bubble Tea erases each text row after writing it. Cell-based images must
// therefore be painted after the body, from the final (help) row. Restoring
// the cursor there keeps its trailing erase away from the image reservation.
func (m exploreModel) placeReplay(view string) string {
	width := m.width
	if m.wide() {
		width -= m.width / 2
	}
	seq := m.imageSequence(width)
	if seq == "" {
		return view
	}
	at := strings.Index(view, seq)
	if at < 0 {
		return view
	}
	row := strings.Count(view[:at], "\n") + 1
	lineStart := strings.LastIndex(view[:at], "\n") + 1
	col := lipgloss.Width(view[lineStart:at]) + 1
	view = view[:at] + view[at+len(seq):]
	// imageSequence itself saves/restores at its origin. Strip that inner
	// pair so the single saved cursor remains on the help row.
	seq = strings.TrimSuffix(strings.TrimPrefix(seq, "\x1b7"), "\x1b8")
	return view + fmt.Sprintf("\x1b7\x1b[%d;%dH", row, col) + seq + "\x1b8"
}

func (m exploreModel) view() string {
	if m.quitting {
		return ""
	}
	if m.height < 4 || m.width < 20 {
		return "Terminal is too small.\n"
	}
	lines := []string{m.renderHeader()}
	body := strings.Split(strings.TrimSuffix(m.renderBody(), "\n"), "\n")
	for len(body) < m.bodyHeight() {
		body = append(body, "")
	}
	lines = append(lines, body...)
	if m.status != "" {
		style := m.styles.muted
		if m.statusErr {
			style = m.styles.failure
		}
		lines = append(lines, style.Render(fit(m.status, m.width)))
	}
	lines = append(lines, m.renderHelp())
	return strings.Join(lines, "\n")
}

func (m exploreModel) renderHeader() string {
	return m.styles.header.Render(fit(m.headerText(), m.width))
}

func (m exploreModel) headerText() string {
	if m.filtering {
		return "/ " + m.filter
	}
	if m.reading {
		return m.reader.title
	}
	parts := []string{m.visible.DungeonLabel, statusTitle(m.query.Status), "newest first"}
	if m.visible.DungeonLabel == "" {
		parts[0] = "All dungeons"
	}
	if m.query.Since != "" {
		parts = append(parts, "since "+m.query.Since)
	}
	if m.query.Until != "" {
		parts = append(parts, "until "+m.query.Until)
	}
	if m.query.Text != "" {
		parts = append(parts, `"`+m.query.Text+`"`)
	}
	return strings.Join(parts, " · ")
}

func statusTitle(status string) string {
	if status == "" {
		return "Finished"
	}
	_, n := utf8.DecodeRuneInString(status)
	return strings.ToUpper(status[:n]) + status[n:]
}

func (m exploreModel) renderHelp() string {
	if m.help {
		return m.styles.help.Render(fitHelp(exploreFullHelp, "  ", m.width))
	}
	if m.reading {
		return m.styles.help.Render(fitHelp(exploreReaderHelp, "   ", m.width))
	}
	return m.styles.help.Render(fitHelp(exploreFeedHelp, "   ", m.width))
}

func fitHelp(hints []helpHint, sep string, width int) string {
	shown := slices.Clone(hints)
	for {
		texts := make([]string, len(shown))
		for i, hint := range shown {
			texts[i] = hint.text
		}
		line := strings.Join(texts, sep)
		if lipgloss.Width(line) <= width || len(shown) == 1 {
			return fit(line, width)
		}
		drop := 0
		for i, hint := range shown {
			if hint.rank < shown[drop].rank {
				drop = i
			}
		}
		shown = slices.Delete(shown, drop, drop+1)
	}
}

func (m exploreModel) renderBody() string {
	if m.loading && len(m.index.Items) == 0 && len(m.index.Dungeons) == 0 {
		return exploreReadingStatus
	}
	if m.reading {
		return m.renderReader()
	}
	if len(m.index.Dungeons) == 0 {
		return "No dungeons in this camp."
	}
	if len(m.visible.Items) == 0 {
		label := m.visible.DungeonLabel
		if label == "" || label == "All dungeons" {
			label = "this camp"
		}
		return "No finished work in " + label + "."
	}
	if m.wide() {
		return m.renderWide()
	}
	return strings.Join(m.window(m.feedLines(m.width, true)), "\n")
}

func (m exploreModel) renderReader() string {
	lines := m.reader.lines
	if m.reader.listing {
		lines = make([]string, len(m.reader.entries))
		for i, entry := range m.reader.entries {
			prefix := "  "
			if i == m.reader.entry {
				prefix = "> "
			}
			lines[i] = prefix + fit(entry.name, m.contentWidth()-2)
			if i == m.reader.entry {
				lines[i] = m.styles.selected.Render(padWidth(lines[i], m.contentWidth()))
			}
		}
		if len(lines) == 0 {
			lines = []string{"No markdown files."}
		}
	}
	height := m.bodyHeight()
	start := m.reader.offset
	if start > len(lines) {
		start = len(lines)
	}
	end := start + height
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[start:end], "\n")
}

func (m exploreModel) renderWide() string {
	leftW := m.width / 2
	if leftW < 30 {
		leftW = 30
	}
	rightW := m.width - leftW
	if rightW < 20 {
		return strings.Join(m.window(m.feedLines(m.width, true)), "\n")
	}
	left := m.window(m.feedLines(leftW-exploreGutter, false))
	right := m.stageBlock(rightW)
	height := m.bodyHeight()
	var b strings.Builder
	seq := m.imageSequence(rightW)
	for i := range height {
		line := ""
		if i < len(left) {
			line = left[i]
		}
		line = padWidth(line, leftW)
		if i == 0 && seq != "" {
			b.WriteString(line)
			b.WriteString(seq)
		} else {
			b.WriteString(line)
		}
		if i < len(right) {
			b.WriteString(right[i])
		}
		if i+1 < height {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func (m exploreModel) feedLines(width int, includeStage bool) []string {
	var lines []string
	var lastDay string
	focusAt := 0
	for i, item := range m.visible.Items {
		if item.DoneDate != lastDay {
			lastDay = item.DoneDate
			lines = append(lines, m.styles.day.Render(fit(prettyDay(item.DoneDate), width)))
		}
		if i == m.cursor {
			focusAt = len(lines)
		}
		mark := "  "
		if i == m.cursor {
			mark = "> "
		}
		title := item.Title
		if item.ID != "" {
			title += "  " + item.ID
		}
		row := mark + keepTail(title, "  "+item.DungeonLabel, width-2)
		if i == m.cursor {
			row = m.styles.selected.Render(padWidth(row, width))
		}
		lines = append(lines, row)
		if item.Summary != "" {
			lines = append(lines, m.styles.muted.Render("  "+fit(item.Summary, width-2)))
		}
		if includeStage && i == m.cursor && m.stageRows() > 0 {
			lines = append(lines, m.stageBlock(m.width)...)
		}
	}
	_ = focusAt
	return lines
}

func (m exploreModel) window(lines []string) []string {
	height := m.bodyHeight()
	if len(lines) <= height {
		return lines
	}
	// Keep the cursor's first line in view. Recount because feedLines does too.
	focus := 0
	seen := 0
	lastDay := ""
	for i, item := range m.visible.Items {
		if item.DoneDate != lastDay {
			lastDay = item.DoneDate
			if i == m.cursor {
				focus = seen
			}
			seen++
		}
		if i == m.cursor {
			focus = seen
			break
		}
		seen += 2
		if item.Summary == "" {
			seen--
		}
	}
	start := focus - height/2
	if start < 0 {
		start = 0
	}
	if start+height > len(lines) {
		start = len(lines) - height
	}
	return lines[start : start+height]
}

func (m exploreModel) stageBlock(width int) []string {
	item, ok := m.focused()
	if !ok {
		return nil
	}
	rows := m.stageRows()
	var lines []string
	if rows > 0 && m.poster != nil && m.mediaFor == item.Path && m.protocol != explore.ProtocolOff {
		if !m.wide() {
			lines = append(lines, m.imageSequence(width))
		}
		for range rows {
			lines = append(lines, "")
		}
	}
	lines = append(lines, m.styles.title.Render(fit(item.Title, width)))
	lines = append(lines, m.styles.day.Render(keepTail(item.ID, "  "+item.DungeonLabel+"  "+item.Status, width)))
	if item.Summary != "" {
		lines = append(lines, m.styles.muted.Render(fit(item.Summary, width)))
	}
	lines = append(lines, m.styles.muted.Render(fit(datePhrase(item), width)))
	lines = append(lines, m.styles.muted.Render(fit(item.Path, width)))
	if item.Kind == explore.KindFestival && item.Replay == "" {
		lines = append(lines, m.styles.muted.Render("No replay yet."))
	}
	return lines
}

func (m exploreModel) imageSequence(width int) string {
	item, ok := m.focused()
	if !ok || m.poster == nil || m.mediaFor != item.Path || m.protocol == explore.ProtocolOff {
		return ""
	}
	rows := m.stageRows()
	if rows < 1 {
		return ""
	}
	// DECSC and DECRC put the cursor back where the image started, so the
	// renderer's next row lands where it expects on every terminal.
	switch m.protocol {
	case explore.ProtocolKitty:
		return "\x1b7" + explore.KittyDelete(exploreImageID) + explore.KittyPNG(exploreImageID, m.poster, min(width, 48), rows) + "\x1b8"
	case explore.ProtocolITerm:
		return "\x1b7" + explore.ITermPNG(m.poster, min(width, 48), rows) + "\x1b8"
	default:
		return ""
	}
}

func (m exploreModel) wide() bool {
	return m.width >= 88 && m.height >= 16
}

func (m exploreModel) stageRows() int {
	if m.width < 60 || m.height < 16 {
		return 0
	}
	if m.wide() {
		rows := 12
		if m.height < 30 {
			rows = 8
		}
		return rows
	}
	if m.height < 22 {
		return 0
	}
	return 8
}

func (m exploreModel) stagePixels() (int, int) {
	cols := m.width
	if m.wide() {
		cols -= m.width / 2
	}
	cols = min(48, cols)
	return cols * 8, m.stageRows() * 16
}

func (m exploreModel) bodyHeight() int {
	h := m.height - 2
	if m.status != "" {
		h--
	}
	if h < 1 {
		return 1
	}
	return h
}

func (m exploreModel) contentWidth() int {
	return m.width - 2
}

func datePhrase(item explore.Item) string {
	if item.DateSource == explore.DateFileTime {
		return "file time " + prettyDay(item.DoneDate)
	}
	return "moved " + prettyDay(item.DoneDate)
}

func prettyDay(day string) string {
	stamp, err := time.Parse("2006-01-02", day)
	if err != nil {
		return day
	}
	if stamp.Year() == time.Now().Year() {
		return stamp.Format("Jan 2")
	}
	return stamp.Format("Jan 2, 2006")
}

func fit(s string, width int) string {
	s = explore.CleanText(s)
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(s, width, "…")
}

func keepTail(head, tail string, width int) string {
	tail = explore.CleanText(tail)
	if head == "" {
		return fit(strings.TrimLeft(tail, " "), width)
	}
	room := width - lipgloss.Width(tail)
	if room < 1 {
		return fit(head+tail, width)
	}
	return fit(head, room) + tail
}

func padWidth(s string, width int) string {
	gap := width - lipgloss.Width(s)
	if gap <= 0 {
		return ansi.Truncate(s, width, "")
	}
	return s + strings.Repeat(" ", gap)
}
