package project

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	tuistyles "github.com/Obedience-Corp/camp/internal/intent/tui"
	projectsvc "github.com/Obedience-Corp/camp/internal/project"
	projectrename "github.com/Obedience-Corp/camp/internal/project/rename"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
)

var renamePal = theme.TUI()

var (
	renameTitle     = tuistyles.TitleStyle
	renameHelp      = tuistyles.HelpStyle
	renameErr       = tuistyles.ErrorStyle
	renameOk        = tuistyles.SuccessStyle
	renameMuted     = lipgloss.NewStyle().Foreground(renamePal.TextMuted)
	renameValue     = lipgloss.NewStyle().Foreground(renamePal.TextPrimary)
	renameNameStyle = lipgloss.NewStyle().Foreground(renamePal.TextPrimary).Bold(true)
	renameAccent    = lipgloss.NewStyle().Foreground(renamePal.Accent).Bold(true)
	renamePillOn    = lipgloss.NewStyle().Foreground(lipgloss.Color("#0D0D11")).Background(renamePal.Accent).Bold(true)
	renamePillDone  = lipgloss.NewStyle().Foreground(renamePal.Success).Bold(true)
	renamePillOff   = lipgloss.NewStyle().Foreground(renamePal.TextMuted)
	renameRule      = lipgloss.NewStyle().Foreground(renamePal.Border)
	renameBox       = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(renamePal.BorderFocus).Padding(0, 1)
	renameBoxDone   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(renamePal.Success).Padding(0, 1)
)

const (
	renameBoxOverhead = 4
	renameMinBoxWidth = 36
	renameMinBoxH     = 14
)

type renameLayout struct {
	cw       int
	boxed    bool
	listRows int
}

func (m renameModel) layout() renameLayout {
	wKnown, hKnown := m.width > 0, m.height > 0
	l := renameLayout{
		boxed: (!wKnown || m.width >= renameMinBoxWidth) && (!hKnown || m.height >= renameMinBoxH),
	}
	if wKnown {
		l.cw = m.width
		if l.boxed {
			l.cw -= renameBoxOverhead
		}
		l.cw = max(l.cw, 1)
	}
	if hKnown {
		chrome := 8
		if l.boxed {
			chrome += 2
		}
		if m.errMsg != "" {
			chrome++
		}
		l.listRows = max(m.height-chrome, 3)
	}
	return l
}

func (m renameModel) View() string {
	if m.quitting && m.step != renameDone {
		return ""
	}
	lay := m.layout()
	lines := []string{m.titleLine(), "", m.stepsLine(), ""}
	lines = append(lines, m.bodyLines(lay)...)
	lines = append(lines, "", m.helpLine())
	if m.errMsg != "" {
		lines = append(lines, renameErr.Render(ui.Truncate(m.errMsg, max(lay.cw, 1))))
	}
	budget := 0
	if m.height > 0 {
		budget = m.height
		if lay.boxed {
			budget = max(budget-2, 1)
		}
	}
	content := strings.Join(ui.CapFrame(lines, lay.cw, budget), "\n")
	box := renameBox
	if m.step == renameDone && !m.flags.dryRun {
		box = renameBoxDone
	}
	if lay.boxed {
		return ui.FitFullscreenView(box.Render(content), m.height)
	}
	return ui.FitFullscreenView(content, m.height)
}

func (m renameModel) titleLine() string {
	switch m.step {
	case renameDone:
		if m.flags.dryRun {
			return renameTitle.Render("Dry run") + "  " + renameMuted.Render("nothing was written")
		}
		return ui.SuccessIcon() + "  " + renameOk.Render("Project renamed")
	case renameWorking:
		return renameTitle.Render("Renaming") + "  " + renameMuted.Render("keeping the checkout and its worktrees")
	default:
		label := ui.CountLabel(len(m.projects), "project", "projects")
		return renameTitle.Render("Rename project") + "  " + renameMuted.Render(label+"  ·  review before write")
	}
}

func (m renameModel) stepsLine() string {
	labels := []string{"Choose", "Name", "Review"}
	current := 0
	switch m.step {
	case renameName:
		current = 1
	case renameReview, renameWorking, renameDone:
		current = 2
	}
	parts := make([]string, len(labels))
	for i, label := range labels {
		text := fmt.Sprintf(" %d %s ", i+1, label)
		switch {
		case m.step == renameDone || i < current:
			parts[i] = renamePillDone.Render(text)
		case i == current:
			parts[i] = renamePillOn.Render(text)
		default:
			parts[i] = renamePillOff.Render(text)
		}
	}
	return "  " + strings.Join(parts, "  ")
}

