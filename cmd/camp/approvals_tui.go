package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/Obedience-Corp/camp/internal/approvals"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/pkg/approvalkit"
)

// approvalsMode is what the browser is doing with the keyboard.
type approvalsMode int

const (
	approvalsBrowse approvalsMode = iota
	// approvalsConfirm shows the exact command about to run and waits for y/n.
	approvalsConfirm
	// approvalsReason collects the reason for a rejection.
	approvalsReason
)

func runApprovalsTUI(cmd *cobra.Command, snap approvalsSnapshot) error {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		renderApprovalsHuman(cmd, snap)
		return nil
	}
	model := newApprovalsTUIModel(cmd.Context(), snap)
	prog := tea.NewProgram(model, tea.WithContext(cmd.Context()), tea.WithAltScreen())
	if _, err := prog.Run(); err != nil {
		return camperrors.Wrap(err, "running approvals browser")
	}
	return nil
}

type approvalsTUIModel struct {
	ctx    context.Context
	snap   approvalsSnapshot
	cursor int
	// scroll is the first visible line of the selected request.
	scroll int
	mode   approvalsMode

	// pending is the decision being confirmed, fixed when the confirm opens so
	// a refresh cannot change which request the confirmed command acts on.
	pendingRow      approvalRow
	pendingDecision approvals.Decision
	pendingArgv     []string
	reason          string

	status    string
	statusErr bool
	width     int
	height    int
	busy      bool
	quitting  bool
}

func newApprovalsTUIModel(ctx context.Context, snap approvalsSnapshot) approvalsTUIModel {
	m := approvalsTUIModel{ctx: ctx, snap: snap}
	m.clampCursor()
	return m
}

