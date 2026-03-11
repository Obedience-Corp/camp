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
	navfuzzy "github.com/Obedience-Corp/camp/internal/nav/fuzzy"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

const docsBrowserVisibleEntries = 7

type docsBrowserMode int

const (
	docsBrowserModeNavigate docsBrowserMode = iota
	docsBrowserModeFilter
)

type docsBrowserChildPreview struct {
	name        string
	hasChildren bool
}

func (p docsBrowserChildPreview) displayName() string {
	if p.hasChildren {
		return p.name + "/"
	}
	return p.name
}

type docsBrowserEntry struct {
	name         string
	path         string
	hasChildren  bool
	childPreview []docsBrowserChildPreview
}

func (e docsBrowserEntry) displayName() string {
	if e.hasChildren {
		return e.name + "/"
	}
	return e.name
}

func (e docsBrowserEntry) displayPath() string {
	if e.hasChildren {
		return e.path + "/"
	}
	return e.path
}

type docsBrowserLevel struct {
	prefix       string
	entries      []docsBrowserEntry
	query        string
	selectedPath string
}

type docsBrowserModel struct {
	itemName string
	docsRoot string
	levels   []docsBrowserLevel
	input    textinput.Model

	width int
	mode  docsBrowserMode

	done      bool
	cancelled bool
	aborted   bool
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

	level, err := newDocsBrowserLevel(filepath.Join(campaignRoot, docsDirName), "")
	if err != nil {
		return docsBrowserModel{}, err
	}

	input := textinput.New()
	input.Placeholder = "Type to fuzzy-filter the current level"
	input.CharLimit = 120
	input.Prompt = "docs/"
	input.Blur()

	model := docsBrowserModel{
		itemName: itemName,
		docsRoot: filepath.Join(campaignRoot, docsDirName),
		levels:   []docsBrowserLevel{level},
		input:    input,
		width:    80,
		mode:     docsBrowserModeNavigate,
	}
	model.syncInput()
	return model, nil
}

func newDocsBrowserLevel(docsRoot, prefix string) (docsBrowserLevel, error) {
	entries, err := listDocsBrowserEntries(docsRoot, prefix)
	if err != nil {
		return docsBrowserLevel{}, err
	}

	level := docsBrowserLevel{
		prefix:  prefix,
		entries: entries,
	}
	if len(entries) > 0 {
		level.selectedPath = entries[0].path
	}
	return level, nil
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

		childPreview, err := listDocsChildPreviews(docsRoot, path)
		if err != nil {
			return nil, err
		}

		result = append(result, docsBrowserEntry{
			name:         entry.Name(),
			path:         path,
			hasChildren:  len(childPreview) > 0,
			childPreview: childPreview,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].path < result[j].path
	})
	return result, nil
}

func listDocsChildPreviews(docsRoot, path string) ([]docsBrowserChildPreview, error) {
	childDir := filepath.Join(docsRoot, filepath.FromSlash(path))
	entries, err := os.ReadDir(childDir)
	if err != nil {
		return nil, camperrors.Wrap(err, "reading docs child directories")
	}

	var result []docsBrowserChildPreview
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		childPath := path + "/" + entry.Name()
		grandchildren, err := os.ReadDir(filepath.Join(docsRoot, filepath.FromSlash(childPath)))
		if err != nil {
			return nil, camperrors.Wrap(err, "reading docs grandchild directories")
		}

		hasChildren := false
		for _, grandchild := range grandchildren {
			if grandchild.IsDir() {
				hasChildren = true
				break
			}
		}

		result = append(result, docsBrowserChildPreview{
			name:        entry.Name(),
			hasChildren: hasChildren,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].name < result[j].name
	})
	return result, nil
}

func (m docsBrowserModel) Init() tea.Cmd {
	return nil
}

