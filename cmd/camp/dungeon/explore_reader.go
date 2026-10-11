package dungeon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/Obedience-Corp/camp/internal/dungeon/explore"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
)

type readerEntry struct {
	name string
	path string
}

type exploreReader struct {
	item  explore.Item
	title string
	path  string
	body  readerBody
	lines []string
	// wrapped is the width lines was rendered at.
	wrapped int
	offset  int
	listing bool
	entries []readerEntry
	entry   int
}

type readerBody struct {
	lead     []string
	text     string
	markdown bool
}

func (b readerBody) render(width int, plain bool) []string {
	text := expandTabs(explore.CleanDocument(b.text))
	var lines []string
	if b.markdown {
		lines = renderExploreMarkdown(explore.MarkdownBody(text), width, plain)
	} else {
		lines = wrapPlain(text, width)
	}
	var lead []string
	for _, line := range b.lead {
		lead = append(lead, wrapPlain(line, width)...)
	}
	return append(lead, lines...)
}

func (r *exploreReader) follow(height int) {
	if r.entry < r.offset {
		r.offset = r.entry
	}
	if height > 0 && r.entry >= r.offset+height {
		r.offset = r.entry - height + 1
	}
}

func (m *exploreModel) reflowReader() {
	if !m.reading {
		return
	}
	if m.reader.listing {
		m.reader.follow(m.bodyHeight())
		return
	}
	// Rendering a long document is slow, and a drag sends a size for every
	// step of it. A change of height alone keeps the rendered lines.
	if width := m.contentWidth(); width != m.reader.wrapped {
		m.reader.lines = m.reader.body.render(width, m.plain)
		m.reader.wrapped = width
	}
	m.reader.offset = maxReaderOffset(m.reader.offset, len(m.reader.lines), m.bodyHeight())
}

func (m exploreModel) openReader() (tea.Model, tea.Cmd) {
	item, ok := m.focused()
	if !ok {
		return m, nil
	}
	abs := joinAbs(m.root, item.Path)
	reader := exploreReader{title: item.Title, path: abs, item: item}
	switch {
	case item.Kind == "workitem" && item.IsDir:
		entries, err := markdownEntries(abs)
		if err != nil {
			m.setStatus(exploreReadStatus, err.Error(), true)
			return m, nil
		}
		reader.listing = true
		reader.entries = entries
	case item.Kind == "festival" && item.IsDir:
		goal := filepath.Join(abs, "FESTIVAL_GOAL.md")
		if info, err := os.Lstat(goal); err == nil && info.Mode().IsRegular() {
			text, cut, err := readExploreText(m.readerContext(), goal)
			if err != nil {
				m.setStatus(exploreReadStatus, "The file could not be parsed.", true)
				return m, nil
			}
			reader.path = goal
			reader.body = readerBody{text: text, markdown: true, lead: cutNotice(cut)}
		} else if item.Summary != "" {
			reader.body = readerBody{text: item.Summary}
		} else {
			reader.body = readerBody{text: "No festival goal file."}
		}
		if item.Replay == "" {
			reader.body.lead = append([]string{"No replay yet.", ""}, reader.body.lead...)
		}
	case !item.IsDir && strings.HasSuffix(strings.ToLower(item.Path), ".md"):
		text, cut, err := readExploreText(m.readerContext(), abs)
		if err != nil {
			m.setStatus(exploreReadStatus, "The file could not be parsed.", true)
			return m, nil
		}
		reader.body = readerBody{text: text, markdown: true, lead: cutNotice(cut)}
	default:
		info, err := os.Lstat(abs)
		if err != nil {
			reader.body = readerBody{text: abs}
		} else {
			reader.body = readerBody{text: abs + "\n" + info.ModTime().Format("2006-01-02 15:04") + "\nsize " + itoa64(info.Size())}
		}
	}
	if !reader.listing {
		reader.wrapped = m.contentWidth()
		reader.lines = reader.body.render(reader.wrapped, m.plain)
	}
	m.reading = true
	m.reader = reader
	m.help = false
	m.clearActionStatus()
	return m, nil
}

