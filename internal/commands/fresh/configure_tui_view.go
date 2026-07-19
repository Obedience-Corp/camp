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
	freshDisabled      = lipgloss.NewStyle().Foreground(freshTUIPal.TextMuted)
	freshUnset         = lipgloss.NewStyle().Foreground(freshTUIPal.Warning)
	freshSection       = lipgloss.NewStyle().Foreground(freshTUIPal.Accent).Bold(true)
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
	title := freshTUITitleStyle.Render("Fresh workflow")
	meta := freshMuted.Render("configure what camp fresh does after a merge")
	line := title + "  " + meta
	return ui.ClampWidth(line, width)
}

func (m *followUpTUIModel) renderScopesPane(lay freshTUILayout) string {
	width := lay.leftWidth
	inner := max(width-4, 1)
	lines := []string{
		ui.ClampWidth(freshTUITitleStyle.Render("Scopes"), inner),
		freshMuted.Render("select the scope to configure"),
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

// freshPaneRow is one rendered line of the workflow pane. A row is either a
// section header or a step; headers carry stepIdx -1 and are skipped by the
// cursor, so j/k still walks the sequence and never lands on a label.
type freshPaneRow struct {
	header  string
	hint    string
	stepIdx int
}

// workflowRows interleaves section headers with the steps they cover. The
// sections partition the sequence by kind in execution order, which is what
// lets the pane say which verbs apply where instead of advertising all of them
// in the footer and rejecting most of them.
func (m *followUpTUIModel) workflowRows(steps []freshWorkflowStep) []freshPaneRow {
	rows := make([]freshPaneRow, 0, len(steps)+4)
	emitted := map[freshStepKind]bool{}
	followUps := 0
	for _, step := range steps {
		if step.Kind == freshStepFollowUp {
			followUps++
		}
	}

	for i, step := range steps {
		if !emitted[step.Kind] {
			emitted[step.Kind] = true
			if header, hint := m.sectionHeader(step.Kind, followUps); header != "" {
				rows = append(rows, freshPaneRow{header: header, hint: hint, stepIdx: -1})
			}
		}
		rows = append(rows, freshPaneRow{stepIdx: i})
	}

	// The follow-up section is the only one that can be empty, and an empty one
	// still has to appear: it is where the a key applies, and a user who cannot
	// see the section has no reason to believe adding a step is possible.
	if followUps == 0 {
		header, hint := m.sectionHeader(freshStepFollowUp, 0)
		rows = append(rows[:len(rows)-1],
			freshPaneRow{header: header, hint: hint, stepIdx: -1},
			rows[len(rows)-1],
		)
	}
	return rows
}

func (m *followUpTUIModel) sectionHeader(kind freshStepKind, followUps int) (string, string) {
	switch kind {
	case freshStepFixed:
		return "Sync", "always runs · not configurable"
	case freshStepSetting:
		if m.inProjectScope() {
			return "Settings", "enter: change · some keys are campaign-wide"
		}
		return "Settings", "enter: change"
	case freshStepFollowUp:
		if followUps == 0 {
			return "Follow-ups", "none yet · a: add one"
		}
		hint := "a: add · e: edit · d: delete · K/J: reorder"
		if m.scopeInheritsGlobal() {
			hint = "inherited from global · editing makes a project copy"
		}
		return "Follow-ups", hint
	default:
		return "", ""
	}
}

func (m *followUpTUIModel) renderWorkflowPane(lay freshTUILayout) string {
	width := lay.rightWidth
	inner := max(width-4, 1)
	scope := workflowScopeLabel(scopeProjectName(m.selectedScope()))
	headerDetail := "the ordered sequence camp fresh runs for this scope"
	if step, ok := m.selectedStep(); ok && m.pane == followUpWorkflowPane {
		headerDetail = "Selected · " + step.Detail
	}
	lines := []string{
		ui.ClampWidth(freshTUITitleStyle.Render("Workflow · "+scope), inner),
		freshMuted.Render(ui.Truncate(headerDetail, max(inner-2, 1))),
	}

	steps := m.workflowSteps()
	paneRows := m.workflowRows(steps)
	cursorRow := 0
	for i, row := range paneRows {
		if row.stepIdx == m.stepCursor {
			cursorRow = i
			break
		}
	}

	rows := max(lay.bodyRows-2, 1)
	start, end := ui.WindowRange(cursorRow, len(paneRows), rows)
	for i := start; i < end; i++ {
		row := paneRows[i]
		if row.stepIdx < 0 {
			lines = append(lines, m.sectionRow(row, inner))
			continue
		}
		lines = append(lines, m.workflowRow(row.stepIdx, steps[row.stepIdx], inner))
	}
	return m.finishPane(lines, inner, lay.bodyRows, m.pane == followUpWorkflowPane)
}

func (m *followUpTUIModel) sectionRow(row freshPaneRow, width int) string {
	// Hints are dropped before the section name is truncated: a half-spelled
	// heading costs the reader the grouping itself, while a missing hint only
	// costs a reminder the help overlay repeats.
	label := freshSection.Render(row.header)
	if row.hint != "" {
		candidate := row.header + "  " + row.hint
		if lipgloss.Width(candidate) <= max(width-6, 1) {
			label = freshSection.Render(row.header) + freshMuted.Render("  "+row.hint)
		}
	}
	return ui.ClampWidth(label, max(width-freshPaneTextInset, 1))
}

// stepGlyph distinguishes the four step states. "Off" and "not configured"
// looked identical before, which made a setting nobody had chosen yet read as
// a deliberate decision.
func stepGlyph(step freshWorkflowStep) string {
	if step.Enabled {
		return "✓"
	}
	switch step.State {
	case freshStateUnset:
		return "○"
	case freshStateBlocked:
		return "◌"
	default:
		return "·"
	}
}

func (m *followUpTUIModel) workflowRow(index int, step freshWorkflowStep, width int) string {
	selected := index == m.stepCursor
	focused := selected && m.pane == followUpWorkflowPane
	title := fmt.Sprintf("%d. %s", index+1, step.Title)
	row := "  " + ui.CursorGlyph(focused) + stepGlyph(step) + " " + title
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
	switch {
	case step.State == freshStateUnset:
		// An unconfigured setting is the one row worth drawing attention to:
		// it is a decision nobody has made, not a step that is working.
		return freshUnset.Render(row)
	case !step.Enabled:
		return freshDisabled.Render(row)
	case step.Kind == freshStepFixed || step.Kind == freshStepDone:
		return freshMuted.Render(row)
	default:
		return freshPrimary.Render(row)
	}
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

// footer advertises the verbs that apply to the row under the cursor. The old
// footer listed every verb unconditionally, so on eight rows out of nine it
// named keys that would be refused.
func (m *followUpTUIModel) footer(width int) string {
	full := "j/k: select · h/l: switch pane · a: add follow-up · r: reload · ?: help · q: quit"
	mid := "j/k select · h/l pane · a add · ? help · q quit"
	short := "j/k · h/l · a · ? · q"

	if m.pane == followUpWorkflowPane {
		if step, ok := m.selectedStep(); ok {
			switch {
			case step.Kind == freshStepFollowUp:
				full = "enter/e: edit · d: delete · K/J: reorder · a: add · j/k: select · h/l: pane · ?: help · q: quit"
				mid = "enter edit · d delete · K/J reorder · a add · ? help · q quit"
				short = "enter · d · K/J · a · q"
			case step.Kind == freshStepSetting && step.Configurable(m.inProjectScope()):
				full = "enter: change this setting · a: add follow-up · j/k: select · h/l: pane · ?: help · q: quit"
				mid = "enter change · a add · j/k select · ? help · q quit"
				short = "enter · a · j/k · q"
			}
		}
	}
	return freshTUIHelpStyle.Render(ui.CollapseHelp(width, full, mid, short, "q: quit"))
}

func (m *followUpTUIModel) statusLine() string {
	if m.status == "" {
		return ""
	}
	switch m.statusLevel {
	case statusError:
		return freshTUIErrorStyle.Render("✗ " + m.status)
	case statusNotice:
		return freshMuted.Render("· " + m.status)
	default:
		return freshTUIOKStyle.Render("✓ " + m.status)
	}
}

func (m *followUpTUIModel) overlayView() string {
	lay := m.layout()
	var body []string
	switch m.overlay {
	case followUpHelpOverlay:
		body = []string{
			freshTUITitleStyle.Render("Fresh workflow configuration"),
			"",
			freshPrimary.Render("The right pane is the actual camp fresh sequence,"),
			freshPrimary.Render("grouped by what you can change about each step."),
			"",
			freshSection.Render("Sync") + freshMuted.Render("  always runs; nothing to configure"),
			freshSection.Render("Settings") + freshMuted.Render("  fresh.yaml keys · enter to change"),
			freshSection.Render("Follow-ups") + freshMuted.Render("  your commands · add, edit, reorder"),
			"",
			freshPrimary.Render("enter  change the selected setting or follow-up"),
			freshPrimary.Render("a  add a follow-up after the core sync steps"),
			freshPrimary.Render("K/J  move the selected follow-up earlier/later"),
			freshPrimary.Render("d  delete the selected follow-up"),
			freshPrimary.Render("r  reload fresh.yaml from disk"),
			freshPrimary.Render("h/l or tab  move between scopes and workflow"),
			"",
			freshMuted.Render("prune and prune_remote are campaign-wide: change"),
			freshMuted.Render("them under Global defaults, not under a project."),
			"",
			freshTUIHelpStyle.Render("esc or ?  close help"),
		}
	case followUpSettingOverlay:
		body = m.settingOverlayBody()
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
	// Place paints its own filler, so the dimmed background has to be handed to
	// Place rather than wrapped around it. A background set on the surrounding
	// style does not survive the resets the box emits, which left the padding to
	// the right of the box on the box's own rows unstyled: a black notch down
	// one side of an otherwise dimmed screen.
	canvas := lipgloss.Place(lay.width, lay.height, lipgloss.Center, lipgloss.Center, box,
		lipgloss.WithWhitespaceBackground(freshTUIPal.BgOverlay))
	return ui.FitFullscreenView(canvas, lay.height)
}

// settingOverlayBody renders the editor for one fresh.yaml key. It names the
// key itself rather than only the step title, so the choice made here is
// traceable to the line it writes in fresh.yaml.
func (m *followUpTUIModel) settingOverlayBody() []string {
	step := m.settingStep
	scope := workflowScopeLabel(scopeProjectName(m.selectedScope()))
	body := []string{
		freshTUITitleStyle.Render(step.Title + " · " + scope),
		freshMuted.Render("fresh.yaml key: " + settingTitle(step.Setting)),
	}
	if step.GlobalOnly {
		body = append(body, freshMuted.Render("This key applies to every project in the campaign."))
	}
	body = append(body, "")

	for i, option := range m.settingOptions {
		prefix := ui.CursorGlyph(i == m.settingChoice)
		line := prefix + option.label
		if i == m.settingChoice {
			body = append(body, freshSelected.Render(line))
			continue
		}
		body = append(body, freshPrimary.Render(line))
	}

	if step.Setting == freshSettingBranch {
		body = append(body, "", freshPrimary.Render("Branch name"), m.settingInput.View())
		if m.selectedSettingAction() != freshSettingCustomBranch {
			body = append(body, freshMuted.Render("used only with \"create a branch\""))
		}
	}
	if m.settingError != "" {
		body = append(body, freshTUIErrorStyle.Render("✗ "+m.settingError))
	}
	return append(body, "", freshTUIHelpStyle.Render("up/down choose · enter save · esc cancel"))
}

func failureBehaviorLabel(continueOnError bool) string {
	if continueOnError {
		return "Continue to later steps if this command fails"
	}
	return "Stop fresh if this command fails"
}
