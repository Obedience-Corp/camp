package crawl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
)

const destinationPickerVisibleEntries = 7

// pickerModel renders a list of destination Options with item
// counts. Esc returns the empty Option (caller treats as "back");
// ctrl+c sets aborted; enter selects.
type pickerModel struct {
	item    Item
	options []Option

	width    int
	selected int

	done      bool
	cancelled bool
	aborted   bool
	value     Option
}

func newPickerModel(item Item, options []Option) (pickerModel, error) {
	if len(options) == 0 {
		return pickerModel{}, fmt.Errorf("no destination options provided")
	}
	return pickerModel{
		item:    item,
		options: options,
		width:   72,
	}, nil
}

func (m pickerModel) Init() tea.Cmd { return nil }

func (m pickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = clampWidth(msg.Width)
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
			if len(m.options) == 0 {
				return m, nil
			}
			m.value = m.options[m.selected]
			m.done = true
			return m, tea.Quit
		case "down", "tab", "ctrl+n", "j":
			if len(m.options) > 0 {
				m.selected = (m.selected + 1) % len(m.options)
			}
			return m, nil
		case "up", "shift+tab", "ctrl+p", "k":
			if len(m.options) > 0 {
				m.selected = (m.selected - 1 + len(m.options)) % len(m.options)
			}
			return m, nil
		}
	}
	return m, nil
}

func (m pickerModel) View() string {
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
	b.WriteString(titleStyle.Render(fmt.Sprintf("Move %s", displayName(m.item))))
	b.WriteString("\n")
	b.WriteString(descStyle.Render("Choose a destination. Esc returns to the previous menu without moving anything."))
	b.WriteString("\n\n")
	b.WriteString(sectionStyle.Render(fmt.Sprintf("Destinations (%d)", len(m.options))))
	b.WriteString("\n")

	start, end := visiblePickerRange(len(m.options), m.selected)
	for i := start; i < end; i++ {
		line := "  " + renderOptionLabel(m.options[i])
		if i == m.selected {
			line = "> " + renderOptionLabel(m.options[i])
			b.WriteString(selectedStyle.Render(line))
		} else {
			b.WriteString(normalStyle.Render(line))
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("↑/↓/Tab or j/k move • Enter select • Esc back • Ctrl+C quit"))
	return b.String()
}

func renderOptionLabel(o Option) string {
	if o.Count > 0 {
		return fmt.Sprintf("%s (%d items)", o.Label, o.Count)
	}
	return o.Label
}

func displayName(item Item) string {
	if item.Title != "" {
		return item.Title
	}
	return item.ID
}

func clampWidth(w int) int {
	return max(56, min(w-4, 96))
}

func visiblePickerRange(total, selected int) (int, int) {
	if total <= destinationPickerVisibleEntries {
		return 0, total
	}
	half := destinationPickerVisibleEntries / 2
	start := selected - half
	if start < 0 {
		start = 0
	}
	end := start + destinationPickerVisibleEntries
	if end > total {
		end = total
		start = total - destinationPickerVisibleEntries
	}
	return start, end
}

// runDestinationPicker drives the bubbletea picker model and maps
// its terminal state to the Prompt.SelectDestination contract.
func runDestinationPicker(ctx context.Context, item Item, options []Option) (Option, error) {
	model, err := newPickerModel(item, options)
	if err != nil {
		return Option{}, err
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
			return Option{}, camperrors.Wrap(ctx.Err(), "context cancelled")
		}
		return Option{}, camperrors.Wrap(err, "running destination picker")
	}

	pm, ok := finalModel.(pickerModel)
	if !ok {
		return Option{}, fmt.Errorf("unexpected picker model type %T", finalModel)
	}
	if pm.aborted {
		return Option{}, ErrAborted
	}
	if pm.cancelled {
		return Option{}, nil
	}
	return pm.value, nil
}