func (m exploreModel) onReaderKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "ctrl+c":
		return m.quit()
	case "esc", "q":
		m.reading = false
		m.clearActionStatus()
		return m, nil
	case "up", "k":
		return m.scrollReader(-1)
	case "down", "j":
		return m.scrollReader(1)
	case "ctrl+u":
		return m.scrollReader(-m.page())
	case "ctrl+d":
		return m.scrollReader(m.page())
	case "enter":
		if !m.reader.listing || len(m.reader.entries) == 0 {
			return m, nil
		}
		picked := m.reader.entries[m.reader.entry]
		text, cut, err := readExploreText(m.readerContext(), picked.path)
		if err != nil {
			m.setStatus(exploreReadStatus, "The file could not be parsed.", true)
			return m, nil
		}
		m.clearActionStatus()
		m.reader.listing = false
		m.reader.path = picked.path
		m.reader.title = picked.name
		m.reader.body = readerBody{text: text, markdown: true, lead: cutNotice(cut)}
		m.reader.wrapped = m.contentWidth()
		m.reader.lines = m.reader.body.render(m.reader.wrapped, m.plain)
		m.reader.offset = 0
		return m, nil
	case "g":
		return m.hopTo(m.reader.item)
	case "y":
		return m.copyPath(m.reader.path)
	}
	return m, nil
}

// scrollReader moves the listing selection or the document by delta rows. A
// status reports one key press, so the next movement key clears it, as in
// the feed.
func (m exploreModel) scrollReader(delta int) (tea.Model, tea.Cmd) {
	m.clearActionStatus()
	if m.reader.listing {
		if len(m.reader.entries) == 0 {
			return m, nil
		}
		m.reader.entry = min(max(m.reader.entry+delta, 0), len(m.reader.entries)-1)
		m.reader.follow(m.bodyHeight())
		return m, nil
	}
	m.reader.offset = maxReaderOffset(m.reader.offset+delta, len(m.reader.lines), m.bodyHeight())
	return m, nil
}

func markdownEntries(dir string) ([]readerEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []readerEntry
	var rest []readerEntry
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
			continue
		}
		item := readerEntry{name: entry.Name(), path: filepath.Join(dir, entry.Name())}
		if strings.EqualFold(entry.Name(), "README.md") {
			out = append(out, item)
			continue
		}
		rest = append(rest, item)
	}
	return append(out, rest...), nil
}

func (m exploreModel) readerContext() context.Context {
	if m.ctx != nil {
		return m.ctx
	}
	return context.Background()
}

// readerLimit is the most of one document the reader loads.
const readerLimit = 256 << 10

// readExploreText reads a document for the reader. cut reports a file longer
// than readerLimit, whose remainder is not shown.
func readExploreText(ctx context.Context, path string) (text string, cut bool, err error) {
	data, err := explore.ReadRegular(ctx, path, readerLimit+1)
	if err != nil {
		return "", false, err
	}
	if len(data) <= readerLimit {
		return string(data), false, nil
	}
	data = data[:readerLimit]
	// The limit can land inside a multi-byte character; drop the fragment.
	for range utf8.UTFMax - 1 {
		if r, size := utf8.DecodeLastRune(data); r != utf8.RuneError || size != 1 {
			break
		}
		data = data[:len(data)-1]
	}
	return string(data), true, nil
}

func cutNotice(cut bool) []string {
	if !cut {
		return nil
	}
	return []string{"Showing the first 256 KiB of this file.", ""}
}

const readerTabStop = 4

// expandTabs replaces each tab with spaces up to the next tab stop. Glamour
// and the wrapper measure a tab as no columns while a terminal advances to
// its next stop, so a raw tab makes a row wider than it was measured.
func expandTabs(text string) string {
	if !strings.Contains(text, "\t") {
		return text
	}
	var b strings.Builder
	b.Grow(len(text) + len(text)/8)
	col := 0
	for _, r := range text {
		switch r {
		case '\n':
			col = 0
			b.WriteRune(r)
		case '\t':
			pad := readerTabStop - col%readerTabStop
			b.WriteString(strings.Repeat(" ", pad))
			col += pad
		default:
			b.WriteRune(r)
			col += ansi.StringWidth(string(r))
		}
	}
	return b.String()
}

func renderExploreMarkdown(text string, width int, plain bool) []string {
	width = max(width, 1)
	// The feed's palette follows the terminal background; so does the document.
	// Glamour's dark style is light grey text, which a light terminal hides.
	opts := []glamour.TermRendererOption{glamour.WithStylePath(theme.GlamourStyle(theme.ThemeAdaptive)), glamour.WithWordWrap(width)}
	if plain {
		opts = []glamour.TermRendererOption{
			glamour.WithStandardStyle(styles.NoTTYStyle),
			glamour.WithColorProfile(termenv.Ascii),
			glamour.WithWordWrap(width),
		}
	}
	rendered, err := glamour.NewTermRenderer(opts...)
	if err != nil {
		return wrapPlain(text, width)
	}
	out, err := rendered.Render(text)
	if err != nil {
		return wrapPlain(text, width)
	}
	// Markdown entity decoding happens inside Glamour. Filter the result, too:
	// only styling may reach the terminal, never document-supplied commands.
	out = safeReaderOutput(out, plain)
	return closeStyles(wrapPlain(strings.TrimRight(out, "\n"), width))
}