func (m renameModel) bodyLines(lay renameLayout) []string {
	switch m.step {
	case renamePick:
		return m.pickLines(lay)
	case renameName:
		return m.nameLines(lay)
	case renameWorking:
		return m.workingLines()
	case renameDone:
		return m.doneLines(lay)
	default:
		return m.reviewLines(lay)
	}
}

func (m renameModel) pickLines(lay renameLayout) []string {
	if m.filtering {
		header := renameAccent.Render("  / " + m.query)
		if m.query == "" {
			header = renameMuted.Render("  / filter")
		}
		return append([]string{header, ""}, m.pickRows(lay)...)
	}
	return m.pickRows(lay)
}

func (m renameModel) pickRows(lay renameLayout) []string {
	if len(m.projects) == 0 {
		return []string{
			renameMuted.Render("  no managed projects"),
			renameMuted.Render("  camp project add <url>"),
			renameMuted.Render("  camp project link <path>"),
		}
	}
	if len(m.visible) == 0 {
		return []string{renameMuted.Render("  no projects match")}
	}
	total := len(m.visible)
	budget := lay.listRows
	if budget <= 0 || total <= budget {
		return m.renderPick(0, total, lay.cw)
	}
	start, end := ui.WindowRange(m.cursor, total, budget)
	out := m.renderPick(start, end, lay.cw)
	out = append(out, renameMuted.Render(fmt.Sprintf("  %d–%d of %d", start+1, end, total)))
	return out
}

func (m renameModel) renderPick(start, end, cw int) []string {
	out := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		out = append(out, m.pickRow(m.visible[i], i == m.cursor, cw))
	}
	return out
}

func (m renameModel) pickRow(p projectsvc.Project, selected bool, cw int) string {
	prefix := "  " + ui.CursorGlyph(selected)
	nameW := 16
	if cw > 0 {
		nameW = min(24, max(10, cw/4))
	}
	name := padRight(ui.Truncate(p.Name, nameW), nameW)
	nameStyled := renameNameStyle.Render(name)
	if selected {
		nameStyled = renameAccent.Render(name)
	}
	row := prefix + nameStyled
	if cw == 0 || cw >= 42 {
		row += "  " + typeBadge(p.Type, 12) + "  " + sourceBadge(p.Source, 10)
	}
	if p.Path != "" && (cw == 0 || cw >= 64) {
		row += "  " + renameMuted.Render(ui.Truncate(p.Path, max(cw-40, 8)))
	}
	return row
}

func (m renameModel) nameLines(lay renameLayout) []string {
	path := m.oldName
	if p, ok := m.projectByName(m.oldName); ok && p.Path != "" {
		path = p.Path
	}
	intro := "  " + renameMuted.Render("Renaming") + "  " + renameNameStyle.Render(m.oldName)
	if path != m.oldName {
		intro += "  " + renameMuted.Render(ui.Truncate(path, max(lay.cw-len(m.oldName)-16, 8)))
	}
	return []string{
		intro,
		"",
		renameMuted.Render("  New name"),
		"  " + m.nameInput.View(),
		"  " + renameAccent.Render(strings.Repeat("─", min(ruleWidth(lay.cw), 42))),
		"",
		renameMuted.Render("  Letters, digits, dot, underscore, or hyphen."),
	}
}

func (m renameModel) reviewLines(lay renameLayout) []string {
	lines := []string{m.heroLine(), ""}
	if m.planning || m.plan == nil {
		lines = append(lines, renameMuted.Render("  Reading the rename plan"))
		return lines
	}
	lines = append(lines, m.factLines(lay)...)
	if !m.flags.dryRun {
		lines = append(lines, "", renameMuted.Render("  Nothing is written until you confirm."))
	} else {
		lines = append(lines, "", renameAccent.Render("  Dry run. Nothing will be written."))
	}
	return lines
}

func (m renameModel) workingLines() []string {
	return []string{
		m.heroLine(),
		"",
		"  " + m.spin.View() + "  " + renameValue.Render("Applying the rename"),
	}
}

