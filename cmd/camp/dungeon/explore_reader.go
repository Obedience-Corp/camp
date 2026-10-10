package dungeon

import (
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"

	"github.com/Obedience-Corp/camp/internal/ui"
)

type readerEntry struct {
	name string
	path string
}

type exploreReader struct {
	title   string
	path    string
	lines   []string
	offset  int
	listing bool
	entries []readerEntry
	entry   int
}

func (m exploreModel) openReader() (tea.Model, tea.Cmd) {
	item, ok := m.focused()
	if !ok {
		return m, nil
	}
	abs := joinAbs(m.root, item.Path)
	reader := exploreReader{title: item.Title, path: abs}
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
		reader.lines = readerListLines(entries)
	case item.Kind == "festival" && item.IsDir:
		goal := filepath.Join(abs, "FESTIVAL_GOAL.md")
		if _, err := os.Stat(goal); err == nil {
			lines, err := readExploreBody(goal, m.contentWidth())
			if err != nil {
				m.statusErr = true
				m.status = "The file could not be parsed."
				return m, nil
			}
			reader.path = goal
			reader.lines = lines
		} else if item.Summary != "" {
			reader.lines = wrapPlain(item.Summary, m.contentWidth())
		} else {
			reader.lines = []string{"No festival goal file."}
		}
		if item.Replay == "" {
			reader.lines = append([]string{"No replay yet.", ""}, reader.lines...)
		}
	case !item.IsDir && strings.HasSuffix(strings.ToLower(item.Path), ".md"):
		lines, err := readExploreBody(abs, m.contentWidth())
		if err != nil {
			m.statusErr = true
			m.status = "The file could not be parsed."
			return m, nil
		}
		reader.lines = lines
	default:
		info, err := os.Lstat(abs)
		if err != nil {
			reader.lines = []string{abs}
		} else {
			reader.lines = []string{abs, info.ModTime().Format("2006-01-02 15:04"), "size " + itoa64(info.Size())}
		}
	}
	m.reading = true
	m.reader = reader
	m.help = false
	return m, nil
}

func (m exploreModel) onReaderKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "esc", "q":
		m.reading = false
		return m, nil
	case "up", "k":
		if m.reader.listing {
			if m.reader.entry > 0 {
				m.reader.entry--
			}
			return m, nil
		}
		if m.reader.offset > 0 {
			m.reader.offset--
		}
		return m, nil
	case "down", "j":
		if m.reader.listing {
			if m.reader.entry < len(m.reader.entries)-1 {
				m.reader.entry++
			}
			return m, nil
		}
		m.reader.offset = maxReaderOffset(m.reader.offset+1, len(m.reader.lines), m.bodyHeight())
		return m, nil
	case "ctrl+u":
		m.reader.offset -= m.page()
		if m.reader.offset < 0 {
			m.reader.offset = 0
		}
		return m, nil
	case "ctrl+d":
		m.reader.offset = maxReaderOffset(m.reader.offset+m.page(), len(m.reader.lines), m.bodyHeight())
		return m, nil
	case "enter":
		if !m.reader.listing || len(m.reader.entries) == 0 {
			return m, nil
		}
		picked := m.reader.entries[m.reader.entry]
		lines, err := readExploreBody(picked.path, m.contentWidth())
		if err != nil {
			m.statusErr = true
			m.status = "The file could not be parsed."
			return m, nil
		}
		m.reader.listing = false
		m.reader.path = picked.path
		m.reader.title = picked.name
		m.reader.lines = lines
		m.reader.offset = 0
		return m, nil
	case "g":
		return m.hop()
	case "y":
		return m.copyPath(m.reader.path)
	case " ":
		return m.togglePlay()
	}
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
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
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

func readerListLines(entries []readerEntry) []string {
	if len(entries) == 0 {
		return []string{"No markdown files."}
	}
	lines := make([]string, len(entries))
	for i, entry := range entries {
		lines[i] = entry.name
	}
	return lines
}

func readExploreBody(path string, width int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, 256<<10)
	n, _ := f.Read(buf)
	text := string(buf[:n])
	if width < 20 {
		width = 20
	}
	rendered, err := glamour.NewTermRenderer(glamour.WithStylePath("dark"), glamour.WithWordWrap(width))
	if err != nil {
		return wrapPlain(text, width), nil
	}
	out, err := rendered.Render(text)
	if err != nil {
		return wrapPlain(text, width), nil
	}
	return strings.Split(strings.TrimRight(out, "\n"), "\n"), nil
}

func wrapPlain(text string, width int) []string {
	if width < 20 {
		width = 20
	}
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		for len(line) > width {
			lines = append(lines, line[:width])
			line = line[width:]
		}
		lines = append(lines, line)
	}
	return lines
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
