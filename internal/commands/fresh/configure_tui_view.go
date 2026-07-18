package fresh

import (
	"fmt"
	"strings"

	"github.com/Obedience-Corp/camp/internal/intent/tui"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
)

var freshTUIPal = theme.TUI()

// freshPaneTextInset and freshOverlayTextInset are the horizontal padding the
// pane and overlay styles add inside their borders, which lipgloss counts
// against the width given to Style.Width. Content has to be clamped to the
// width minus its inset or lipgloss wraps the line inside the box.
const (
	freshPaneTextInset    = 2
	freshOverlayTextInset = 4
)

var (
	freshTUITitleStyle = tui.TitleStyle
	freshTUIHelpStyle  = tui.HelpStyle
	freshTUIErrorStyle = tui.ErrorStyle
	freshTUIOKStyle    = tui.SuccessStyle
	freshPaneFocused   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(freshTUIPal.BorderFocus).Padding(0, 1)
	freshPaneBlurred   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(freshTUIPal.Border).Padding(0, 1)
	freshSelected      = lipgloss.NewStyle().Foreground(freshTUIPal.Accent).Bold(true)
	freshPrimary       = lipgloss.NewStyle().Foreground(freshTUIPal.TextPrimary)
	freshMuted         = lipgloss.NewStyle().Foreground(freshTUIPal.TextMuted)
	freshDisabled      = lipgloss.NewStyle().Foreground(freshTUIPal.Warning)
	freshOverlay       = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(freshTUIPal.BorderFocus).Background(freshTUIPal.BgOverlay).Padding(1, 2)
)

type freshTUILayout struct {
	width      int
	height     int
	dual       bool
	leftWidth  int
	rightWidth int
	bodyRows   int
	showFooter bool
}

func (m *followUpTUIModel) layout() freshTUILayout {
	w, h := m.width, m.height
	if w <= 0 {
		w = 100
	}
	if h <= 0 {
		h = 30
	}
	dual := w >= 78
	left := w
	right := w
	if dual {
		left = max(w/3, 28)
		right = max(w-left-2, 30)
	}
	// Leave room for the title, status, footer, and the pane borders. Keeping
	// this conservative prevents the footer from being evicted on short PTYs.
	bodyRows := max(h-8, 4)
	return freshTUILayout{
		width:      w,
		height:     h,
		dual:       dual,
		leftWidth:  left,
		rightWidth: right,
		bodyRows:   bodyRows,
		showFooter: h >= 8,
	}
}

func (m *followUpTUIModel) View() string {
	if m.quitting {
		return ""
	}
	if m.overlay != followUpNoOverlay {
		return m.overlayView()
	}

	lay := m.layout()
	lines := []string{m.topBar(lay.width)}
	if lay.dual {
		left := m.renderScopesPane(lay)
		right := m.renderWorkflowPane(lay)
		joined := lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
		lines = append(lines, strings.Split(joined, "\n")...)
	} else if m.pane == followUpScopesPane {
		lines = append(lines, strings.Split(m.renderScopesPane(lay), "\n")...)
	} else {
		lines = append(lines, strings.Split(m.renderWorkflowPane(lay), "\n")...)
	}
	if lay.showFooter {
		if status := m.statusLine(); status != "" {
			lines = append(lines, status)
		}
		lines = append(lines, m.footer(lay.width))
	}
	return m.frame(lines, lay.width, lay.height)
}

func (m *followUpTUIModel) topBar(width int) string {
	title := freshTUITitleStyle.Render("Fresh follow-ups")
	meta := freshMuted.Render("configure the sequence that runs after a successful fresh cycle")
	line := title + "  " + meta
	return ui.ClampWidth(line, width)
}

func (m *followUpTUIModel) renderScopesPane(lay freshTUILayout) string {
	width := lay.leftWidth
	inner := max(width-4, 1)
	lines := []string{
		ui.ClampWidth(freshTUITitleStyle.Render("Scopes"), inner),
		freshMuted.Render("select where a follow-up runs"),
	}
	rows := max(lay.bodyRows-2, 1)
	start, end := ui.WindowRange(m.scopeCursor, len(m.scopes), rows)
	for i := start; i < end; i++ {
		scope := m.scopes[i]
		selected := i == m.scopeCursor
		prefix := ui.CursorGlyph(selected && m.pane == followUpScopesPane)
		style := freshPrimary
		if selected {
			style = freshSelected
		}
		lines = append(lines, prefix+style.Render(scopeRowText(scope, max(inner-freshPaneTextInset-lipgloss.Width(prefix), 1))))
	}
	if len(m.scopes) == 0 {
		lines = append(lines, freshMuted.Render("no projects found"))
	}
	return m.finishPane(lines, inner, lay.bodyRows, m.pane == followUpScopesPane)
}