// wrapPlain breaks text into rows of at most width cells, measured as
// cellWidth measures them, and keeps escape sequences whole. Glamour pads its
// rows with spaces by its own measure; where a break leaves only that padding
// behind, the padding makes no row.
func wrapPlain(text string, width int) []string {
	width = max(width, 1)
	var rows []string
	for _, line := range strings.Split(text, "\n") {
		pieces := wrapLine(line, width)
		for len(pieces) > 1 && strings.TrimSpace(ansi.Strip(pieces[len(pieces)-1])) == "" {
			pieces = pieces[:len(pieces)-1]
		}
		rows = append(rows, pieces...)
	}
	return rows
}

func wrapLine(line string, width int) []string {
	var pieces []string
	var piece strings.Builder
	used := 0
	var state byte
	for len(line) > 0 {
		seq, clustered, n, next := ansi.DecodeSequence(line, state, nil)
		state, line = next, line[n:]
		// Escape sequences and combining marks take no cells of their own.
		if clustered > 0 {
			cells := clusterCells(seq, clustered)
			if used > 0 && used+cells > width {
				pieces = append(pieces, piece.String())
				piece.Reset()
				used = 0
			}
			used += cells
		}
		piece.WriteString(seq)
	}
	return append(pieces, piece.String())
}

func maxReaderOffset(offset, lines, height int) int {
	maxOff := lines - height
	if maxOff < 0 {
		maxOff = 0
	}
	if offset > maxOff {
		return maxOff
	}
	if offset < 0 {
		return 0
	}
	return offset
}

func copyExplorePath(path string) error {
	return ui.WriteClipboard(path)
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	var digits [20]byte
	i := len(digits)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		digits[i] = '-'
	}
	return string(digits[i:])
}

// safeReaderOutput preserves printable text and SGR styling only. In particular,
// OSC hyperlinks/clipboard, cursor controls, incomplete sequences, and the
// format characters CleanDocument strips are dropped.
func safeReaderOutput(text string, plain bool) string {
	var out strings.Builder
	var state byte
	for len(text) > 0 {
		seq, _, n, next := ansi.DecodeSequence(text, state, nil)
		state = next
		text = text[n:]
		switch {
		case !plain && readerSGR(seq):
			out.WriteString(seq)
		case seq == "\t":
			// A terminal expands a tab past the width the row was measured at.
			out.WriteByte(' ')
		case len(seq) == 1 && seq[0] >= 0x20 && seq[0] < 0x7f:
			out.WriteString(seq)
		case utf8.ValidString(seq):
			// The document was cleaned before Glamour saw it, but Glamour
			// decodes entities: &#x202E; arrives here as a bidi override. The
			// rule that cleaned the source cleans what it turned into.
			out.WriteString(explore.CleanDocument(seq))
		}
	}
	return out.String()
}

func readerSGR(seq string) bool {
	if !strings.HasPrefix(seq, "\x1b[") || !strings.HasSuffix(seq, "m") {
		return false
	}
	for _, r := range seq[2 : len(seq)-1] {
		if r != ';' && r != ':' && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// closeStyles makes each reader line carry its own styling. A hard break
// inside a styled token otherwise leaves the style open at the end of one
// row, where the renderer's erase paints it to the margin and into the rows
// below, and absent from the continuation when that row is drawn on its own.
func closeStyles(lines []string) []string {
	active := ""
	for i, line := range lines {
		carried := active
		rest := line
		for {
			at := strings.Index(rest, "\x1b[")
			if at < 0 {
				break
			}
			end := strings.IndexByte(rest[at:], 'm')
			if end < 0 {
				break
			}
			seq := rest[at : at+end+1]
			rest = rest[at+end+1:]
			if !readerSGR(seq) {
				continue
			}
			switch params := seq[2 : len(seq)-1]; {
			case params == "" || params == "0":
				active = ""
			case strings.HasPrefix(params, "0;"):
				active = seq
			default:
				active += seq
			}
		}
		if carried != "" {
			line = carried + line
		}
		if active != "" {
			line += "\x1b[0m"
		}
		lines[i] = line
	}
	return lines
}
