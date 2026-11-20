package main

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/ui"
)

const (
	listAllOrgsLabel = "all orgs"
	// listNoOrgLabel names rows without an org (remote camps from an older
	// camp). It cannot collide with a real org: org names are lowercase,
	// digits, and hyphens only.
	listNoOrgLabel = "(no org)"
)

// orgLabel is how an org is shown and filtered on.
func orgLabel(org string) string {
	if org == "" {
		return listNoOrgLabel
	}
	return org
}

// orgPickerRow is one choice in the org picker; org is the filter value it
// applies, "" meaning no filter.
type orgPickerRow struct {
	org    string
	label  string
	total  int
	active int
}

// orgNames is every org the browser knows about: the registry's orgs (so an
// org with no camps is still listed) plus any org carried only by remote rows,
// ordered fallback first, then alphabetically. Rows without an org are not an
// org a camp can be moved to, so they are left out here.
func (m listTUIModel) orgNames() []string {
	set := make(map[string][]campaignEntry, len(m.orgs))
	for _, o := range m.orgs {
		set[o] = nil
	}
	for _, e := range m.all {
		if e.Org != "" {
			set[e.Org] = nil
		}
	}
	return sortedGroupOrgs(set, m.fallback)
}

func (m listTUIModel) orgPickerRows() []orgPickerRow {
	total := map[string]int{}
	active := map[string]int{}
	all := orgPickerRow{label: listAllOrgsLabel, total: len(m.all)}
	for _, e := range m.all {
		total[e.Org]++
		if e.Status == config.StatusActive {
			active[e.Org]++
			all.active++
		}
	}
	rows := []orgPickerRow{all}
	for _, o := range m.orgNames() {
		rows = append(rows, orgPickerRow{org: o, label: o, total: total[o], active: active[o]})
	}
	if total[""] > 0 {
		rows = append(rows, orgPickerRow{org: listNoOrgLabel, label: listNoOrgLabel, total: total[""], active: active[""]})
	}
	return rows
}

func (m *listTUIModel) openOrgPicker() {
	m.overlay = listOverlayOrgs
	m.pointOrgPickerAt(m.orgFilter)
}

func (m *listTUIModel) pointOrgPickerAt(org string) {
	m.orgCursor = 0
	for i, r := range m.orgPickerRows() {
		if r.org == org {
			m.orgCursor = i
			return
		}
	}
}

// pickedOrg is the org under the picker cursor ("" for all orgs).
func (m listTUIModel) pickedOrg() string {
	rows := m.orgPickerRows()
	return rows[ui.ClampIdx(m.orgCursor, len(rows))].org
}

func (m listTUIModel) updateOrgPicker(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	rows := m.orgPickerRows()
	switch key.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "esc", "o":
		m.overlay = listOverlayNone
	case "down", "j":
		m.orgCursor = (m.orgCursor + 1) % len(rows)
	case "up", "k":
		m.orgCursor = (m.orgCursor - 1 + len(rows)) % len(rows)
	case "enter":
		m.overlay = listOverlayNone
		m.applyOrgFilter(m.pickedOrg())
	}
	return m, nil
}

// applyOrgFilter narrows the browser to org ("" for all orgs), keeping the
// cursor on the same camp when that camp is still visible.
func (m *listTUIModel) applyOrgFilter(org string) {
	m.orgFilter = org
	m.rebuildKeepingSelection()
	if org == "" {
		m.setStatus("showing all orgs", false)
		return
	}
	m.setStatus(fmt.Sprintf("showing org %q", org), false)
}

// overlayBudget is the number of content lines an overlay may use, or 0 when
// the terminal height is not yet known.
func (m listTUIModel) overlayBudget(lay listLayout) int {
	if m.height <= 0 {
		return 0
	}
	if lay.boxed {
		return max(m.height-2, 1)
	}
	return m.height
}

func (m listTUIModel) orgPickerView() string {
	lay := m.layout()
	rows := m.orgPickerRows()
	cursor := ui.ClampIdx(m.orgCursor, len(rows))

	budget := m.overlayBudget(lay)
	chrome := 4 // title, blank, blank, hint
	if budget > 0 && len(rows) > budget-chrome && budget-chrome < 3 {
		chrome = 2 // drop the blank separators before dropping rows
	}
	gap := []string{""}
	if chrome == 2 {
		gap = nil
	}
	start, end := 0, len(rows)
	indicator := false
	if budget > 0 && len(rows) > budget-chrome {
		capacity := budget - chrome
		if capacity >= 2 {
			capacity--
			indicator = true
		}
		start, end = ui.WindowRange(cursor, len(rows), max(capacity, 1))
	}

	nameW := 0
	for _, r := range rows {
		nameW = max(nameW, lipgloss.Width(r.label))
	}
	if lay.cw > 0 {
		nameW = min(nameW, max(lay.cw-listRowPrefix-2-len("0 (0 active)"), listNameMin))
	}

	lines := append([]string{listTitleStyle.Render("Filter by org")}, gap...)
	for i := start; i < end; i++ {
		r := rows[i]
		name := fmt.Sprintf("%-*s", nameW, ui.Truncate(r.label, nameW))
		lines = append(lines, "  "+ui.CursorGlyph(i == cursor)+styleName(name, i == cursor)+"  "+
			listMutedStyle.Render(fmt.Sprintf("%d (%d active)", r.total, r.active)))
	}
	if indicator {
		lines = append(lines, listMutedStyle.Render(fmt.Sprintf("  [%d-%d of %d]", start+1, end, len(rows))))
	}
	lines = append(lines, gap...)
	lines = append(lines, listHelpStyle.Render(ui.CollapseHelp(lay.cw,
		"j/k: move . enter: apply . esc: cancel", "enter apply . esc cancel", "esc cancel")))
	return m.frame(lines, lay)
}
