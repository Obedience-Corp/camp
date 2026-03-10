package dungeon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

const docsBrowserVisibleEntries = 7

type docsBrowserEntry struct {
	name        string
	path        string
	hasChildren bool
}

func (e docsBrowserEntry) displayPath() string {
	if e.hasChildren {
		return e.path + "/"
	}
	return e.path
}

type docsBrowserLevel struct {
	prefix   string
	entries  []docsBrowserEntry
	selected int
}

type docsBrowserModel struct {
	itemName string
	docsRoot string
	levels   []docsBrowserLevel

	width     int
	done      bool
	cancelled bool
	selected  string
	err       error
}

func newDocsBrowserModel(itemName, campaignRoot string) (docsBrowserModel, error) {
	allDirs, err := listDocsSubdirectories(campaignRoot)
	if err != nil {
		return docsBrowserModel{}, camperrors.Wrap(err, "listing docs subdirectories")
	}
	if len(allDirs) == 0 {
		return docsBrowserModel{}, camperrors.Wrap(
			ErrInvalidDocsDestination,
			"no docs subdirectories exist under campaign-root docs/",
		)
	}

	docsRoot := filepath.Join(campaignRoot, docsDirName)
	level, err := newDocsBrowserLevel(docsRoot, "")
	if err != nil {
		return docsBrowserModel{}, err
	}

	return docsBrowserModel{
		itemName: itemName,
		docsRoot: docsRoot,
		levels:   []docsBrowserLevel{level},
		width:    72,
	}, nil
}

func newDocsBrowserLevel(docsRoot, prefix string) (docsBrowserLevel, error) {
	entries, err := listDocsBrowserEntries(docsRoot, prefix)
	if err != nil {
		return docsBrowserLevel{}, err
	}
	return docsBrowserLevel{
		prefix:   prefix,
		entries:  entries,
		selected: 0,
	}, nil
}

func listDocsBrowserEntries(docsRoot, prefix string) ([]docsBrowserEntry, error) {
	dir := docsRoot
	if prefix != "" {
		dir = filepath.Join(docsRoot, filepath.FromSlash(prefix))
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, camperrors.Wrap(
				ErrInvalidDocsDestination,
				"campaign-root docs/ directory does not exist",
			)
		}
		return nil, camperrors.Wrap(err, "reading docs subdirectories")
	}

	var result []docsBrowserEntry
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		path := entry.Name()
		if prefix != "" {
			path = prefix + "/" + entry.Name()
		}

		hasChildren, err := docsEntryHasChildren(docsRoot, path)
		if err != nil {
			return nil, err
		}

		result = append(result, docsBrowserEntry{
			name:        entry.Name(),
			path:        path,
			hasChildren: hasChildren,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].path < result[j].path
	})
	return result, nil
}

func docsEntryHasChildren(docsRoot, path string) (bool, error) {
	childDir := filepath.Join(docsRoot, filepath.FromSlash(path))
	entries, err := os.ReadDir(childDir)
	if err != nil {
		return false, camperrors.Wrap(err, "reading docs child directories")
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return true, nil
		}
	}
	return false, nil
}

func (m docsBrowserModel) Init() tea.Cmd {
	return nil
}

func (m docsBrowserModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = max(48, min(msg.Width-4, 96))
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyTab, tea.KeyDown:
			m.next()
		case tea.KeyShiftTab, tea.KeyUp:
			m.prev()
		case tea.KeyEnter:
			m.selectCurrent()
		case tea.KeyEsc, tea.KeyCtrlC:
			m.back()
		}
	}

	if m.done {
		return m, tea.Quit
	}
	return m, nil
}

func (m *docsBrowserModel) next() {
	level := m.currentLevel()
	if level == nil || len(level.entries) == 0 {
		return
	}
	level.selected = (level.selected + 1) % len(level.entries)
}

func (m *docsBrowserModel) prev() {
	level := m.currentLevel()
	if level == nil || len(level.entries) == 0 {
		return
	}
	level.selected = (level.selected - 1 + len(level.entries)) % len(level.entries)
}

func (m *docsBrowserModel) selectCurrent() {
	entry, ok := m.currentEntry()
	if !ok {
		return
	}

	if entry.hasChildren {
		level, err := newDocsBrowserLevel(m.docsRoot, entry.path)
		if err != nil {
			m.err = err
			m.done = true
			return
		}
		m.levels = append(m.levels, level)
		return
	}

	m.selected = entry.path
	m.done = true
}