func (m docsBrowserModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = max(64, min(msg.Width-4, 110))
		m.syncInput()
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			m.aborted = true
			m.done = true
			return m, tea.Quit
		case "tab", "down", "ctrl+n":
			m.next()
			return m, nil
		case "shift+tab", "up", "ctrl+p":
			m.prev()
			return m, nil
		case "enter", "right":
			m.selectCurrent()
			m.syncInput()
			if m.done {
				return m, tea.Quit
			}
			return m, nil
		}

		if m.mode == docsBrowserModeNavigate {
			switch msg.String() {
			case "j":
				m.next()
				return m, nil
			case "k":
				m.prev()
				return m, nil
			case "l":
				m.selectCurrent()
				m.syncInput()
				if m.done {
					return m, tea.Quit
				}
				return m, nil
			case "h", "left", "esc":
				m.back()
				m.syncInput()
				if m.done {
					return m, tea.Quit
				}
				return m, nil
			}

			if isDocsBrowserFilterInput(msg) {
				m.mode = docsBrowserModeFilter
				m.syncInput()
			}
		} else if msg.String() == "esc" {
			if m.input.Value() != "" {
				m.setQuery("")
				m.mode = docsBrowserModeNavigate
				m.syncInput()
				return m, nil
			}
			m.mode = docsBrowserModeNavigate
			m.back()
			m.syncInput()
			if m.done {
				return m, tea.Quit
			}
			return m, nil
		}
	}

	if m.mode == docsBrowserModeFilter {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.setQuery(m.input.Value())
		m.syncInput()
		return m, cmd
	}

	return m, nil
}

func isDocsBrowserFilterInput(msg tea.KeyMsg) bool {
	return msg.Type == tea.KeyRunes && len(msg.Runes) > 0 && !msg.Alt
}

func (m *docsBrowserModel) setQuery(query string) {
	level := m.currentLevel()
	if level == nil {
		return
	}
	level.query = query
	m.ensureSelection()
}

func (m *docsBrowserModel) syncInput() {
	level := m.currentLevel()
	if level == nil {
		return
	}

	prefix := "docs/"
	if level.prefix != "" {
		prefix = "docs/" + level.prefix + "/"
	}

	m.input.Prompt = prefix
	m.input.SetValue(level.query)
	m.input.Width = max(16, m.width-lipgloss.Width(prefix)-8)

	if m.mode == docsBrowserModeFilter {
		m.input.Focus()
	} else {
		m.input.Blur()
	}

	m.ensureSelection()
}

func (m *docsBrowserModel) ensureSelection() {
	level := m.currentLevel()
	if level == nil {
		return
	}

	matches := filteredDocsBrowserEntries(level.entries, level.query)
	if len(matches) == 0 {
		level.selectedPath = ""
		return
	}
	if level.selectedPath == "" {
		level.selectedPath = matches[0].path
		return
	}
	for _, entry := range matches {
		if entry.path == level.selectedPath {
			return
		}
	}
	level.selectedPath = matches[0].path
}

func filteredDocsBrowserEntries(entries []docsBrowserEntry, query string) []docsBrowserEntry {
	if strings.TrimSpace(query) == "" {
		return append([]docsBrowserEntry(nil), entries...)
	}

	type scoredEntry struct {
		entry docsBrowserEntry
		score int
	}

	var scored []scoredEntry
	for _, entry := range entries {
		nameScore, _ := navfuzzy.Score(query, entry.name)
		pathScore, _ := navfuzzy.Score(query, entry.path)
		score := max(nameScore, pathScore)
		if score == 0 {
			continue
		}
		scored = append(scored, scoredEntry{entry: entry, score: score})
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].entry.path < scored[j].entry.path
		}
		return scored[i].score > scored[j].score
	})

	result := make([]docsBrowserEntry, 0, len(scored))
	for _, match := range scored {
		result = append(result, match.entry)
	}
	return result
}

func (m *docsBrowserModel) next() {
	matches := m.currentMatches()
	if len(matches) == 0 {
		return
	}

	idx := m.currentIndex(matches)
	level := m.currentLevel()
	level.selectedPath = matches[(idx+1)%len(matches)].path
}

func (m *docsBrowserModel) prev() {
	matches := m.currentMatches()
	if len(matches) == 0 {
		return
	}

	idx := m.currentIndex(matches)
	level := m.currentLevel()
	level.selectedPath = matches[(idx-1+len(matches))%len(matches)].path
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
		m.mode = docsBrowserModeNavigate
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
	m.mode = docsBrowserModeNavigate
}

func (m *docsBrowserModel) currentLevel() *docsBrowserLevel {
	if len(m.levels) == 0 {
		return nil
	}
	return &m.levels[len(m.levels)-1]
}

func (m docsBrowserModel) currentMatches() []docsBrowserEntry {
	level := m.currentLevel()
	if level == nil {
		return nil
	}
	return filteredDocsBrowserEntries(level.entries, level.query)
}

func (m docsBrowserModel) currentIndex(matches []docsBrowserEntry) int {
	level := m.currentLevel()
	if level == nil || len(matches) == 0 {
		return 0
	}

	for i, entry := range matches {
		if entry.path == level.selectedPath {
			return i
		}
	}
	return 0
}

