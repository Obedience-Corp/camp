package dungeon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

const statusPickerVisibleEntries = 7

type statusPickerEntry struct {
	label string
	value string
}

type statusPickerModel struct {
	itemName string
	entries  []statusPickerEntry

	width    int
	selected int

	done      bool
	cancelled bool
	aborted   bool
	value     string
}

func newStatusPickerModel(itemName string, dirs []StatusDir) (statusPickerModel, error) {
	if len(dirs) == 0 {
		return statusPickerModel{}, fmt.Errorf("no status directories found. Run 'camp dungeon init' to create defaults")
	}

	entries := make([]statusPickerEntry, 0, len(dirs))
	for _, dir := range dirs {
		entries = append(entries, statusPickerEntry{
			label: fmt.Sprintf("%s/ (%d items)", dir.Name, dir.ItemCount),
			value: dir.Name,
		})
	}

	return statusPickerModel{
		itemName: itemName,
		entries:  entries,
		width:    72,
	}, nil
}

func (m statusPickerModel) Init() tea.Cmd {
	return nil
}

func (m statusPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = max(56, min(msg.Width-4, 96))
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			m.aborted = true
			m.done = true
			return m, tea.Quit
		case "esc":
			m.cancelled = true
			m.done = true
			return m, tea.Quit
		case "enter":
			if len(m.entries) == 0 {
				return m, nil
			}
			m.value = m.entries[m.selected].value
			m.done = true
			return m, tea.Quit
		case "down", "tab", "ctrl+n", "j":
			if len(m.entries) > 0 {
				m.selected = (m.selected + 1) % len(m.entries)
			}
			return m, nil
		case "up", "shift+tab", "ctrl+p", "k":
			if len(m.entries) > 0 {
				m.selected = (m.selected - 1 + len(m.entries)) % len(m.entries)
			}
			return m, nil
		}
	}

	return m, nil
}

func (m statusPickerModel) View() string {
	pal := theme.TUI()

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(pal.Accent)
	descStyle := lipgloss.NewStyle().Foreground(pal.TextMuted)
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(pal.AccentAlt)
	selectedStyle := lipgloss.NewStyle().
		Width(m.width).
		Background(pal.BgSelected).
		Bold(true).
		Foreground(pal.TextPrimary)
	normalStyle := lipgloss.NewStyle().
		Width(m.width).
		Foreground(pal.TextSecondary)
	mutedStyle := lipgloss.NewStyle().Foreground(pal.TextMuted)

	var b strings.Builder
	b.WriteString(titleStyle.Render(fmt.Sprintf("Move %s to dungeon status", m.itemName)))
	b.WriteString("\n")
	b.WriteString(descStyle.Render("Choose a destination. Esc returns to the previous crawl menu without moving anything."))
	b.WriteString("\n\n")
	b.WriteString(sectionStyle.Render(fmt.Sprintf("Statuses (%d)", len(m.entries))))
	b.WriteString("\n")

	start, end := visibleStatusPickerRange(len(m.entries), m.selected)
	for i := start; i < end; i++ {
		line := "  " + m.entries[i].label
		if i == m.selected {
			line = "> " + m.entries[i].label
			b.WriteString(selectedStyle.Render(line))
		} else {
			b.WriteString(normalStyle.Render(line))
		}
		b.WriteString("\n")
	}

	if len(m.entries) == 0 {
		b.WriteString(mutedStyle.Render("No status directories available."))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("↑/↓/Tab or j/k move • Enter select • Esc back • Ctrl+C quit"))
	return b.String()
}

func visibleStatusPickerRange(total, selected int) (int, int) {
	if total <= statusPickerVisibleEntries {
		return 0, total
	}

	half := statusPickerVisibleEntries / 2
	start := selected - half
	if start < 0 {
		start = 0
	}
	end := start + statusPickerVisibleEntries
	if end > total {
		end = total
		start = total - statusPickerVisibleEntries
	}
	return start, end
}

func runStatusPicker(ctx context.Context, itemName string, dirs []StatusDir) (string, error) {
	model, err := newStatusPickerModel(itemName, dirs)
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
		return "", camperrors.Wrap(err, "running status picker")
	}

	pickerModel, ok := finalModel.(statusPickerModel)
	if !ok {
		return "", fmt.Errorf("unexpected status picker model type %T", finalModel)
	}
	if pickerModel.aborted {
		return "", ErrCrawlAborted
	}
	if pickerModel.cancelled {
		return "", nil
	}
	return pickerModel.value, nil
}
