package dungeon

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/Obedience-Corp/camp/internal/dungeon/explore"
	"github.com/Obedience-Corp/camp/internal/ui"
)

type readerEntry struct {
	name string
	path string
}

type exploreReader struct {
	item    explore.Item
	title   string
	path    string
	body    readerBody
	lines   []string
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
	text := explore.CleanDocument(b.text)
	var lines []string
	if b.markdown {
		lines = renderExploreMarkdown(explore.MarkdownBody(text), width, plain)
	} else {
		lines = wrapPlain(text, width)
	}
	return append(slices.Clone(b.lead), lines...)
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
	m.reader.lines = m.reader.body.render(m.contentWidth(), m.plain)
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
			m.statusErr = true
			m.status = err.Error()
			return m, nil
		}
		reader.listing = true
		reader.entries = entries
	case item.Kind == "festival" && item.IsDir:
		goal := filepath.Join(abs, "FESTIVAL_GOAL.md")
		if _, err := os.Stat(goal); err == nil {
			text, err := readExploreText(m.readerContext(), goal)
			if err != nil {
				m.statusErr = true
				m.status = "The file could not be parsed."
				return m, nil
			}
			reader.path = goal
			reader.body = readerBody{text: text, markdown: true}
		} else if item.Summary != "" {
			reader.body = readerBody{text: item.Summary}
		} else {
			reader.body = readerBody{text: "No festival goal file."}
		}
		if item.Replay == "" {
			reader.body.lead = []string{"No replay yet.", ""}
		}
	case !item.IsDir && strings.HasSuffix(strings.ToLower(item.Path), ".md"):
		text, err := readExploreText(m.readerContext(), abs)
		if err != nil {
			m.statusErr = true
			m.status = "The file could not be parsed."
			return m, nil
		}
		reader.body = readerBody{text: text, markdown: true}
	default:
		info, err := os.Lstat(abs)
		if err != nil {
			reader.body = readerBody{text: abs}
		} else {
			reader.body = readerBody{text: abs + "\n" + info.ModTime().Format("2006-01-02 15:04") + "\nsize " + itoa64(info.Size())}
		}
	}
	if !reader.listing {
		reader.lines = reader.body.render(m.contentWidth(), m.plain)
	}
	m.reading = true
	m.reader = reader
	m.help = false
	return m, nil
}

func (m exploreModel) onReaderKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "ctrl+c":
		return m.quit()
	case "esc", "q":
		m.reading = false
		return m, nil
	case "up", "k":
		if m.reader.listing {
			return m.moveEntry(-1)
		}
		if m.reader.offset > 0 {
			m.reader.offset--
		}
		return m, nil
	case "down", "j":
		if m.reader.listing {
			return m.moveEntry(1)
		}
		m.reader.offset = maxReaderOffset(m.reader.offset+1, len(m.reader.lines), m.bodyHeight())
		return m, nil
	case "ctrl+u":
		if m.reader.listing {
			return m.moveEntry(-m.page())
		}
		m.reader.offset -= m.page()
		if m.reader.offset < 0 {
			m.reader.offset = 0
		}
		return m, nil
	case "ctrl+d":
		if m.reader.listing {
			return m.moveEntry(m.page())
		}
		m.reader.offset = maxReaderOffset(m.reader.offset+m.page(), len(m.reader.lines), m.bodyHeight())
		return m, nil
	case "enter":
		if !m.reader.listing || len(m.reader.entries) == 0 {
			return m, nil
		}
		picked := m.reader.entries[m.reader.entry]
		text, err := readExploreText(m.readerContext(), picked.path)
		if err != nil {
			m.statusErr = true
			m.status = "The file could not be parsed."
			return m, nil
		}
		m.reader.listing = false
		m.reader.path = picked.path
		m.reader.title = picked.name
		m.reader.body = readerBody{text: text, markdown: true}
		m.reader.lines = m.reader.body.render(m.contentWidth(), m.plain)
		m.reader.offset = 0
		return m, nil
	case "g":
		return m.hopTo(m.reader.item)
	case "y":
		return m.copyPath(m.reader.path)
	case " ":
		return m.togglePlay()
	}
	return m, nil
}

func (m exploreModel) moveEntry(delta int) (tea.Model, tea.Cmd) {
	if len(m.reader.entries) == 0 {
		return m, nil
	}
	m.reader.entry = min(max(m.reader.entry+delta, 0), len(m.reader.entries)-1)
	m.reader.follow(m.bodyHeight())
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

func readExploreText(ctx context.Context, path string) (string, error) {
	data, err := explore.ReadRegular(ctx, path, 256<<10)
	return string(data), err
}

func renderExploreMarkdown(text string, width int, plain bool) []string {
	width = max(width, 1)
	opts := []glamour.TermRendererOption{glamour.WithStylePath("dark"), glamour.WithWordWrap(width)}
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
	return wrapPlain(strings.TrimRight(out, "\n"), width)
}

func wrapPlain(text string, width int) []string {
	width = max(width, 1)
	return strings.Split(ansi.Hardwrap(text, width, true), "\n")
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
// OSC hyperlinks/clipboard, cursor controls and incomplete sequences are dropped.
func safeReaderOutput(text string, plain bool) string {
	var out strings.Builder
	var state byte
	for len(text) > 0 {
		seq, _, n, next := ansi.DecodeSequence(text, state, nil)
		state = next
		text = text[n:]
		if !plain && readerSGR(seq) {
			out.WriteString(seq)
		} else if utf8.ValidString(seq) && !strings.ContainsFunc(seq, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) {
			out.WriteString(seq)
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