func (m *approvalsTUIModel) clampCursor() {
	if m.cursor >= len(m.snap.rows) {
		m.cursor = len(m.snap.rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// clampScroll keeps the request scrolled to a line that exists.
func (m *approvalsTUIModel) clampScroll() {
	visible, total := m.requestViewport()
	m.scroll = max(min(m.scroll, total-visible), 0)
}

func (m approvalsTUIModel) selected() (approvalRow, bool) {
	if m.cursor < 0 || m.cursor >= len(m.snap.rows) {
		return approvalRow{}, false
	}
	return m.snap.rows[m.cursor], true
}

func (m *approvalsTUIModel) setStatus(s string, isErr bool) { m.status, m.statusErr = s, isErr }

func (m approvalsTUIModel) Init() tea.Cmd { return nil }

type approvalsRefreshedMsg struct {
	snap approvalsSnapshot
	note string
	err  error
}

// approvalsRanMsg arrives when a handed-off command returns the terminal.
type approvalsRanMsg struct {
	what string
	err  error
}

// refreshApprovalsCmd commits any new approval events (the decision that was
// just made is one) and re-reads the inbox.
func refreshApprovalsCmd(ctx context.Context, campRoot string) tea.Cmd {
	return func() tea.Msg {
		note := commitApprovals(ctx, campRoot)
		snap, err := loadApprovals(ctx, campRoot)
		return approvalsRefreshedMsg{snap: snap, note: note, err: err}
	}
}

func (m approvalsTUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.clampScroll()
		return m, nil
	case approvalsRefreshedMsg:
		m.busy = false
		if msg.err != nil {
			m.setStatus("refresh failed: "+msg.err.Error(), true)
			return m, nil
		}
		m.snap = msg.snap
		m.clampCursor()
		m.clampScroll()
		if msg.note != "" && m.status == "" {
			m.setStatus(msg.note, strings.HasPrefix(msg.note, "Warning"))
		}
		return m, nil
	case approvalsRanMsg:
		if msg.err != nil {
			m.setStatus(msg.what+" did not complete: "+msg.err.Error(), true)
		} else {
			m.setStatus(msg.what+" finished", false)
		}
		m.busy = true
		return m, refreshApprovalsCmd(m.ctx, m.snap.campRoot)
	case tea.KeyMsg:
		switch m.mode {
		case approvalsConfirm:
			return m.updateConfirm(msg)
		case approvalsReason:
			return m.updateReason(msg)
		default:
			return m.updateBrowse(msg)
		}
	}
	return m, nil
}

func (m approvalsTUIModel) updateBrowse(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "ctrl+c", "q", "esc":
		m.quitting = true
		return m, tea.Quit
	}
	if m.busy {
		return m, nil
	}
	// A status line describes the last key only.
	m.setStatus("", false)
	switch key.String() {
	case "up", "k":
		m.cursor--
		m.clampCursor()
		m.scroll = 0
	case "down", "j":
		m.cursor++
		m.clampCursor()
		m.scroll = 0
	case "pgdown", "ctrl+d", "ctrl+f":
		visible, _ := m.requestViewport()
		m.scroll += max(visible-1, 1)
		m.clampScroll()
	case "pgup", "ctrl+u", "ctrl+b":
		visible, _ := m.requestViewport()
		m.scroll -= max(visible-1, 1)
		m.clampScroll()
	case "r":
		m.busy = true
		return m, refreshApprovalsCmd(m.ctx, m.snap.campRoot)
	case "a":
		return m.beginDecision(approvals.Approve)
	case "x":
		row, ok := m.selected()
		if !ok {
			return m, nil
		}
		if !row.ready.Actionable {
			m.setStatus("not actionable here: "+row.ready.Reason, true)
			return m, nil
		}
		m.pendingRow, m.reason, m.mode = row, "", approvalsReason
	case "o":
		return m.openDocument()
	}
	return m, nil
}

// beginDecision fixes the command for the selected request and opens the
// confirm. Nothing runs until the user confirms what they are shown.
func (m approvalsTUIModel) beginDecision(decision approvals.Decision) (tea.Model, tea.Cmd) {
	row, ok := m.selected()
	if m.mode == approvalsReason {
		row, ok = m.pendingRow, true
	}
	if !ok {
		return m, nil
	}
	if !row.ready.Actionable {
		m.mode = approvalsBrowse
		m.setStatus("not actionable here: "+row.ready.Reason, true)
		return m, nil
	}
	argv, err := approvals.Command(row.Pending, decision, m.reason)
	if err != nil {
		m.mode = approvalsBrowse
		m.setStatus(err.Error(), true)
		return m, nil
	}
	m.pendingRow, m.pendingDecision, m.pendingArgv = row, decision, argv
	m.mode = approvalsConfirm
	return m, nil
}

func (m approvalsTUIModel) updateReason(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.Type {
	case tea.KeyCtrlC, tea.KeyEsc:
		m.mode, m.reason = approvalsBrowse, ""
		return m, nil
	case tea.KeyEnter:
		if strings.TrimSpace(m.reason) == "" {
			return m, nil
		}
		return m.beginDecision(approvals.Reject)
	case tea.KeyBackspace:
		if r := []rune(m.reason); len(r) > 0 {
			m.reason = string(r[:len(r)-1])
		}
	case tea.KeySpace:
		m.reason += " "
	case tea.KeyRunes:
		m.reason += string(key.Runes)
	}
	return m, nil
}

func (m approvalsTUIModel) updateConfirm(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "y", "Y":
		m.mode = approvalsBrowse
		// The directory was resolved when the list loaded. Resolve it again
		// now and require the same answer, so a path swapped for a symlink in
		// between cannot send the command somewhere else.
		dir, err := approvalkit.ResolveCwd(m.snap.campRoot, m.pendingRow.Cwd)
		if err != nil || dir != m.pendingRow.ready.Dir {
			m.setStatus("the source directory changed since the list loaded; press r to refresh", true)
			return m, nil
		}
		// The source's own command takes the terminal: it is the only thing
		// that can record the decision, and for an approval it asks the person
		// to type a confirmation itself.
		c := exec.CommandContext(m.ctx, m.pendingArgv[0], m.pendingArgv[1:]...)
		c.Dir = m.pendingRow.ready.Dir
		what := string(m.pendingDecision) + " of " + m.pendingRow.ID
		return m, tea.ExecProcess(c, func(err error) tea.Msg { return approvalsRanMsg{what: what, err: err} })
	case "n", "N", "esc", "ctrl+c", "q":
		m.mode, m.reason = approvalsBrowse, ""
		m.setStatus("cancelled", false)
	}
	return m, nil
}

// openDocument shows the source document in the user's pager or editor.
func (m approvalsTUIModel) openDocument() (tea.Model, tea.Cmd) {
	row, ok := m.selected()
	if !ok {
		return m, nil
	}
	if row.Doc == "" {
		m.setStatus("this request names no source document", true)
		return m, nil
	}
	// The file itself is resolved, not just its directory: a document that is
	// a symlink to somewhere outside the camp is refused.
	path, err := approvalkit.ResolveFile(m.snap.campRoot, row.Doc)
	if err != nil {
		m.setStatus("the source document is not available on this machine", true)
		return m, nil
	}
	viewer := strings.TrimSpace(os.Getenv("PAGER"))
	if viewer == "" {
		viewer = "less"
	}
	parts := strings.Fields(viewer)
	c := exec.CommandContext(m.ctx, parts[0], append(parts[1:], path)...)
	return m, tea.ExecProcess(c, func(err error) tea.Msg {
		return approvalsRanMsg{what: "viewing " + filepath.Base(path), err: err}
	})
}

// shellQuoteArgv renders a command for display so the user sees exactly what
// will run, with arguments that contain spaces quoted. It is for reading only:
// the command is executed from the argument list, never through a shell.
func shellQuoteArgv(argv []string) string {
	parts := make([]string, len(argv))
	for i, arg := range argv {
		if arg == "" || strings.ContainsAny(arg, " \t\n\"'\\$`") {
			parts[i] = "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
			continue
		}
		parts[i] = arg
	}
	return strings.Join(parts, " ")
}
