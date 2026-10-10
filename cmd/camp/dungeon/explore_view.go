package dungeon

import (
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Obedience-Corp/camp/internal/dungeon/explore"
)

func (m exploreModel) View() string {
	out := m.view()
	if m.protocol == explore.ProtocolKitty && !explore.HasKittyImage(out) {
		out = explore.KittyDelete(exploreImageID) + out
	}
	return out
}

func (m exploreModel) view() string {
	if m.quitting {
		return ""
	}
	if m.height < 4 || m.width < 20 {
		return "Terminal is too small.\n"
	}
	var b strings.Builder
	b.WriteString(m.renderHeader())
	b.WriteByte('\n')
	body := m.renderBody()
	b.WriteString(body)
	if !strings.HasSuffix(body, "\n") {
		b.WriteByte('\n')
	}
	if m.status != "" {
		b.WriteString(termSafe(m.status))
		b.WriteByte('\n')
	}
	b.WriteString(m.renderHelp())
	return b.String()
}

func (m exploreModel) renderHeader() string {
	return termSafe(m.headerText())
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
	return strings.ToUpper(status[:1]) + status[1:]
}

func (m exploreModel) renderHelp() string {
	if m.help {
		return "j/k move  enter read  g go  [ ] dungeon  s status  space play  / filter  y copy  r rescan  q quit"
	}
	if m.reading {
		return "j/k scroll   enter open   g go   y copy   esc back"
	}
	return "j/k move   enter read   g go   [ ] dungeon   s status   space play   / filter   q quit"
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
	return strings.Join(m.window(m.feedLines(true)), "\n")
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
			lines[i] = prefix + termSafe(entry.name)
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
		return strings.Join(m.window(m.feedLines(true)), "\n")
	}
	left := m.window(m.feedLines(false))
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

func (m exploreModel) feedLines(includeStage bool) []string {
	var lines []string
	var lastDay string
	focusAt := 0
	for i, item := range m.visible.Items {
		if item.DoneDate != lastDay {
			lastDay = item.DoneDate
			lines = append(lines, prettyDay(item.DoneDate))
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
		title += "  " + item.DungeonLabel
		lines = append(lines, mark+fit(title, m.width-2))
		if item.Summary != "" {
			lines = append(lines, "  "+fit(item.Summary, m.width-2))
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
	lines = append(lines, fit(item.Title, width))
	meta := item.ID
	if meta != "" {
		meta += "  "
	}
	meta += item.DungeonLabel + "  " + item.Status
	lines = append(lines, fit(meta, width))
	if item.Summary != "" {
		lines = append(lines, fit(item.Summary, width))
	}
	lines = append(lines, datePhrase(item))
	lines = append(lines, fit(item.Path, width))
	if item.Kind == explore.KindFestival && item.Replay == "" {
		lines = append(lines, "No replay yet.")
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
		return "\x1b7" + explore.KittyDelete(exploreImageID) + explore.KittyPNG(exploreImageID, m.poster) + "\x1b8"
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
	cols := 48
	if !m.wide() && m.width < cols {
		cols = m.width
	}
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
	if m.wide() {
		return m.width/2 - 2
	}
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

func termSafe(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			return ' '
		case unicode.IsControl(r):
			return -1
		default:
			return r
		}
	}, ansi.Strip(s))
}

func termSafeText(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, ansi.Strip(s))
}

func fit(s string, width int) string {
	s = termSafe(s)
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes)) > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes)
}

func padWidth(s string, width int) string {
	gap := width - lipgloss.Width(s)
	if gap <= 0 {
		return fit(s, width)
	}
	return s + strings.Repeat(" ", gap)
}
