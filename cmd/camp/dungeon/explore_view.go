package dungeon

import (
	"bytes"
	"fmt"
	"hash/fnv"
	"image/png"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Obedience-Corp/camp/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Obedience-Corp/camp/internal/dungeon/explore"
)

const exploreGutter = 2

// Terminal cells are 8×16 in the pixel budget DecodeReplay scales into.
const (
	exploreCellWidth  = 8
	exploreCellHeight = 16
)

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

// exploreHelpSep separates hints. Two columns keep the feed row, with its
// pointer to the full help, inside 80 columns.
const exploreHelpSep = "  "

// The feed row is the short list and always points at the full one. The full
// help names every feed key and takes a second row when one is too narrow.
var (
	exploreFeedHelp = []helpHint{
		{"j/k move", 7}, {"enter read", 6}, {"g go", 2}, {"[ ] dungeon", 4},
		{"s status", 3}, {"space play", 1}, {"/ filter", 5}, {"? help", 98}, {"q quit", 99},
	}
	exploreFullHelp = []helpHint{
		{"j/k move", 11}, {"ctrl+u/d page", 3}, {"enter read", 10}, {"g go", 6}, {"[ ] dungeon", 8},
		{"s status", 7}, {"space play", 5}, {"/ filter", 9}, {"y copy", 2}, {"r rescan", 4},
		{"? close", 98}, {"q quit", 99},
	}
	// The reader row for a listing. A document drops "enter open": there is
	// nothing to open from one.
	exploreReaderHelp = []helpHint{
		{"j/k scroll", 5}, {"ctrl+u/d page", 1}, {"enter open", 4}, {"g go", 2}, {"y copy", 3}, {"esc back", 99},
	}
	// While the filter is typed every printable key is text, so the row names
	// only the keys that still act.
	exploreFilterHelp = []helpHint{
		{"enter apply", 98}, {"esc cancel", 99}, {"ctrl+c quit", 1},
	}
)

const exploreOpenHint = "enter open"

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
	col := cellWidth(view[lineStart:at]) + 1
	view = view[:at] + view[at+len(seq):]
	// The feed window can clip reservation rows. Paint only the rows that
	// remain in the body so the image cannot cover the status or help line.
	rows := min(m.drawRows(), m.bodyRowsFrom(row))
	if rows < 1 {
		return view
	}
	name := ""
	if m.protocol == explore.ProtocolITerm {
		view = markReplayRows(view, row, rows, col, min(width, 48), m.replayTag(min(width, 48), rows))
		name = replayRowsKey(view, row, rows)
	}
	if name != "" || rows != m.drawRows() {
		seq = m.imageSequenceNamed(width, rows, name)
	}
	// imageSequence itself saves/restores at its origin. Strip that inner
	// pair so the single saved cursor remains on the help row.
	seq = strings.TrimSuffix(strings.TrimPrefix(seq, "\x1b7"), "\x1b8")
	return view + fmt.Sprintf("\x1b7\x1b[%d;%dH", row, col) + seq + "\x1b8"
}

// iTerm has no command that deletes an image. Its cells clear only when their
// rows are repainted, and Bubble Tea skips a row that did not change. Every
// row under the image therefore carries the image's own geometry, so when the
// image goes away, moves, or changes size those rows differ from the last
// frame and are painted again. tag names the picture and its box: iTerm
// paints only the cells the picture covers once it is fitted into the box,
// so another replay there, or the same one in a shorter box, can leave cells
// of the last one unless the rows are painted again for it too.
func markReplayRows(view string, row, rows, col, cols int, tag string) string {
	lines := strings.Split(view, "\n")
	mark := tag + fmt.Sprintf("\x1b[%dG\x1b[%dX", col, cols)
	for i := max(row-1, 0); i < row-1+rows && i < len(lines); i++ {
		lines[i] += mark
	}
	return strings.Join(lines, "\n")
}