func (m *docsBrowserModel) back() {
	if len(m.levels) <= 1 {
		m.cancelled = true
		m.done = true
		return
	}
	m.levels = m.levels[:len(m.levels)-1]
}

func (m *docsBrowserModel) currentLevel() *docsBrowserLevel {
	if len(m.levels) == 0 {
		return nil
	}
	return &m.levels[len(m.levels)-1]
}

func (m docsBrowserModel) currentEntry() (docsBrowserEntry, bool) {
	level := m.currentLevel()
	if level == nil || len(level.entries) == 0 {
		return docsBrowserEntry{}, false
	}
	return level.entries[level.selected], true
}

func (m docsBrowserModel) View() string {
	pal := theme.TUI()

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	descStyle := lipgloss.NewStyle().Foreground(pal.TextMuted)
	inputStyle := lipgloss.NewStyle().
		Width(max(24, m.width)).
		Border(lipgloss.NormalBorder()).
		BorderForeground(pal.BorderFocus).
		Padding(0, 1)
	selectedStyle := lipgloss.NewStyle().
		Width(max(24, m.width)).
		Background(pal.BgSelected).
		Bold(true).
		Foreground(pal.TextPrimary)
	normalStyle := lipgloss.NewStyle().
		Width(max(24, m.width)).
		Foreground(pal.TextSecondary)
	helpStyle := lipgloss.NewStyle().Foreground(pal.TextMuted)

	current := "docs/"
	if entry, ok := m.currentEntry(); ok {
		current = "docs/" + entry.displayPath()
	}

	var b strings.Builder
	b.WriteString(titleStyle.Render(fmt.Sprintf("Route %s to docs/ subdirectory:", m.itemName)))
	b.WriteString("\n")
	b.WriteString(descStyle.Render(m.breadcrumb()))
	b.WriteString("\n\n")
	b.WriteString(inputStyle.Render(current))
	b.WriteString("\n\n")

	level := m.currentLevel()
	if level == nil || len(level.entries) == 0 {
		b.WriteString(helpStyle.Render("(no docs subdirectories)"))
	} else {
		start, end := visibleDocsBrowserRange(len(level.entries), level.selected)
		for i := start; i < end; i++ {
			entry := level.entries[i]
			line := "  " + entry.displayPath()
			if i == level.selected {
				line = "> " + entry.displayPath()
				b.WriteString(selectedStyle.Render(line))
			} else {
				b.WriteString(normalStyle.Render(line))
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("Tab/Shift+Tab: cycle • Enter: open/select • Esc: back"))

	return b.String()
}

func (m docsBrowserModel) breadcrumb() string {
	level := m.currentLevel()
	if level == nil || level.prefix == "" {
		return "Browsing docs/"
	}
	return "Browsing docs/" + level.prefix + "/"
}

func visibleDocsBrowserRange(total, selected int) (int, int) {
	if total <= docsBrowserVisibleEntries {
		return 0, total
	}

	half := docsBrowserVisibleEntries / 2
	start := selected - half
	if start < 0 {
		start = 0
	}
	end := start + docsBrowserVisibleEntries
	if end > total {
		end = total
		start = total - docsBrowserVisibleEntries
	}
	return start, end
}

func runDocsBrowser(ctx context.Context, itemName, campaignRoot string) (string, error) {
	model, err := newDocsBrowserModel(itemName, campaignRoot)
	if err != nil {
		return "", err
	}

	programOpts := []tea.ProgramOption{tea.WithContext(ctx)}
	var tty *os.File
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		tty, err = os.OpenFile("/dev/tty", os.O_RDWR, 0)
		if err == nil {
			programOpts = append(programOpts, tea.WithInput(tty), tea.WithOutput(tty))
		}
	}
	if tty != nil {
		defer tty.Close()
	}

	finalModel, err := tea.NewProgram(model, programOpts...).Run()
	if err != nil {
		if ctx.Err() != nil && errors.Is(err, tea.ErrProgramKilled) {
			return "", camperrors.Wrap(ctx.Err(), "context cancelled")
		}
		return "", camperrors.Wrap(err, "running docs browser")
	}

	browserModel, ok := finalModel.(docsBrowserModel)
	if !ok {
		return "", fmt.Errorf("unexpected docs browser model type %T", finalModel)
	}
	if browserModel.err != nil {
		return "", browserModel.err
	}
	if browserModel.cancelled {
		return "", nil
	}
	return browserModel.selected, nil
}