// scopeRowText fits a scope onto one line of the given width, shedding badges
// as it narrows. Badges are annotations, so they go before the project name is
// truncated: a half-spelled name is worse than a missing hint, and a wrapped
// one is worse than both because it costs the pane a row it does not have.
//
// The override badge outranks "here" when only one fits. Which project you are
// standing in is already obvious from the cursor and the opening status line,
// while whether a project has its own follow-up list is not visible anywhere
// else in this pane.
func scopeRowText(scope followUpScope, width int) string {
	here := ""
	if scope.current {
		here = "here"
	}
	override := ""
	if scope.override {
		override = fmt.Sprintf("override %d", scope.overrideCount)
	}

	for _, badges := range [][]string{{here, override}, {override}, {here}} {
		kept := make([]string, 0, len(badges))
		for _, badge := range badges {
			if badge != "" {
				kept = append(kept, badge)
			}
		}
		if len(kept) == 0 {
			continue
		}
		candidate := scope.name + " · " + strings.Join(kept, " · ")
		if lipgloss.Width(candidate) <= width {
			return candidate
		}
	}
	return ui.Truncate(scope.name, width)
}

func (m *followUpTUIModel) renderWorkflowPane(lay freshTUILayout) string {
	width := lay.rightWidth
	inner := max(width-4, 1)
	scope := workflowScopeLabel(scopeProjectName(m.selectedScope()))
	headerDetail := "Follow-ups run only after the sync steps succeed"
	if m.scopeInheritsGlobal() {
		headerDetail = "Inherits the global follow-ups · editing here makes a project copy"
	}
	steps := m.workflowSteps()
	if m.pane == followUpWorkflowPane && m.stepCursor >= 0 && m.stepCursor < len(steps) {
		headerDetail = "Selected · " + steps[m.stepCursor].Detail
	}
	lines := []string{
		ui.ClampWidth(freshTUITitleStyle.Render("Workflow · "+scope), inner),
		freshMuted.Render(ui.Truncate(headerDetail, max(inner-2, 1))),
	}
	rows := max(lay.bodyRows-2, 1)
	start, end := ui.WindowRange(m.stepCursor, len(steps), rows)
	for i := start; i < end; i++ {
		lines = append(lines, m.workflowRow(i, steps[i], inner))
	}
	return m.finishPane(lines, inner, lay.bodyRows, m.pane == followUpWorkflowPane)
}

func (m *followUpTUIModel) workflowRow(index int, step freshWorkflowStep, width int) string {
	selected := index == m.stepCursor
	focused := selected && m.pane == followUpWorkflowPane
	icon := "✓"
	if !step.Enabled {
		icon = "⚠"
	}
	title := fmt.Sprintf("%d. %s", index+1, step.Title)
	if !step.Enabled {
		title += " [off]"
	}
	row := ui.CursorGlyph(focused) + icon + " " + title
	if width > 44 {
		row += "  · " + step.Detail
	}
	// The pane style adds border and padding around the inner width. Reserve a
	// few cells here so a row remains one visual line instead of being wrapped
	// by lipgloss after truncation.
	row = ui.Truncate(row, max(width-6, 1))
	if selected {
		return freshSelected.Render(row)
	}
	if !step.Enabled {
		return freshDisabled.Render(row)
	}
	return freshPrimary.Render(row)
}

func (m *followUpTUIModel) finishPane(lines []string, width, rows int, focused bool) string {
	want := rows + 2
	for len(lines) < want {
		lines = append(lines, "")
	}
	if len(lines) > want {
		lines = lines[:want]
	}
	if m.layout().dual || m.width <= 0 {
		style := freshPaneBlurred
		if focused {
			style = freshPaneFocused
		}
		// lipgloss.Width sets the block width with the padding counted inside
		// it, so the text area is narrower than the number passed to Width.
		// Clamping to the block width instead lets a full-width line wrap onto
		// a second row, which pushes the pane past the row budget the layout
		// allotted and shoves the bottom border off the terminal.
		content := strings.Join(ui.ClampLines(lines, max(width-freshPaneTextInset, 1)), "\n")
		return style.Width(width).Render(content)
	}
	return strings.Join(ui.ClampLines(lines, width), "\n")
}

