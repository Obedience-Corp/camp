package promote

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/intent/tui"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/workitem"
)

type selectorModel struct {
	items    []workitem.WorkItem
	selected int
	picked   bool
	aborted  bool
	value    workitem.WorkItem
}

func (m selectorModel) Init() tea.Cmd { return nil }

func (m selectorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "ctrl+c":
		m.aborted = true
		return m, tea.Quit
	case "esc", "q":
		return m, tea.Quit
	case "enter":
		if len(m.items) > 0 {
			m.value = m.items[m.selected]
			m.picked = true
			return m, tea.Quit
		}
	case "down", "j", "tab":
		if len(m.items) > 0 {
			m.selected = (m.selected + 1) % len(m.items)
		}
	case "up", "k", "shift+tab":
		if len(m.items) > 0 {
			m.selected = (m.selected - 1 + len(m.items)) % len(m.items)
		}
	}
	return m, nil
}

func (m selectorModel) View() string {
	var b strings.Builder
	b.WriteString(tui.TitleStyle.Render("Promote") + "\n\n")
	for i, it := range m.items {
		marker := "  "
		if i == m.selected {
			marker = "> "
		}
		b.WriteString(marker + selectorLabel(it) + "\n")
	}
	b.WriteString("\n" + tui.HelpStyle.Render("j/k: navigate . Enter: select . Esc: cancel"))
	return b.String()
}

func selectorLabel(it workitem.WorkItem) string {
	return fmt.Sprintf("%-9s %s (%s)", string(it.WorkflowType), it.Title, string(it.LifecycleStage))
}

func selectItem(ctx context.Context, campaignRoot string, resolver *paths.Resolver) (workitem.WorkItem, error) {
	pool, err := promotablePool(ctx, campaignRoot, resolver)
	if err != nil {
		return workitem.WorkItem{}, err
	}
	if len(pool) == 0 {
		return workitem.WorkItem{}, camperrors.New("nothing promotable found")
	}

	final, rerr := runPicker(ctx, selectorModel{items: pool})
	if rerr != nil {
		return workitem.WorkItem{}, camperrors.Wrap(rerr, "running promote selector")
	}
	sm, ok := final.(selectorModel)
	if !ok || sm.aborted || !sm.picked {
		return workitem.WorkItem{}, camperrors.New("promote cancelled")
	}
	return sm.value, nil
}

type targetModel struct {
	title    string
	options  []string
	selected int
	picked   bool
	aborted  bool
	value    string
}

func (m targetModel) Init() tea.Cmd { return nil }

func (m targetModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "ctrl+c":
		m.aborted = true
		return m, tea.Quit
	case "esc", "q":
		return m, tea.Quit
	case "enter":
		if len(m.options) > 0 {
			m.value = m.options[m.selected]
			m.picked = true
			return m, tea.Quit
		}
	case "down", "j", "tab":
		if len(m.options) > 0 {
			m.selected = (m.selected + 1) % len(m.options)
		}
	case "up", "k", "shift+tab":
		if len(m.options) > 0 {
			m.selected = (m.selected - 1 + len(m.options)) % len(m.options)
		}
	}
	return m, nil
}

func (m targetModel) View() string {
	var b strings.Builder
	b.WriteString(tui.TitleStyle.Render(m.title) + "\n\n")
	for i, opt := range m.options {
		marker := "  "
		if i == m.selected {
			marker = "> "
		}
		b.WriteString(marker + opt + "\n")
	}
	b.WriteString("\n" + tui.HelpStyle.Render("j/k: navigate . Enter: select . Esc: cancel"))
	return b.String()
}

func targetsForKind(kind promoteKind) []string {
	switch kind {
	case kindIntent:
		return []string{"ready", "festival", "design"}
	case kindWorkitem:
		return []string{"festival", "doc", "completed", "archived", "someday"}
	case kindFestival:
		return []string{"next", "completed", "archived", "someday"}
	}
	return nil
}

func pickTarget(ctx context.Context, kind promoteKind) (string, error) {
	options := targetsForKind(kind)
	if len(options) == 0 {
		return "", camperrors.New("no targets available for item")
	}
	final, rerr := runPicker(ctx, targetModel{title: "Promote target", options: options})
	if rerr != nil {
		return "", camperrors.Wrap(rerr, "running target picker")
	}
	tm, ok := final.(targetModel)
	if !ok || tm.aborted || !tm.picked {
		return "", camperrors.New("promote cancelled")
	}
	return tm.value, nil
}

func runPicker(ctx context.Context, model tea.Model) (tea.Model, error) {
	opts := []tea.ProgramOption{tea.WithContext(ctx), tea.WithAltScreen()}
	var tty *os.File
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		if f, oerr := os.OpenFile("/dev/tty", os.O_RDWR, 0); oerr == nil {
			tty = f
			opts = append(opts, tea.WithInput(tty), tea.WithOutput(tty))
		}
	}
	if tty != nil {
		defer func() { _ = tty.Close() }()
	}
	return tea.NewProgram(model, opts...).Run()
}