func (m docsBrowserModel) currentEntry() (docsBrowserEntry, bool) {
	matches := m.currentMatches()
	if len(matches) == 0 {
		return docsBrowserEntry{}, false
	}

	level := m.currentLevel()
	for _, entry := range matches {
		if entry.path == level.selectedPath {
			return entry, true
		}
	}
	return matches[0], true
}

func (m docsBrowserModel) View() string {
	pal := theme.TUI()

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	descStyle := lipgloss.NewStyle().Foreground(pal.TextMuted)
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(pal.AccentAlt)
	boxStyle := lipgloss.NewStyle().
		Width(m.width).
		Border(lipgloss.NormalBorder()).
		BorderForeground(pal.BorderFocus).
		Padding(0, 1)
	selectedStyle := lipgloss.NewStyle().
		Width(m.width).
		Background(pal.BgSelected).
		Bold(true).
		Foreground(pal.TextPrimary)
	normalStyle := lipgloss.NewStyle().
		Width(m.width).
		Foreground(pal.TextSecondary)
	mutedStyle := lipgloss.NewStyle().Foreground(pal.TextMuted)
	successStyle := lipgloss.NewStyle().Foreground(pal.Success)
	warningStyle := lipgloss.NewStyle().Foreground(pal.Warning)

	level := m.currentLevel()
	matches := m.currentMatches()
	selectedEntry, hasSelection := m.currentEntry()

	var b strings.Builder
	b.WriteString(titleStyle.Render(fmt.Sprintf("Route %s to docs/ subdirectory", m.itemName)))
	b.WriteString("\n")
	b.WriteString(descStyle.Render(m.breadcrumb()))
	b.WriteString("\n\n")

	filterTitle := "Filter"
	if m.mode == docsBrowserModeFilter {
		filterTitle = "Filter (typing)"
	}
	b.WriteString(sectionStyle.Render(filterTitle))
	b.WriteString("\n")
	b.WriteString(boxStyle.Render(m.input.View()))
	b.WriteString("\n\n")

	selectedLabel := "Selected: (no matching directory)"
	if hasSelection {
		selectedLabel = "Selected: docs/" + selectedEntry.displayPath()
	}
	b.WriteString(successStyle.Render(selectedLabel))
	b.WriteString("\n\n")

	scope := "docs/"
	if level != nil && level.prefix != "" {
		scope = "docs/" + level.prefix + "/"
	}
	b.WriteString(sectionStyle.Render(fmt.Sprintf("Matches in %s (%d/%d)", scope, len(matches), len(level.entries))))
	b.WriteString("\n")
	if len(matches) == 0 {
		b.WriteString(warningStyle.Render(fmt.Sprintf("No directories at this level match %q.", level.query)))
		b.WriteString("\n")
	} else {
		start, end := visibleDocsBrowserRange(len(matches), m.currentIndex(matches))
		for i := start; i < end; i++ {
			entry := matches[i]
			line := "  " + entry.displayName()
			if entry.path == level.selectedPath {
				line = "> " + entry.displayName()
				b.WriteString(selectedStyle.Render(line))
			} else {
				b.WriteString(normalStyle.Render(line))
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(sectionStyle.Render("Children"))
	b.WriteString("\n")
	switch {
	case !hasSelection:
		b.WriteString(mutedStyle.Render("No child preview while there are no matches."))
	case len(selectedEntry.childPreview) == 0:
		b.WriteString(mutedStyle.Render("Leaf directory. Press Enter to route here."))
	default:
		preview := min(len(selectedEntry.childPreview), 6)
		for i := 0; i < preview; i++ {
			b.WriteString(normalStyle.Render("  " + selectedEntry.childPreview[i].displayName()))
			b.WriteString("\n")
		}
		if len(selectedEntry.childPreview) > preview {
			b.WriteString(mutedStyle.Render(fmt.Sprintf("  … %d more", len(selectedEntry.childPreview)-preview)))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	if m.mode == docsBrowserModeFilter {
		b.WriteString(mutedStyle.Render("Type to fuzzy filter • ↑/↓/Tab move • Enter/→ select • Esc clear filter/back • Ctrl+C quit"))
	} else {
		b.WriteString(mutedStyle.Render("↑/↓/Tab or j/k move • Enter/→ or l open/select • Esc/← or h back • Type to filter • Ctrl+C quit"))
	}

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

	programOpts := []tea.ProgramOption{
		tea.WithContext(ctx),
		tea.WithAltScreen(),
	}

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
	if browserModel.aborted {
		return "", ErrCrawlAborted
	}
	if browserModel.cancelled {
		return "", nil
	}
	return browserModel.selected, nil
}