func (m *followUpTUIModel) frame(lines []string, width, height int) string {
	return ui.FitFullscreenView(strings.Join(ui.CapFrame(lines, width, height), "\n"), height)
}

func (m *followUpTUIModel) footer(width int) string {
	full := "j/k: select · K/J: move step · h/l: switch pane · a: add · e: edit · d: delete · r: reload · ?: help · q: quit"
	mid := "j/k select · K/J move · h/l pane · a add · e edit · d delete · ? help · q quit"
	short := "j/k · K/J · h/l · a/e/d · ? · q"
	return freshTUIHelpStyle.Render(ui.CollapseHelp(width, full, mid, short, "q: quit"))
}

func (m *followUpTUIModel) statusLine() string {
	if m.status == "" {
		return ""
	}
	if m.statusErr {
		return freshTUIErrorStyle.Render("✗ " + m.status)
	}
	return freshTUIOKStyle.Render("✓ " + m.status)
}

func (m *followUpTUIModel) overlayView() string {
	lay := m.layout()
	var body []string
	switch m.overlay {
	case followUpHelpOverlay:
		body = []string{
			freshTUITitleStyle.Render("Fresh follow-up configuration"),
			"",
			freshPrimary.Render("The right pane is the actual camp fresh sequence."),
			freshMuted.Render("Select a project on the left to see its resolved overrides."),
			"",
			freshPrimary.Render("a  add a follow-up after the core sync steps"),
			freshPrimary.Render("e  edit the selected follow-up"),
			freshPrimary.Render("K/J  move the selected follow-up earlier/later"),
			freshPrimary.Render("d  delete the selected follow-up"),
			freshPrimary.Render("r  reload fresh.yaml from disk"),
			freshPrimary.Render("h/l or tab  move between scopes and workflow"),
			"",
			freshTUIHelpStyle.Render("esc or ?  close help"),
		}
	case followUpDeleteOverlay:
		body = []string{
			freshTUITitleStyle.Render("Remove follow-up?"),
			"",
			freshPrimary.Render(fmt.Sprintf("Remove %q from %s?", m.pendingDelete, workflowScopeLabel(scopeProjectName(m.selectedScope())))),
			freshMuted.Render("The remaining workflow steps will stay in order."),
		}
		if note := m.forkNotice(); note != "" {
			body = append(body, freshMuted.Render(note))
		}
		body = append(body, "", freshTUIHelpStyle.Render("y/enter remove · n/esc cancel"))
	case followUpFormOverlay:
		title := "Add follow-up"
		if m.formEditName != "" {
			title = "Edit follow-up"
		}
		body = []string{
			freshTUITitleStyle.Render(title + " · " + workflowScopeLabel(scopeProjectName(m.selectedScope()))),
			freshMuted.Render("This command runs after checkout, pull, prune, and branch setup."),
		}
		if note := m.forkNotice(); note != "" {
			body = append(body, freshMuted.Render(note))
		}
		body = append(body,
			"",
			freshPrimary.Render("Name"),
			m.inputs[0].View(),
			freshPrimary.Render("Command"),
			m.inputs[1].View(),
			freshPrimary.Render("Working directory (optional)"),
			m.inputs[2].View(),
			freshPrimary.Render("Failure behavior"),
			freshSelected.Render(failureBehaviorLabel(m.formContinue)),
		)
		if m.formError != "" {
			body = append(body, freshTUIErrorStyle.Render("✗ "+m.formError))
		}
		body = append(body, "", freshTUIHelpStyle.Render("tab/shift+tab move · space toggle · enter save · esc cancel"))
	}

	boxWidth := min(max(lay.width-6, 30), 76)
	inner := max(boxWidth-6, 24)
	box := freshOverlay.Width(inner).Render(strings.Join(ui.ClampLines(body, max(inner-freshOverlayTextInset, 1)), "\n"))
	canvas := lipgloss.NewStyle().
		Width(lay.width).
		Height(lay.height).
		Background(freshTUIPal.BgOverlay).
		Render(lipgloss.Place(lay.width, lay.height, lipgloss.Center, lipgloss.Center, box))
	return ui.FitFullscreenView(canvas, lay.height)
}

func failureBehaviorLabel(continueOnError bool) string {
	if continueOnError {
		return "Continue to later steps if this command fails"
	}
	return "Stop fresh if this command fails"
}