// replayRowsKey names the bytes of the rows under an iTerm replay. Bubble Tea
// repaints a changed row and erases to its end, which clears the image cells
// there, and skips the row that draws the image unless that row changed too.
// The image row therefore carries a digest of the rows it covers, so the two
// are always painted together. The digest rides in the image's own file name
// field, which sets nothing in the terminal.
func replayRowsKey(view string, row, rows int) string {
	lines := strings.Split(view, "\n")
	digest := fnv.New64a()
	for i := max(row-1, 0); i < row-1+rows && i < len(lines); i++ {
		_, _ = digest.Write([]byte(lines[i]))
		_, _ = digest.Write([]byte{'\n'})
	}
	return strconv.FormatUint(digest.Sum64(), 36)
}

// replayTag names the drawn replay in bytes a terminal shows nothing for:
// cursor hops along the row, spelled from the item, the picture's size, and
// the box it is fitted into. The column move that follows them in the row
// mark puts the cursor back. Every frame of one replay has the same tag, so
// playing it repaints nothing.
func (m exploreModel) replayTag(cols, rows int) string {
	w, h, _ := pngPixelSize(m.poster)
	sum := fnv.New32a()
	_, _ = fmt.Fprintf(sum, "%s|%dx%d|%dx%d", m.mediaFor, w, h, cols, rows)
	v := sum.Sum32()
	span := uint32(max(m.width, 1))
	var tag strings.Builder
	for range 4 {
		fmt.Fprintf(&tag, "\x1b[%dG", 1+v%span)
		v /= span
	}
	return tag.String()
}

func (m exploreModel) bodyRowsFrom(row int) int {
	last := m.height - (m.chromeLines() - 1)
	return last - row + 1
}

func (m exploreModel) view() string {
	if m.quitting {
		return ""
	}
	if m.height < 4 || m.width < 20 {
		return "Terminal is too small.\n"
	}
	lines := []string{m.renderHeader()}
	lines = append(lines, m.noticeLines()...)
	body := strings.Split(strings.TrimSuffix(m.renderBody(), "\n"), "\n")
	for len(body) < m.bodyHeight() {
		body = append(body, "")
	}
	lines = append(lines, body...)
	if row := m.statusRow(); row.text != "" {
		style := m.styles.muted
		if row.failed {
			style = m.styles.failure
		}
		lines = append(lines, style.Render(fit(row.text, m.width)))
	}
	lines = append(lines, m.helpLines()...)
	return strings.Join(lines, "\n")
}

// statusRow is what the status row shows. The answer to the last key press
// comes first and goes with the next key; what lies under it is state, which
// stays for as long as it is true: a query that does not resolve, a rescan
// that is running or failed, then a note about the focused replay. The reader
// does not draw the replay, so that note waits until the reader closes.
func (m exploreModel) statusRow() exploreNote {
	switch {
	case m.status != "":
		return exploreNote{text: m.status, failed: m.statusErr}
	case m.queryNote.text != "":
		return m.queryNote
	case m.loadNote.text != "":
		return m.loadNote
	case m.replayNote.text != "" && !m.reading:
		return m.replayNote
	}
	return exploreNote{}
}

func (m exploreModel) shownStatus() string {
	return m.statusRow().text
}

func (m exploreModel) renderHeader() string {
	if m.filtering {
		return m.styles.header.Render(keepEnd("/ ", m.filter, m.width))
	}
	text := m.headerText()
	if filter := m.query.Text; filter != "" && !m.reading && cellWidth(explore.CleanText(text)) > m.width {
		// A cut header still names the filter in force: without it nothing
		// on screen says why rows are missing. The lens gives up the room.
		mark := fit(`"`+filter+`"`, max(m.width/2, 1))
		return m.styles.header.Render(keepTail(strings.Join(m.lensParts(), " · "), " · "+mark, m.width))
	}
	return m.styles.header.Render(fit(text, m.width))
}

