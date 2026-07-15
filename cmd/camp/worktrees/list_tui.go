package worktrees

import (
	"os"
	"sort"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

// stdoutIsTTY reports whether stdout is an interactive terminal. Overridden in
// tests so dispatch does not depend on the runner's terminal.
var stdoutIsTTY = func() bool { return term.IsTerminal(int(os.Stdout.Fd())) }

// wtListModel is the interactive worktree browser. It mirrors the campaign
// browser (camp list): a flat, cursor-navigable list grouped by project, with
// copy-path, go (cd via shell integration), and a stale-only filter, degrading
// to a scrolling window when the list outgrows the terminal.
type wtListModel struct {
	all     []WorktreeListItem
	visible []WorktreeListItem
	cursor  int

	staleOnly bool

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

// worktreesListTUIRequested decides whether bare `camp worktrees list` opens the
// browser. --json never does; -i forces it; a shaping flag (--project/--stale)
// prints the table instead; otherwise an interactive terminal opens it.
func worktreesListTUIRequested(cmd *cobra.Command, isTTY bool) bool {
	if listJSON {
		return false
	}
	if interactive, _ := cmd.Flags().GetBool("interactive"); interactive {
		return true
	}
	for _, f := range []string{"project", "stale"} {
		if cmd.Flags().Changed(f) {
			return false
		}
	}
	return isTTY
}

// runWorktreesListTUI loads every worktree and runs the interactive browser.
// -i honors --project/--stale as the initial view (the way camp list -i honors
// --org); a non-terminal stdout falls back to the table.
func runWorktreesListTUI(cmd *cobra.Command, result *WorktreeListResult) error {
	ctx := cmd.Context()
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		if listJSON {
			return outputListJSON(result)
		}
		return outputListTable(result)
	}
	pathOutput, _ := cmd.Flags().GetString("path-output")
	model := newWtListModel(result.Worktrees)
	model.gotoEnabled = pathOutput != ""
	model.staleOnly = listStale
	model.rebuildVisible()

	prog := tea.NewProgram(model, tea.WithContext(ctx), tea.WithAltScreen())
	final, err := prog.Run()
	if err != nil {
		return camperrors.Wrap(err, "running worktree browser")
	}
	return writeGotoSelection(final, pathOutput)
}

// writeGotoSelection persists the worktree path the user chose, for the shell
// function to cd into. No-op unless a path-output file was supplied and a
// selection was made.
func writeGotoSelection(final tea.Model, pathOutput string) error {
	m, ok := final.(wtListModel)
	if !ok || pathOutput == "" || m.gotoPath == "" {
		return nil
	}
	return os.WriteFile(pathOutput, []byte(m.gotoPath), 0o600)
}

// newWtListModel builds a browser over the given worktrees, sorted into a
// stable grouped order (by project, then name) so project headers render once.
func newWtListModel(items []WorktreeListItem) wtListModel {
	all := make([]WorktreeListItem, len(items))
	copy(all, items)
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Project != all[j].Project {
			return all[i].Project < all[j].Project
		}
		return all[i].Name < all[j].Name
	})
	m := wtListModel{all: all}
	m.rebuildVisible()
	return m
}

func (m *wtListModel) rebuildVisible() {
	out := make([]WorktreeListItem, 0, len(m.all))
	for _, e := range m.all {
		if m.staleOnly && !e.Stale {
			continue
		}
		out = append(out, e)
	}
	m.visible = out
	m.cursor = ui.ClampIdx(m.cursor, len(m.visible))
}

func (m wtListModel) Init() tea.Cmd { return nil }

func (m *wtListModel) copyPath() error {
	return ui.WriteClipboard(m.visible[m.cursor].Path)
}

func (m *wtListModel) setStatus(s string, isErr bool) {
	m.status = s
	m.statusErr = isErr
}

func (m wtListModel) distinctProjects() int {
	seen := map[string]bool{}
	for _, e := range m.visible {
		seen[e.Project] = true
	}
	return len(seen)
}
