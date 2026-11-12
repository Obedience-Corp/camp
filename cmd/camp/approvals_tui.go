package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/Obedience-Corp/camp/internal/approvals"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jobs"
	"github.com/Obedience-Corp/camp/internal/ui"
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
		return m, nil
	case approvalsRefreshedMsg:
		m.busy = false
		if msg.err != nil {
			m.setStatus("refresh failed: "+msg.err.Error(), true)
			return m, nil
		}
		m.snap = msg.snap
		m.clampCursor()
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
	switch key.String() {
	case "up", "k":
		m.cursor--
		m.clampCursor()
		m.setStatus("", false)
	case "down", "j":
		m.cursor++
		m.clampCursor()
		m.setStatus("", false)
	case "r":
		m.busy = true
		m.setStatus("", false)
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
	if row.Doc == "" || !approvalkit.IsCampRelative(row.Doc) {
		m.setStatus("this request names no source document", true)
		return m, nil
	}
	dir, err := approvalkit.ResolveCwd(m.snap.campRoot, filepath.ToSlash(filepath.Dir(row.Doc)))
	if err != nil {
		m.setStatus("the source document is not on this machine", true)
		return m, nil
	}
	path := filepath.Join(dir, filepath.Base(row.Doc))
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

func (m approvalsTUIModel) contentWidth() int {
	if m.width <= 0 {
		return 100
	}
	return max(m.width-2, 40)
}

func (m approvalsTUIModel) View() string {
	if m.quitting {
		return ""
	}
	w := m.contentWidth()
	var lines []string
	switch m.mode {
	case approvalsConfirm:
		lines = m.confirmLines(w)
	case approvalsReason:
		lines = m.reasonLines(w)
	default:
		lines = m.browseLines(w)
	}
	budget := 0
	if m.height > 0 {
		budget = m.height
	}
	return ui.FitFullscreenView(strings.Join(ui.CapFrame(lines, w, budget), "\n"), m.height)
}

func (m approvalsTUIModel) browseLines(w int) []string {
	now := time.Now()
	summary := ui.CountLabel(len(m.snap.rows), "approval", "approvals") + " waiting on you"
	if m.busy {
		summary += "  .  working…"
	}
	lines := []string{jobsHeaderStyle.Render("camp approvals") + "  " + jobsMutedStyle.Render(summary)}
	for _, note := range approvalsNotices(m.snap) {
		lines = append(lines, jobsStallStyle.Render(ui.Truncate(note, w)))
	}
	lines = append(lines, "")

	if len(m.snap.rows) == 0 {
		lines = append(lines, jobsMutedStyle.Render("Nothing is waiting on your approval."))
	}
	for i, row := range m.snap.rows {
		marker := "  "
		style := jobsPrimary
		if i == m.cursor {
			marker, style = "> ", jobsSelStyle
		}
		if !row.ready.Actionable {
			style = jobsMutedStyle
			if i == m.cursor {
				style = jobsStallStyle
			}
		}
		line := fmt.Sprintf("%s%-12s %-9s %6s  %s", marker, row.ID, approvalSourceLabel(row.SourceKind),
			jobs.ShortDuration(approvalAge(row.Pending, now)), row.Title)
		lines = append(lines, style.Render(ui.Truncate(line, w)))
	}

	if row, ok := m.selected(); ok {
		lines = append(lines, "", jobsMutedStyle.Render(strings.Repeat("─", min(w, 60))))
		lines = append(lines, approvalDetailLines(row, now, w)...)
	}

	lines = append(lines, "")
	if m.status != "" {
		style := jobsMutedStyle
		if m.statusErr {
			style = jobsFailedStyle
		}
		lines = append(lines, style.Render(ui.Truncate(m.status, w)))
	}
	lines = append(lines, jobsMutedStyle.Render("↑/↓ move  .  a approve  .  x reject  .  o open document  .  r refresh  .  q quit"))
	return lines
}

func (m approvalsTUIModel) reasonLines(w int) []string {
	return []string{
		jobsHeaderStyle.Render("Reject " + m.pendingRow.ID),
		jobsPrimary.Render(ui.Truncate(m.pendingRow.Title, w)),
		"",
		"Reason (the agent reads this to revise the work):",
		"> " + m.reason + "█",
		"",
		jobsMutedStyle.Render("enter continue  .  esc cancel"),
	}
}

func (m approvalsTUIModel) confirmLines(w int) []string {
	verb := "Approve"
	note := "The command will ask you to confirm before anything is recorded."
	if m.pendingDecision == approvals.Reject {
		verb = "Reject"
		note = "The step stays blocked until the revised work is submitted again."
	}
	lines := []string{
		jobsHeaderStyle.Render(verb + " " + m.pendingRow.ID + "?"),
		jobsPrimary.Render(ui.Truncate(m.pendingRow.Title, w)),
		"",
		"camp will hand your terminal to this command:",
		"",
		"  " + jobsSelStyle.Render(shellQuoteArgv(m.pendingArgv)),
		"  " + jobsMutedStyle.Render("in "+m.pendingRow.ready.Dir),
		"",
	}
	lines = append(lines, wrapApprovalText(note, w)...)
	lines = append(lines, "", jobsMutedStyle.Render("y run it  .  n cancel"))
	return lines
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