func (m exploreModel) lensParts() []string {
	label := m.visible.DungeonLabel
	if label == "" {
		label = "All dungeons"
	}
	return []string{label, statusTitle(m.query.Status)}
}

func (m exploreModel) headerText() string {
	if m.reading {
		return m.reader.title
	}
	parts := m.lensParts()
	if m.query.Text != "" {
		parts = append(parts, `"`+m.query.Text+`"`)
	}
	if m.query.Since != "" {
		parts = append(parts, "since "+m.query.Since)
	}
	if m.query.Until != "" {
		parts = append(parts, "until "+m.query.Until)
	}
	// The order never changes, so it goes last: a narrow header cuts it
	// before it cuts the lens or the filter that is in force.
	return strings.Join(append(parts, "newest first"), " · ")
}

func statusTitle(status string) string {
	if status == "" {
		return "Finished"
	}
	_, n := utf8.DecodeRuneInString(status)
	return strings.ToUpper(status[:n]) + status[n:]
}

// helpLines is the help at the bottom of the screen: one row, or two for the
// full help when one row cannot name every key and the terminal has the room.
func (m exploreModel) helpLines() []string {
	hints, rows := exploreFeedHelp, 1
	switch {
	case m.filtering:
		hints = exploreFilterHelp
	case m.reading:
		hints = exploreReaderHelp
		if !m.reader.listing {
			hints = slices.DeleteFunc(slices.Clone(hints), func(hint helpHint) bool { return hint.text == exploreOpenHint })
		}
	case m.help:
		hints = exploreFullHelp
		// Leave the header and one body row, and the status row when shown.
		room := m.height - 2
		if m.shownStatus() != "" {
			room--
		}
		rows = min(max(room, 1), 2)
	}
	lines := fitHelp(hints, exploreHelpSep, m.width, rows)
	for i, line := range lines {
		lines[i] = m.styles.help.Render(line)
	}
	return lines
}

