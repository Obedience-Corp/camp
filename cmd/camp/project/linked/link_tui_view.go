package linked

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	tuistyles "github.com/Obedience-Corp/camp/internal/intent/tui"
	projectsvc "github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
)

var linkPal = theme.TUI()

var (
	linkTitle    = tuistyles.TitleStyle
	linkHelp     = tuistyles.HelpStyle
	linkErr      = tuistyles.ErrorStyle
	linkOk       = tuistyles.SuccessStyle
	linkMuted    = lipgloss.NewStyle().Foreground(linkPal.TextMuted)
	linkValue    = lipgloss.NewStyle().Foreground(linkPal.TextPrimary)
	linkName     = lipgloss.NewStyle().Foreground(linkPal.TextPrimary).Bold(true)
	linkAccent   = lipgloss.NewStyle().Foreground(linkPal.Accent).Bold(true)
	linkWarn     = lipgloss.NewStyle().Foreground(linkPal.Warning)
	linkPillOn   = lipgloss.NewStyle().Foreground(lipgloss.Color("#0D0D11")).Background(linkPal.Accent).Bold(true)
	linkPillDone = lipgloss.NewStyle().Foreground(linkPal.Success).Bold(true)
	linkPillOff  = lipgloss.NewStyle().Foreground(linkPal.TextMuted)
	linkRule     = lipgloss.NewStyle().Foreground(linkPal.Border)
	linkBox      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(linkPal.BorderFocus).Padding(0, 1)
	linkBoxDone  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(linkPal.Success).Padding(0, 1)
)

const (
	linkBoxOverhead = 4
	linkMinBoxWidth = 36
	linkMinBoxH     = 14
)

type linkLayout struct {
	cw       int
	boxed    bool
	listRows int
}

