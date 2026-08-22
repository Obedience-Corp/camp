//go:build dev

package quest

import (
	"os"
	"path/filepath"
	"slices"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/quest"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

// stdoutIsTTY reports whether stdout is an interactive terminal. Overridden in
// tests so dispatch does not depend on the runner's terminal.
var stdoutIsTTY = func() bool { return term.IsTerminal(int(os.Stdout.Fd())) }

type questListFilter int

const (
	questFilterLive questListFilter = iota
	questFilterAll
	questFilterDungeon
)

func (f questListFilter) label() string {
	switch f {
	case questFilterAll:
		return "all"
	case questFilterDungeon:
		return "dungeon"
	default:
		return "live"
	}
}

func (f questListFilter) next() questListFilter {
	switch f {
	case questFilterLive:
		return questFilterAll
	case questFilterAll:
		return questFilterDungeon
	default:
		return questFilterLive
	}
}

// questListItem is one browser row. AbsPath is the on-disk quest directory used
// for go/copy; RelPath is the campaign-relative display path.
type questListItem struct {
	Name    string
	Status  quest.Status
	RelPath string
	AbsPath string
}

type questListModel struct {
	all          []questListItem
	visible      []questListItem
	cursor       int
	filter       questListFilter
	statusFilter []quest.Status

	status    string
	statusErr bool

	// gotoEnabled mirrors camp list: "go" only cds when the shell wrapper
	// passed --path-output. Without it, enter/g explains how to enable it
	// rather than silently doing nothing.
	gotoEnabled bool
	gotoPath    string

	width    int
	height   int
	quitting bool
}

// questListTUIRequested reports whether the user asked for the browser.
// --json never does; -i forces the request; a shaping flag prints the table
// instead. -i on a non-TTY still requests it; questListShouldRunTUI is what
// actually starts the browser.
func questListTUIRequested(cmd *cobra.Command, isTTY bool) bool {
	if questListJSON {
		return false
	}
	if interactive, _ := cmd.Flags().GetBool("interactive"); interactive {
		return true
	}
	for _, f := range []string{"status", "all", "dungeon"} {
		if cmd.Flags().Changed(f) {
			return false
		}
	}
	return isTTY
}

// questListShouldRunTUI is true only when the interactive browser will start.
func questListShouldRunTUI(cmd *cobra.Command, isTTY bool) bool {
	return questListTUIRequested(cmd, isTTY) && isTTY
}

// questListLoadOptions returns ListOptions for the table or JSON path, or an
// unfiltered --all set when the browser will run (so f can cycle filters).
func questListLoadOptions(runTUI bool, statuses []quest.Status) *quest.ListOptions {
	if runTUI {
		return &quest.ListOptions{All: true}
	}
	return &quest.ListOptions{
		Statuses: statuses,
		All:      questListAll,
		Dungeon:  questListDungeon,
	}
}

func initialQuestFilter() questListFilter {
	if questListDungeon {
		return questFilterDungeon
	}
	if questListAll || len(questListStatuses) > 0 {
		return questFilterAll
	}
	return questFilterLive
}

func runQuestListTUI(cmd *cobra.Command, qctx *questCommandContext, quests []*quest.Quest, statuses []quest.Status) error {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return outputQuestTable(qctx, quests)
	}
	pathOutput, _ := cmd.Flags().GetString("path-output")
	model := newQuestListModel(itemsFromQuests(qctx, quests), statuses)
	model.gotoEnabled = pathOutput != ""
	model.filter = initialQuestFilter()
	model.rebuildVisible()

	prog := tea.NewProgram(model, tea.WithContext(cmd.Context()), tea.WithAltScreen())
	final, err := prog.Run()
	if err != nil {
		return camperrors.Wrap(err, "running quest browser")
	}
	return writeGotoSelection(final, pathOutput)
}

func writeGotoSelection(final tea.Model, pathOutput string) error {
	m, ok := final.(questListModel)
	if !ok || pathOutput == "" || m.gotoPath == "" {
		return nil
	}
	return os.WriteFile(pathOutput, []byte(m.gotoPath), 0o600)
}

func itemsFromQuests(qctx *questCommandContext, quests []*quest.Quest) []questListItem {
	items := make([]questListItem, 0, len(quests))
	for _, q := range quests {
		if q == nil {
			continue
		}
		// q.Path is the quest.yaml FILE (see quest.QuestPathForDir); go/copy need
		// the containing directory, not the manifest file, or "cd" fails with
		// "not a directory".
		abs := q.Path
		if abs != "" && !filepath.IsAbs(abs) {
			abs = filepath.Join(qctx.campaignRoot, abs)
		}
		if abs != "" {
			abs = filepath.Dir(abs)
		}
		items = append(items, questListItem{
			Name:    q.Name,
			Status:  q.Status,
			RelPath: quest.RelativePath(qctx.campaignRoot, q.Path),
			AbsPath: abs,
		})
	}
	return items
}

func newQuestListModel(items []questListItem, statuses []quest.Status) questListModel {
	m := questListModel{all: items, statusFilter: statuses, filter: questFilterLive}
	m.rebuildVisible()
	return m
}

func (m *questListModel) rebuildVisible() {
	out := make([]questListItem, 0, len(m.all))
	for _, e := range m.all {
		if len(m.statusFilter) > 0 && !slices.Contains(m.statusFilter, e.Status) {
			continue
		}
		switch m.filter {
		case questFilterLive:
			if e.Status.InDungeon() {
				continue
			}
		case questFilterDungeon:
			if !e.Status.InDungeon() {
				continue
			}
		}
		out = append(out, e)
	}
	m.visible = out
	m.cursor = ui.ClampIdx(m.cursor, len(m.visible))
}

func (m questListModel) Init() tea.Cmd { return nil }

func (m *questListModel) copyPath() error {
	return ui.WriteClipboard(m.visible[m.cursor].AbsPath)
}

func (m *questListModel) setStatus(s string, isErr bool) {
	m.status = s
	m.statusErr = isErr
}

func (m questListModel) distinctStatuses() int {
	seen := map[quest.Status]bool{}
	for _, e := range m.visible {
		seen[e.Status] = true
	}
	return len(seen)
}