// fitHelp packs hints into at most rows rows of width columns, dropping the
// lowest-ranked hint until they fit.
func fitHelp(hints []helpHint, sep string, width, rows int) []string {
	shown := slices.Clone(hints)
	for {
		var lines []string
		line := ""
		for _, hint := range shown {
			next := hint.text
			if line != "" {
				next = line + sep + hint.text
			}
			if line != "" && lipgloss.Width(next) > width {
				lines = append(lines, line)
				next = hint.text
			}
			line = next
		}
		lines = append(lines, line)
		fits := len(lines) <= rows
		for _, text := range lines {
			fits = fits && lipgloss.Width(text) <= width
		}
		if fits || len(shown) == 1 {
			for i, text := range lines {
				lines[i] = fit(text, width)
			}
			return lines[:min(len(lines), rows)]
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

func (m exploreModel) emptyFeed() string {
	if m.unfilteredFinished() {
		label := m.visible.DungeonLabel
		if label == "" || label == "All dungeons" {
			label = "this camp"
		}
		return "No finished work in " + label + "."
	}
	return "No items match the current filters."
}

func (m exploreModel) unfilteredFinished() bool {
	if m.query.Status != "" && !strings.EqualFold(m.query.Status, "finished") {
		return false
	}
	if m.query.Since != "" || m.query.Until != "" || strings.TrimSpace(m.query.Text) != "" {
		return false
	}
	dungeon := strings.TrimSpace(m.query.Dungeon)
	return dungeon == "" || strings.EqualFold(dungeon, explore.LensAll)
}

func (m exploreModel) renderBody() string {
	if m.loading && len(m.index.Items) == 0 && len(m.index.Dungeons) == 0 {
		return fit(exploreReadingStatus, m.width)
	}
	if m.reading {
		return m.renderReader()
	}
	if len(m.index.Dungeons) == 0 {
		if m.neverIndexed() {
			return fit("The dungeons could not be read. Press r to try again.", m.width)
		}
		return fit("No dungeons in this camp.", m.width)
	}
	if len(m.visible.Items) == 0 {
		return fit(m.emptyFeed(), m.width)
	}
	if m.wide() {
		return m.renderWide()
	}
	return strings.Join(m.window(m.feedLines(m.width, true)), "\n")
}

// neverIndexed reports an empty index left by a load that failed, as when
// the first read fails with no cache: the camp may well have dungeons.
func (m exploreModel) neverIndexed() bool {
	return m.loadFailed && len(m.index.Items) == 0 && len(m.index.Dungeons) == 0
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
	if m.reader.listing {
		// The status row comes and goes between key presses and changes the
		// body height, so the stored offset can leave the selection outside.
		reader := m.reader
		reader.follow(height)
		start = reader.offset
	}
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
	for i, item := range m.visible.Items {
		if item.DoneDate != lastDay {
			lastDay = item.DoneDate
			lines = append(lines, m.styles.day.Render(fit(prettyDay(item.DoneDate), width)))
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
		if includeStage && i == m.cursor {
			lines = append(lines, m.stageBlock(m.width)...)
		}
	}
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
	// The focused row's block must fit below it: its summary and, in the
	// narrow layout, the replay and the card.
	if low := focus + m.focusBlockRows() - height; start < low {
		start = min(low, focus)
	}
	if start < 0 {
		start = 0
	}
	if start+height > len(lines) {
		start = len(lines) - height
	}
	return lines[start : start+height]
}

// focusBlockRows is the most rows the focused item takes in the feed column:
// its row and summary, then in the narrow layout the card, under the replay
// reservation when a replay can be drawn. It does not depend on whether the
// replay has arrived, so the feed does not jump when it does.
func (m exploreModel) focusBlockRows() int {
	if m.wide() {
		return 2
	}
	if m.stageRows() == 0 || m.protocol == explore.ProtocolOff {
		item, _ := m.focused()
		return 2 + len(m.cardLines(item, m.width))
	}
	return 2 + 1 + m.stageRows() + 5
}

// stageBlock is the focused item's replay reservation and its card. The card
// holds the only copy of the status, the date, and the path, so it is drawn
// whenever a row is focused; only the reservation needs room for a replay.
func (m exploreModel) stageBlock(width int) []string {
	item, ok := m.focused()
	if !ok {
		return nil
	}
	rows := m.drawRows()
	var lines []string
	if rows > 0 && m.poster != nil && m.mediaFor == item.Path && m.protocol != explore.ProtocolOff {
		if !m.wide() {
			lines = append(lines, m.imageSequence(width))
		}
		for range rows {
			lines = append(lines, "")
		}
	}
	return append(lines, m.cardLines(item, width)...)
}

func (m exploreModel) cardLines(item explore.Item, width int) []string {
	lines := []string{
		m.styles.title.Render(fit(item.Title, width)),
		m.styles.day.Render(keepTail(item.ID, "  "+item.DungeonLabel+"  "+item.Status, width)),
	}
	if item.Summary != "" {
		lines = append(lines, m.styles.muted.Render(fit(item.Summary, width)))
	}
	lines = append(lines, m.styles.muted.Render(fit(datePhrase(item), width)))
	lines = append(lines, m.styles.muted.Render(fit(item.Path, width)))
	if item.Kind == explore.KindFestival && item.Replay == "" {
		lines = append(lines, m.styles.muted.Render(fit("No replay yet.", width)))
	}
	return lines
}

// drawRows is how many rows the focused replay is given. The wide layout
// stacks the replay on the card in one column, so on a short body the replay
// gives up rows until the card fits under it. The decoded picture keeps the
// size stageRows asked for; the terminal scales it into the smaller box.
func (m exploreModel) drawRows() int {
	rows := m.stageRows()
	if rows == 0 || !m.wide() {
		return rows
	}
	item, ok := m.focused()
	if !ok {
		return rows
	}
	room := m.bodyHeight() - len(m.cardLines(item, m.width))
	return max(min(rows, room), 0)
}

func (m exploreModel) imageSequence(width int) string {
	return m.imageSequenceNamed(width, m.drawRows(), "")
}

// imageSequenceNamed is the image sequence for a box of rows rows, with name
// in the iTerm file name field. Kitty has no use for a name.
func (m exploreModel) imageSequenceNamed(width, rows int, name string) string {
	item, ok := m.focused()
	if !ok || m.poster == nil || m.mediaFor != item.Path || m.protocol == explore.ProtocolOff {
		return ""
	}
	if rows < 1 {
		return ""
	}
	cols := min(width, 48)
	// DECSC and DECRC put the cursor back where the image started, so the
	// renderer's next row lands where it expects on every terminal.
	switch m.protocol {
	case explore.ProtocolKitty:
		// Kitty's c and r stretch the PNG. Place it in the cell box of the
		// decoded image so a square or portrait replay keeps its aspect.
		cols, rows = kittyPlacement(m.poster, cols, rows)
		return "\x1b7" + explore.KittyDelete(exploreImageID) + explore.KittyPNG(exploreImageID, m.poster, cols, rows) + "\x1b8"
	case explore.ProtocolITerm:
		return "\x1b7" + explore.ITermPNG(name, m.poster, cols, rows) + "\x1b8"
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
	return cols * exploreCellWidth, m.stageRows() * exploreCellHeight
}

func (m exploreModel) bodyHeight() int {
	h := m.height - m.chromeLines() - len(m.noticeLines())
	if h < 1 {
		return 1
	}
	return h
}

func (m exploreModel) chromeLines() int {
	n := 1 + len(m.helpLines())
	if m.shownStatus() != "" {
		n++
	}
	return n
}

func (m exploreModel) noticeLines() []string {
	if m.reading || m.filtering {
		return nil
	}
	var lines []string
	if text := scanWarning(m.index.Warnings); text != "" {
		lines = append(lines, m.styles.failure.Render(fit(text, m.width)))
	}
	if item, ok := m.focused(); ok {
		if text := strings.TrimSpace(item.Warning); text != "" {
			lines = append(lines, m.styles.failure.Render(fit(text, m.width)))
		}
	}
	limit := m.height - m.chromeLines() - 1
	if limit < 0 {
		return nil
	}
	if len(lines) > limit {
		return lines[:limit]
	}
	return lines
}

// scanWarning keeps the notice to one row however many paths failed to scan.
func scanWarning(warnings []string) string {
	first, count := "", 0
	for _, warning := range warnings {
		if text := strings.TrimSpace(warning); text != "" {
			if count == 0 {
				first = text
			}
			count++
		}
	}
	if count > 1 {
		return fmt.Sprintf("%d scan warnings, first: %s", count, first)
	}
	return first
}

func kittyPlacement(pngBytes []byte, maxCols, maxRows int) (int, int) {
	if maxCols < 1 {
		maxCols = 1
	}
	if maxRows < 1 {
		maxRows = 1
	}
	w, h, ok := pngPixelSize(pngBytes)
	if !ok {
		return maxCols, maxRows
	}
	maxW := maxCols * exploreCellWidth
	maxH := maxRows * exploreCellHeight
	if w > maxW || h > maxH {
		dw, dh := w, h
		if dw > maxW {
			dh = dh * maxW / dw
			dw = maxW
		}
		if dh > maxH {
			dw = dw * maxH / dh
			dh = maxH
		}
		if dw < 1 {
			dw = 1
		}
		if dh < 1 {
			dh = 1
		}
		w, h = dw, dh
	}
	cols := (w + exploreCellWidth - 1) / exploreCellWidth
	rows := (h + exploreCellHeight - 1) / exploreCellHeight
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	if cols > maxCols {
		cols = maxCols
	}
	if rows > maxRows {
		rows = maxRows
	}
	return cols, rows
}

func pngPixelSize(data []byte) (int, int, bool) {
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 {
		return 0, 0, false
	}
	return cfg.Width, cfg.Height, true
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
	return truncateCells(s, width, "…")
}

// cellWidth is the most columns s can take on a terminal. Terminals disagree
// about emoji sequences: one that clusters draws a family, a flag, or a
// skin-toned hand in two cells, and one that does not draws every code point
// of it. Each cluster counts as the wider of the two, so a row measured here
// stays inside the margin on both. Escape sequences count for nothing.
func cellWidth(s string) int {
	if width, ok := asciiCells(s); ok {
		return width
	}
	s = ansi.Strip(s)
	if width, ok := asciiCells(s); ok {
		return width
	}
	width := 0
	for len(s) > 0 {
		cluster, clustered := ansi.FirstGraphemeCluster(s, ansi.GraphemeWidth)
		if cluster == "" {
			break
		}
		width += clusterCells(cluster, clustered)
		s = s[len(cluster):]
	}
	return width
}

// asciiCells is the width of text every terminal draws one cell a byte:
// printable ASCII, which is nearly every row of a feed.
func asciiCells(s string) (int, bool) {
	for i := range len(s) {
		if s[i] < 0x20 || s[i] >= 0x7f {
			return 0, false
		}
	}
	return len(s), true
}

func clusterCells(cluster string, clustered int) int {
	apart := 0
	for _, r := range cluster {
		apart += ansi.StringWidth(string(r))
	}
	return max(clustered, apart)
}

// truncateCells cuts unstyled text to width cells and ends a cut with tail.
func truncateCells(s string, width int, tail string) string {
	if cellWidth(s) <= width {
		return s
	}
	room := width - cellWidth(tail)
	if room < 0 {
		return ""
	}
	if _, ok := asciiCells(s); ok {
		return s[:room] + tail
	}
	var b strings.Builder
	for used := 0; len(s) > 0; {
		cluster, clustered := ansi.FirstGraphemeCluster(s, ansi.GraphemeWidth)
		cells := clusterCells(cluster, clustered)
		if cluster == "" || used+cells > room {
			break
		}
		b.WriteString(cluster)
		used += cells
		s = s[len(cluster):]
	}
	return b.String() + tail
}

func keepTail(head, tail string, width int) string {
	tail = explore.CleanText(tail)
	if head == "" {
		return fit(strings.TrimLeft(tail, " "), width)
	}
	room := width - cellWidth(tail)
	if room < 1 {
		return fit(head+tail, width)
	}
	return fit(head, room) + tail
}

// keepEnd fits prefix and text into width, cutting text from the left so the
// end being typed stays visible.
func keepEnd(prefix, text string, width int) string {
	text = explore.CleanText(text)
	room := width - cellWidth(prefix)
	if room < 1 {
		return fit(prefix, width)
	}
	if cellWidth(text) <= room {
		return prefix + text
	}
	// One column goes to the ellipsis that marks the cut.
	var clusters []string
	for rest := text; len(rest) > 0; {
		cluster, _ := ansi.FirstGraphemeCluster(rest, ansi.GraphemeWidth)
		if cluster == "" {
			break
		}
		clusters = append(clusters, cluster)
		rest = rest[len(cluster):]
	}
	from, used := len(clusters), 0
	for from > 0 {
		cells := cellWidth(clusters[from-1])
		if used+cells > room-1 {
			break
		}
		used += cells
		from--
	}
	return prefix + "…" + strings.Join(clusters[from:], "")
}

func padWidth(s string, width int) string {
	gap := width - cellWidth(s)
	if gap <= 0 {
		return ansi.Truncate(s, width, "")
	}
	return s + strings.Repeat(" ", gap)
}