func (m linkModel) layout() linkLayout {
	wKnown, hKnown := m.width > 0, m.height > 0
	l := linkLayout{
		boxed: (!wKnown || m.width >= linkMinBoxWidth) && (!hKnown || m.height >= linkMinBoxH),
	}
	if wKnown {
		l.cw = m.width
		if l.boxed {
			l.cw -= linkBoxOverhead
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

func (m linkModel) View() string {
	if m.quitting && m.step != stepDone {
		return ""
	}
	lay := m.layout()
	lines := []string{m.titleLine(), "", m.stepsLine(), ""}
	lines = append(lines, m.bodyLines(lay)...)
	lines = append(lines, "", m.helpLine())
	if m.errMsg != "" {
		lines = append(lines, linkErr.Render(ui.Truncate(m.errMsg, max(lay.cw, 1))))
	}
	budget := 0
	if m.height > 0 {
		budget = m.height
		if lay.boxed {
			budget = max(budget-2, 1)
		}
	}
	content := strings.Join(ui.CapFrame(lines, lay.cw, budget), "\n")
	box := linkBox
	if m.step == stepDone {
		box = linkBoxDone
	}
	if lay.boxed {
		return ui.FitFullscreenView(box.Render(content), m.height)
	}
	return ui.FitFullscreenView(content, m.height)
}

func (m linkModel) titleLine() string {
	switch m.step {
	case stepDone:
		return ui.SuccessIcon() + "  " + linkOk.Render("Project linked")
	case stepWork:
		return linkTitle.Render("Linking") + "  " + linkMuted.Render("adding the shortcut")
	case stepCamp:
		label := ui.CountLabel(len(m.camps), "camp", "camps")
		return linkTitle.Render("Choose a camp") + "  " + linkMuted.Render(label)
	case stepFolder:
		if m.hasCamp && m.camp.Name != "" && !m.anchorOffer {
			return linkTitle.Render("Link a project") + "  " + linkMuted.Render("into "+m.camp.Name)
		}
		return linkTitle.Render("Link a project") + "  " + linkMuted.Render("the folder stays where it is")
	default:
		return linkTitle.Render("Link a project") + "  " + linkMuted.Render("the folder stays where it is")
	}
}

func (m linkModel) stepsLine() string {
	labels := []string{"Folder", "Name", "Camp", "Review"}
	current := 0
	switch m.step {
	case stepName:
		current = 1
	case stepCamp:
		current = 2
	case stepReview, stepWork, stepDone:
		current = 3
	}
	parts := make([]string, len(labels))
	for i, label := range labels {
		text := fmt.Sprintf(" %d %s ", i+1, label)
		switch {
		case m.step == stepDone || i < current:
			parts[i] = linkPillDone.Render(text)
		case i == current:
			parts[i] = linkPillOn.Render(text)
		default:
			parts[i] = linkPillOff.Render(text)
		}
	}
	return "  " + strings.Join(parts, "  ")
}

func (m linkModel) bodyLines(lay linkLayout) []string {
	switch m.step {
	case stepFolder:
		return m.browseLines(lay)
	case stepName:
		return m.nameLines(lay)
	case stepCamp:
		return m.campLines(lay)
	case stepWork:
		return m.workingLines()
	case stepDone:
		return m.doneLines(lay)
	default:
		return m.reviewLines(lay)
	}
}

func (m linkModel) browseLines(lay linkLayout) []string {
	lines := []string{
		linkMuted.Render("  Project folder"),
		"  " + m.pathInput.View(),
	}
	if m.awaitingPath && strings.TrimSpace(m.pathInput.Value()) == "" {
		lines = append(lines, linkMuted.Render("  The project can live anywhere on this machine."))
	}
	lines = append(lines, "", m.locationLine(lay.cw), "")
	lines = append(lines, m.browseRows(lay)...)
	if m.query != "" && !m.hasDir() {
		lines = append(lines, linkMuted.Render("  no folders match"))
	}
	return lines
}

func (m linkModel) hasDir() bool {
	for _, entry := range m.visible {
		if entry.kind == "dir" {
			return true
		}
	}
	return false
}

func (m linkModel) browseRows(lay linkLayout) []string {
	total := len(m.visible)
	if total == 0 {
		return []string{linkMuted.Render("  this folder is empty")}
	}
	budget := lay.listRows
	if budget <= 0 || total <= budget {
		return m.renderEntries(0, total, lay.cw)
	}
	start, end := ui.WindowRange(m.cursor, total, budget)
	out := m.renderEntries(start, end, lay.cw)
	out = append(out, linkMuted.Render(fmt.Sprintf("  %d–%d of %d", start+1, end, total)))
	return out
}

func (m linkModel) renderEntries(start, end, cw int) []string {
	out := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		out = append(out, m.entryRow(m.visible[i], i == m.cursor, cw))
	}
	return out
}

func (m linkModel) entryRow(entry linkEntry, selected bool, cw int) string {
	prefix := "  " + ui.CursorGlyph(selected)
	label := entry.label
	styled := linkName.Render(label)
	if selected {
		styled = linkAccent.Render(label)
	}
	row := prefix + styled
	if entry.badge != "" {
		row += "  " + linkMuted.Render(entry.badge)
	}
	if entry.kind == "here" && (cw == 0 || cw >= 48) {
		row += "  " + linkMuted.Render(ui.Truncate(linkShortPath(entry.path, m.homes), max(cw-28, 8)))
	}
	return row
}

func (m linkModel) locationLine(cw int) string {
	loc := linkShortPath(m.cwd, m.homes)
	return "  " + linkMuted.Render(ui.Truncate(loc, max(cw-2, 1)))
}

func (m linkModel) nameLines(lay linkLayout) []string {
	where := linkShortPath(m.chosenPath, m.homes)
	return []string{
		"  " + linkMuted.Render("Linking") + "  " + linkName.Render(filepathBase(m.chosenPath)),
		"  " + linkMuted.Render(ui.Truncate(where, max(lay.cw-2, 8))),
		"",
		linkMuted.Render("  Name in the camp"),
		"  " + m.nameInput.View(),
		"  " + linkAccent.Render(strings.Repeat("─", min(linkRuleWidth(lay.cw), 42))),
		"",
		linkMuted.Render("  Letters, digits, dot, underscore, or hyphen."),
	}
}

func (m linkModel) campLines(lay linkLayout) []string {
	lines := []string{}
	if m.campFiltering {
		header := linkAccent.Render("  / " + m.campQuery)
		if m.campQuery == "" {
			header = linkMuted.Render("  / filter")
		}
		lines = append(lines, header, "")
	}
	if len(m.campVisible) == 0 {
		lines = append(lines, linkMuted.Render("  no camps match"))
		return lines
	}
	total := len(m.campVisible)
	budget := lay.listRows
	start, end := 0, total
	if budget > 0 && total > budget {
		start, end = ui.WindowRange(m.campCursor, total, budget)
	}
	for i := start; i < end; i++ {
		lines = append(lines, m.campRow(m.campVisible[i], i == m.campCursor, lay.cw))
	}
	if budget > 0 && total > budget {
		lines = append(lines, linkMuted.Render(fmt.Sprintf("  %d–%d of %d", start+1, end, total)))
	}
	return lines
}

func (m linkModel) campRow(camp linkCamp, selected bool, cw int) string {
	prefix := "  " + ui.CursorGlyph(selected)
	name := camp.Name
	styled := linkName.Render(name)
	if selected {
		styled = linkAccent.Render(name)
	}
	row := prefix + styled
	if camp.Path != "" && (cw == 0 || cw >= 36) {
		row += "  " + linkMuted.Render(ui.Truncate(linkShortPath(camp.Path, m.homes), max(cw-len([]rune(name))-8, 8)))
	}
	return row
}

func (m linkModel) reviewLines(lay linkLayout) []string {
	lines := []string{m.heroLine(), "", linkMuted.Render("  The folder stays where it is. This is not a git submodule."), ""}
	if m.planning || m.plan == nil {
		lines = append(lines, linkMuted.Render("  Checking the folder"))
		return lines
	}
	if m.insideCamp() {
		lines = append(lines, "  "+ui.WarningIcon()+" "+linkWarn.Render("This folder is already inside the camp."), "")
	}
	lines = append(lines, m.factLines(lay)...)
	lines = append(lines, "", linkMuted.Render("  Nothing is written until you confirm."))
	return lines
}

func (m linkModel) workingLines() []string {
	return []string{
		m.heroLine(),
		"",
		"  " + m.spin.View() + "  " + linkValue.Render("Linking "+m.chosenName),
	}
}

func (m linkModel) doneLines(lay linkLayout) []string {
	lines := []string{m.heroLine(), "", linkMuted.Render("  The folder stays where it is. This is not a git submodule."), ""}
	lines = append(lines, m.factLines(lay)...)
	if m.result != nil {
		for _, warning := range m.result.Warnings {
			lines = append(lines, "  "+ui.WarningIcon()+" "+linkWarn.Render(ui.Truncate(warning, max(lay.cw-6, 8))))
		}
	}
	if m.note.Message != "" {
		lines = append(lines, "", "  "+linkMuted.Render(ui.Truncate(m.note.Message, max(lay.cw-2, 8))))
	}
	lines = append(lines, "", linkMuted.Render("  Undo"), "  "+linkAccent.Render(m.clip("camp project unlink "+m.linkedName(), max(lay.cw-4, 0))))
	return lines
}

func (m linkModel) heroLine() string {
	return "  " + linkAccent.Render(m.linkedName())
}

func (m linkModel) linkedName() string {
	if m.plan != nil && m.plan.Name != "" {
		return m.plan.Name
	}
	if m.result != nil && m.result.Name != "" {
		return m.result.Name
	}
	if m.chosenName != "" {
		return m.chosenName
	}
	return filepathBase(m.chosenPath)
}

func (m linkModel) factLines(lay linkLayout) []string {
	valueW := 0
	if lay.cw > 18 {
		valueW = lay.cw - 16
	}
	folder := m.chosenPath
	camp := m.camp.Name
	shortcut := "projects/" + m.linkedName()
	git := "no"
	if m.plan != nil {
		folder = m.plan.Source
		if m.plan.CampaignName != "" {
			camp = m.plan.CampaignName
		}
		if m.plan.Path != "" {
			shortcut = m.plan.Path
		}
		git = linkGitFact(m.plan)
	}
	facts := [][2]string{
		{"Folder", linkShortPath(folder, m.homes)},
		{"Camp", camp},
		{"Shortcut", shortcut},
		{"Git", git},
		{"Commit", m.commitFact()},
	}
	out := make([]string, 0, len(facts)+1)
	out = append(out, linkRule.Render(strings.Repeat("─", linkRuleWidth(lay.cw))))
	for _, fact := range facts {
		label := linkMuted.Render(fmt.Sprintf("  %-10s", fact[0]))
		out = append(out, label+"  "+linkValue.Render(m.clip(fact[1], valueW)))
	}
	return out
}

func (m linkModel) insideCamp() bool {
	if m.plan == nil || !m.hasCamp {
		return false
	}
	return linkInsideCamp(m.plan.Source, m.camp.Path)
}

func (m linkModel) commitFact() string {
	if m.step == stepDone && m.note.Committed {
		return "committed"
	}
	if m.step == stepDone && m.note.Message != "" {
		return "see below"
	}
	if m.noCommit {
		return "skipped"
	}
	return "automatic"
}

func linkGitFact(plan *projectsvc.LinkPlan) string {
	if plan == nil || !plan.IsGit {
		return "no"
	}
	if plan.Type != "" {
		return "yes, " + plan.Type
	}
	return "yes"
}

func (m linkModel) helpLine() string {
	if m.campFiltering {
		return linkHelp.Render("enter choose  ·  esc clear  ·  up/down move")
	}
	switch m.step {
	case stepFolder:
		if m.awaitingPath && strings.TrimSpace(m.pathInput.Value()) == "" {
			return linkHelp.Render("type or paste a path  ·  up/down browse  ·  esc quit")
		}
		esc := "esc quit"
		if strings.TrimSpace(m.pathInput.Value()) != "" {
			esc = "esc clear"
		}
		return linkHelp.Render("enter link  ·  tab open  ·  up/down move  ·  " + esc)
	case stepName:
		return linkHelp.Render("enter continue  ·  esc back")
	case stepCamp:
		return linkHelp.Render("j/k move  ·  enter choose  ·  / filter  ·  esc back  ·  q quit")
	case stepReview:
		return linkHelp.Render("enter link  ·  esc back  ·  c camp  ·  q quit")
	case stepWork:
		return linkHelp.Render("linking")
	default:
		return linkHelp.Render("enter close")
	}
}

func (m linkModel) clip(s string, width int) string {
	if width <= 0 {
		return s
	}
	return ui.Truncate(s, width)
}

func linkRuleWidth(cw int) int {
	if cw <= 0 {
		return 22
	}
	if cw < 4 {
		return 1
	}
	return min(cw-2, 56)
}

func filepathBase(path string) string {
	if path == "" {
		return ""
	}
	base := path
	if i := strings.LastIndexAny(path, `/\`); i >= 0 {
		base = path[i+1:]
	}
	if base == "" {
		return path
	}
	return base
}