func (m renameModel) doneLines(lay renameLayout) []string {
	lines := []string{m.heroLine(), ""}
	lines = append(lines, m.factLines(lay)...)
	if m.flags.dryRun {
		lines = append(lines, "", renameMuted.Render("  No changes made."))
		return lines
	}
	if m.result != nil {
		for _, warning := range m.result.Warnings {
			lines = append(lines, "  "+ui.WarningIcon()+" "+renameErr.Render(ui.Truncate(warning, max(lay.cw-6, 8))))
		}
		if n := len(m.result.ResidualReferences); n > 0 && m.plan != nil {
			lines = append(lines, renameMuted.Render("  "+ui.CountLabel(n, "historical reference kept", "historical references kept")))
		}
	}
	if undo := projectRenameUndo(m.shownPlan()); undo != "" {
		lines = append(lines, "", renameMuted.Render("  Undo"), "  "+renameAccent.Render(m.clip(undo, max(lay.cw-4, 0))))
	}
	return lines
}

func (m renameModel) heroLine() string {
	plan := m.shownPlan()
	oldName, newName := m.oldName, m.newName
	if plan != nil {
		oldName, newName = plan.OldName, plan.NewName
	}
	return "  " + renameNameStyle.Render(oldName) + renameAccent.Render("  →  ") + renameAccent.Render(newName)
}

func (m renameModel) factLines(lay renameLayout) []string {
	plan := m.shownPlan()
	if plan == nil {
		return nil
	}
	valueW := 0
	if lay.cw > 18 {
		valueW = lay.cw - 16
	}
	remote := renameRemoteLine(plan)
	if m.editingRemote {
		remote = m.remoteInput.View()
	}
	facts := [][2]string{
		{"Kind", renameKindLabel(plan.Kind)},
		{"Path", plan.OldPath + " → " + plan.NewPath},
		{"Remote", remote},
		{"Worktrees", renameWorktreeSummary(plan.Worktrees)},
		{"Metadata", renameMetadataSummary(plan.Metadata)},
		{"Commit", m.commitFact()},
	}
	out := make([]string, 0, len(facts)+1)
	out = append(out, renameRule.Render(strings.Repeat("─", ruleWidth(lay.cw))))
	for _, fact := range facts {
		label := renameMuted.Render(fmt.Sprintf("  %-10s", fact[0]))
		value := fact[1]
		if fact[0] != "Remote" || !m.editingRemote {
			value = m.clip(value, valueW)
			value = renameValue.Render(value)
		}
		out = append(out, label+"  "+value)
	}
	return out
}

func (m renameModel) commitFact() string {
	if m.step == renameDone && m.commit != nil {
		if m.commit.Committed {
			return "committed"
		}
		if m.commit.Message != "" {
			return m.commit.Message
		}
	}
	if m.flags.dryRun {
		return "not written"
	}
	if m.flags.noCommit {
		return "skipped"
	}
	if m.plan != nil && !m.plan.AutoCommitEligible {
		if m.plan.AutoCommitSkipReason != "" {
			return m.plan.AutoCommitSkipReason
		}
		return "skipped"
	}
	return "automatic"
}

func (m renameModel) helpLine() string {
	if m.filtering {
		return renameHelp.Render("enter choose  ·  esc clear  ·  up/down move")
	}
	switch m.step {
	case renamePick:
		return renameHelp.Render("j/k move  ·  enter choose  ·  / filter  ·  q quit")
	case renameName:
		return renameHelp.Render("enter continue  ·  esc back")
	case renameReview:
		if m.editingRemote {
			return renameHelp.Render("enter update the plan  ·  esc cancel the url")
		}
		if m.flags.dryRun {
			return renameHelp.Render("enter close  ·  esc back  ·  u remote  ·  q quit")
		}
		return renameHelp.Render("enter rename  ·  esc back  ·  u remote  ·  q quit")
	case renameWorking:
		return renameHelp.Render("working")
	default:
		return renameHelp.Render("enter close")
	}
}

func renameRemoteLine(plan *projectrename.PlanResult) string {
	if plan == nil || (plan.OldURL == "" && plan.NewURL == "") {
		return "none"
	}
	if plan.OldURL == plan.NewURL {
		return plan.OldURL + "  unchanged"
	}
	if plan.OldURL == "" {
		return plan.NewURL
	}
	return plan.OldURL + " → " + plan.NewURL
}

func padRight(s string, width int) string {
	gap := width - len([]rune(s))
	if gap <= 0 {
		return s
	}
	return s + strings.Repeat(" ", gap)
}

func (m renameModel) clip(s string, width int) string {
	if width <= 0 {
		return s
	}
	return ui.Truncate(s, width)
}

func ruleWidth(cw int) int {
	if cw <= 0 {
		return 22
	}
	if cw < 4 {
		return 1
	}
	return min(cw-2, 56)
}
