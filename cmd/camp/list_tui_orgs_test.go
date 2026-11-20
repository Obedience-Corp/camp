package main

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/ui/uitest"
)

var orgFixtureSizes = []struct{ w, h int }{
	{120, 40}, {85, 30}, {80, 24}, {60, 16}, {60, 20}, {40, 10}, {30, 8}, {24, 6}, {20, 5}, {15, 6},
	{10, 3}, {8, 2}, {5, 2}, {3, 3}, {1, 1}, {2, 1}, {0, 0}, {40, 1}, {1, 40},
}

// orgFixtureModel is 33 camps across 9 orgs in org-major order, the shape the
// browser was reported against.
func orgFixtureModel() listTUIModel {
	counts := []struct {
		org string
		n   int
	}{
		{"default", 3}, {"design", 4}, {"devtools", 5}, {"examples", 2}, {"festival-demo", 4},
		{"marketing", 3}, {"operations", 3}, {"research", 3}, {"web", 6},
	}
	var entries []campaignEntry
	orgs := make([]string, 0, len(counts))
	for _, c := range counts {
		orgs = append(orgs, c.org)
		for i := range c.n {
			status := config.StatusActive
			if i%3 == 2 {
				status = config.StatusInactive
			}
			id := fmt.Sprintf("c%02d", len(entries)+1)
			entries = append(entries, campaignEntry{
				ID: id, Name: id, Org: c.org, Status: status,
				Path: "/Users/someone/Dev/workspaces/" + id,
			})
		}
	}
	ti := textinput.New()
	ti.Prompt = "> "
	return listTUIModel{ctx: context.Background(), fallback: "default", orgs: orgs, all: entries, visible: entries, input: ti}
}

func listKeyMsg(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func listPress(m listTUIModel, keys ...string) listTUIModel {
	for _, k := range keys {
		next, _ := m.Update(listKeyMsg(k))
		m = next.(listTUIModel)
	}
	return m
}

func TestListFooter_KeepsStatusAndOrgsInSplitPane(t *testing.T) {
	m := responsiveModel()
	m.machinesConfigured = true
	for _, cw := range []int{81, 82} {
		got := m.footer(cw)
		if lipgloss.Width(got) > cw {
			t.Fatalf("cw=%d: footer width %d exceeds %d: %q", cw, lipgloss.Width(got), cw, got)
		}
		for _, want := range []string{"s status", "o orgs", "? help", "g go", "q quit"} {
			if !strings.Contains(got, want) {
				t.Errorf("cw=%d: footer %q missing %q", cw, got, want)
			}
		}
	}
}

func TestListFooter_LevelsDropLeastImportantFirst(t *testing.T) {
	m := responsiveModel()
	m.machinesConfigured = true
	levels := m.footerLevels()
	if !strings.Contains(levels[0], "o: orgs") || !strings.Contains(levels[0], "r: remotes") {
		t.Fatalf("full level should be colon style with every key: %q", levels[0])
	}
	dropOrder := []string{"j/k move", "y copy", "r remotes", "f filter", "m move org", "g go", "s status", "o orgs", "? help"}
	compact := levels[1:]
	if len(compact) != len(dropOrder)+1 {
		t.Fatalf("got %d compact levels, want %d: %q", len(compact), len(dropOrder)+1, compact)
	}
	for i, gone := range dropOrder {
		if !strings.Contains(compact[i], gone) || strings.Contains(compact[i+1], gone) {
			t.Errorf("level %d should be the last to show %q: %q then %q", i, gone, compact[i], compact[i+1])
		}
	}
	if last := compact[len(compact)-1]; last != "q quit" {
		t.Errorf("last level = %q, want q quit", last)
	}
	m.machinesConfigured = false
	for _, l := range m.footerLevels() {
		if strings.Contains(l, "remotes") {
			t.Fatalf("remotes hint shown without machines: %q", l)
		}
	}
}

func TestListOrgPicker_AppliesAndClearsFilterKeepingCursor(t *testing.T) {
	m := sized(responsiveModel(), 80, 24)
	m.orgs = []string{"default", "obey", "platform", "research"}
	m.cursor = 3 // delta, org platform

	m = listPress(m, "o")
	if m.overlay != listOverlayOrgs || m.orgCursor != 0 {
		t.Fatalf("o should open the picker on all orgs: overlay=%v cursor=%d", m.overlay, m.orgCursor)
	}
	m = listPress(m, "j", "j", "j", "enter") // default, obey, platform
	if m.overlay != listOverlayNone || m.orgFilter != "platform" {
		t.Fatalf("enter should apply platform: overlay=%v filter=%q", m.overlay, m.orgFilter)
	}
	if got := names(m.visible); !reflect.DeepEqual(got, []string{"delta", "epsilon"}) {
		t.Fatalf("visible = %v, want platform camps", got)
	}
	if m.visible[m.cursor].Name != "delta" {
		t.Errorf("cursor on %q, want delta (same camp as before)", m.visible[m.cursor].Name)
	}
	if m.status != `showing org "platform"` {
		t.Errorf("status = %q", m.status)
	}

	m = listPress(m, "o")
	if m.orgCursor != 3 {
		t.Fatalf("picker should reopen on the active filter row 3, got %d", m.orgCursor)
	}
	m = listPress(m, "k", "k", "k", "enter")
	if m.orgFilter != "" || len(m.visible) != len(m.all) {
		t.Fatalf("all orgs should clear the filter: filter=%q visible=%d", m.orgFilter, len(m.visible))
	}
	if m.visible[m.cursor].Name != "delta" {
		t.Errorf("cursor on %q after clearing, want delta", m.visible[m.cursor].Name)
	}
	if m.status != "showing all orgs" {
		t.Errorf("status = %q", m.status)
	}
}

func TestListOrgPicker_CancelAndWrap(t *testing.T) {
	m := responsiveModel()
	m.orgs = []string{"default", "obey", "platform", "research"}
	m = listPress(m, "o", "k")
	if m.orgCursor != 4 {
		t.Fatalf("k from the top should wrap to the last org, got %d", m.orgCursor)
	}
	m = listPress(m, "j")
	if m.orgCursor != 0 {
		t.Fatalf("j from the bottom should wrap to all orgs, got %d", m.orgCursor)
	}
	for _, cancel := range []string{"esc", "o"} {
		mm := listPress(m, "j", cancel)
		if mm.overlay != listOverlayNone || mm.orgFilter != "" {
			t.Errorf("%s should cancel without filtering: overlay=%v filter=%q", cancel, mm.overlay, mm.orgFilter)
		}
	}
	next, cmd := m.Update(listKeyMsg("ctrl+c"))
	if !next.(listTUIModel).quitting || cmd == nil {
		t.Error("ctrl+c in the picker should quit")
	}
	if mm := listPress(m, "q"); mm.quitting || mm.overlay != listOverlayOrgs {
		t.Error("q inside the picker should not quit the browser")
	}
}

func TestListOrgPicker_ListsEmptyAndRemoteOnlyOrgs(t *testing.T) {
	m := responsiveModel()
	m.orgs = []string{"default", "obey", "platform", "research", "archive"}
	m.machinesConfigured = true
	next, _ := m.Update(remoteLoadedMsg{rows: []campaignEntry{
		{ID: "R", Name: "far", Org: "fleet", Status: config.StatusActive, Machine: "archdtop", Path: "/r"},
	}})
	m = next.(listTUIModel)

	var got []string
	counts := map[string]string{}
	for _, r := range m.orgPickerRows() {
		got = append(got, r.label)
		counts[r.label] = fmt.Sprintf("%d (%d active)", r.total, r.active)
	}
	want := []string{"all orgs", "default", "archive", "fleet", "obey", "platform", "research"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("picker rows = %v, want %v", got, want)
	}
	for label, c := range map[string]string{"all orgs": "7 (4 active)", "archive": "0 (0 active)", "fleet": "1 (1 active)", "obey": "2 (0 active)"} {
		if counts[label] != c {
			t.Errorf("%s counts = %q, want %q", label, counts[label], c)
		}
	}
	if !strings.Contains(m.orgNamesCSV(), "archive") {
		t.Errorf("move overlay org list should include the empty org: %q", m.orgNamesCSV())
	}

	m = sized(m, 80, 24)
	m = listPress(m, "o")
	out := m.View()
	for _, want := range []string{"Filter by org", "archive", "0 (0 active)", "fleet"} {
		if !strings.Contains(out, want) {
			t.Errorf("picker view missing %q:\n%s", want, out)
		}
	}
}

func TestListOrgPicker_RemoteLoadKeepsHighlightedOrg(t *testing.T) {
	m := responsiveModel()
	m.orgs = []string{"default", "obey", "platform", "research"}
	m.machinesConfigured = true
	m = listPress(m, "o", "j", "j")
	if m.pickedOrg() != "obey" {
		t.Fatalf("picker on %q, want obey", m.pickedOrg())
	}
	next, _ := m.Update(remoteLoadedMsg{rows: []campaignEntry{
		{ID: "R", Name: "far", Org: "fleet", Status: config.StatusActive, Machine: "archdtop", Path: "/r"},
	}})
	m = listPress(next.(listTUIModel), "enter")
	if m.orgFilter != "obey" {
		t.Fatalf("filter = %q after a remote org was inserted above the highlight, want obey", m.orgFilter)
	}
}

func TestListOrgPicker_RemoteRowsSurviveFilterChange(t *testing.T) {
	m := responsiveModel()
	m.orgs = []string{"default", "obey", "platform", "research"}
	m.machinesConfigured = true
	m.applyOrgFilter("obey")
	next, _ := m.Update(remoteLoadedMsg{rows: []campaignEntry{
		{ID: "R", Name: "far", Org: "platform", Status: config.StatusActive, Machine: "archdtop", Path: "/r"},
	}})
	m = next.(listTUIModel)
	if got := names(m.visible); !reflect.DeepEqual(got, []string{"this-is-an-extremely-long-campaign-name-that-should-truncate", "gamma"}) {
		t.Fatalf("obey filter visible = %v", got)
	}
	m.applyOrgFilter("platform")
	if got := names(m.visible); !reflect.DeepEqual(got, []string{"delta", "epsilon", "far"}) {
		t.Fatalf("platform visible = %v, want the remote row fetched while obey was shown", got)
	}
}

func TestListTopBar_ShowsOrgAndFilteredCount(t *testing.T) {
	m := responsiveModel()
	if got := m.topBar(); !strings.Contains(got, "6 camps  .  showing: all") || strings.Contains(got, " of ") || strings.Contains(got, "org:") {
		t.Errorf("unfiltered top bar changed: %q", got)
	}
	m.applyOrgFilter("obey")
	got := m.topBar()
	for _, want := range []string{"2 of 6 camps", "org: obey"} {
		if !strings.Contains(got, want) {
			t.Errorf("top bar %q missing %q", got, want)
		}
	}
	m.applyOrgFilter("")
	m.activeOnly = true
	m.rebuildVisible()
	if got := m.topBar(); !strings.Contains(got, "3 of 6 camps") || strings.Contains(got, "org:") {
		t.Errorf("active-only top bar = %q, want 3 of 6 camps and no org", got)
	}
}

func TestListView_EmptyOrgMessage(t *testing.T) {
	m := responsiveModel()
	m.orgs = []string{"default", "archive"}
	m.applyOrgFilter("archive")
	for _, s := range []struct{ w, h int }{{80, 24}, {40, 10}} {
		out := sized(m, s.w, s.h).View()
		if !strings.Contains(out, `no camps in org "archive"`) {
			t.Errorf("%dx%d: empty org view:\n%s", s.w, s.h, out)
		}
		uitest.AssertBounded(t, out, s.w, s.h)
	}
}

var indicatorRe = regexp.MustCompile(`\[(\d+)-(\d+) of (\d+)\]`)

// TestListView_ScrolledKeepsOrgHeaders checks every cursor position at every
// size: the selected camp is on screen exactly once under its own org header,
// headers never stack or dangle, and the indicator counts the camps drawn.
func TestListView_ScrolledKeepsOrgHeaders(t *testing.T) {
	sizes := []struct{ w, h int }{{85, 30}, {60, 16}, {80, 24}, {40, 10}, {60, 12}, {30, 10}}
	for _, s := range sizes {
		base := sized(orgFixtureModel(), s.w, s.h)
		lay := base.layout()
		if lay.listRows-1 < listMinHeaderWindow {
			t.Fatalf("%dx%d does not exercise the header window (listRows %d)", s.w, s.h, lay.listRows)
		}
		for cur := range base.visible {
			m := base
			m.cursor = cur
			uitest.AssertBounded(t, m.View(), s.w, s.h)
			body := m.bodyLines(lay)
			ctx := fmt.Sprintf("%dx%d cursor %d", s.w, s.h, cur)
			checkHeaderWindow(t, m, body, ctx)
			if len(body) < lay.listRows-1 {
				t.Fatalf("%s: drew %d of %d lines:\n%s", ctx, len(body), lay.listRows, strings.Join(body, "\n"))
			}
		}
	}
}

// At 60x16 with the cursor on the third camp of a three-camp org, centering
// lands on the previous org's last row and the window then ends on the next
// header; both edge rules firing used to leave a blank row above the indicator.
func TestListView_ScrolledWindowUsesEveryRow(t *testing.T) {
	m := sized(orgFixtureModel(), 60, 16)
	m.cursor = 20
	if m.visible[m.cursor].Org != "marketing" || m.visible[m.cursor-2].Org != "marketing" {
		t.Fatalf("fixture changed: cursor %d is %s in %s", m.cursor, m.visible[m.cursor].Name, m.visible[m.cursor].Org)
	}
	lay := m.layout()
	body := m.bodyLines(lay)
	checkHeaderWindow(t, m, body, "60x16 cursor 20")
	if len(body) != lay.listRows {
		t.Fatalf("drew %d of %d lines:\n%s", len(body), lay.listRows, strings.Join(body, "\n"))
	}
}

func checkHeaderWindow(t *testing.T, m listTUIModel, body []string, ctx string) {
	t.Helper()
	if len(body) > m.layout().listRows {
		t.Fatalf("%s: %d body lines exceed budget %d", ctx, len(body), m.layout().listRows)
	}
	match := indicatorRe.FindStringSubmatch(body[len(body)-1])
	if match == nil {
		t.Fatalf("%s: last line is not an indicator: %q", ctx, body[len(body)-1])
	}
	first, _ := strconv.Atoi(match[1])
	last, _ := strconv.Atoi(match[2])
	lines := body[:len(body)-1]

	isHeader := func(l string) bool { return l != "" && !strings.HasPrefix(l, " ") }
	if !isHeader(lines[0]) {
		t.Fatalf("%s: top line is not an org header: %q", ctx, lines[0])
	}
	if isHeader(lines[len(lines)-1]) {
		t.Fatalf("%s: trailing header with no rows: %q", ctx, lines[len(lines)-1])
	}
	org, row, selected := "", first-1, 0
	for i, l := range lines {
		if isHeader(l) {
			if i > 0 && isHeader(lines[i-1]) {
				t.Fatalf("%s: back-to-back headers %q, %q", ctx, lines[i-1], l)
			}
			org = strings.TrimSpace(l)
			continue
		}
		if row >= len(m.visible) {
			t.Fatalf("%s: more rows drawn than the indicator %d-%d claims", ctx, first, last)
		}
		e := m.visible[row]
		if !strings.Contains(l, e.Name) {
			t.Fatalf("%s: line %q should be camp %s (row %d)", ctx, l, e.Name, row)
		}
		if e.Org != org {
			t.Fatalf("%s: camp %s (org %s) drawn under header %q", ctx, e.Name, e.Org, org)
		}
		if strings.Contains(l, "> ") {
			selected++
			if row != m.cursor {
				t.Fatalf("%s: row %d marked selected, cursor is %d", ctx, row, m.cursor)
			}
		}
		row++
	}
	if row != last {
		t.Fatalf("%s: drew rows %d-%d, indicator says %d-%d", ctx, first, row, first, last)
	}
	if selected != 1 {
		t.Fatalf("%s: %d selected rows on screen, want 1:\n%s", ctx, selected, strings.Join(body, "\n"))
	}
}

func TestListView_ScrolledFirstPaintShowsSeveralOrgs(t *testing.T) {
	m := sized(orgFixtureModel(), 85, 30)
	out := m.View()
	for _, want := range []string{"default", "design", "devtools", "[1-"} {
		if !strings.Contains(out, want) {
			t.Errorf("first paint missing %q:\n%s", want, out)
		}
	}
	m.cursor = len(m.visible) - 1
	out = m.View()
	if !strings.Contains(out, "web") || !strings.Contains(out, "> c33") {
		t.Errorf("bottom of list should show the web header and the selected row:\n%s", out)
	}
}

func TestListOverlays_BoundedAtEverySize(t *testing.T) {
	for _, s := range orgFixtureSizes {
		for _, open := range []string{"o", "?"} {
			for _, mc := range []bool{false, true} {
				m := orgFixtureModel()
				m.machinesConfigured = mc
				m = listPress(sized(m, s.w, s.h), open)
				for range 6 {
					out := m.View()
					if out == "" {
						t.Fatalf("%dx%d %s: empty view", s.w, s.h, open)
					}
					if s.w > 0 && s.h > 0 {
						uitest.AssertBounded(t, out, s.w, s.h)
					}
					if open == "o" {
						m = listPress(m, "j", "j")
					}
				}
			}
		}
	}
}

func TestListOrgPicker_WindowsAroundCursor(t *testing.T) {
	m := listPress(sized(orgFixtureModel(), 60, 10), "o")
	for range 8 {
		m = listPress(m, "j")
	}
	out := m.View()
	if !strings.Contains(out, "> research") {
		t.Fatalf("picker cursor row not visible:\n%s", out)
	}
	if !strings.Contains(out, "of 10]") {
		t.Errorf("clipped picker should show a position indicator:\n%s", out)
	}
	if !strings.Contains(out, "esc") {
		t.Errorf("picker should keep its key hint:\n%s", out)
	}
}

func TestListHelp_OpensListsKeysAndCloses(t *testing.T) {
	m := sized(orgFixtureModel(), 85, 30)
	m.machinesConfigured = true
	m = listPress(m, "?")
	if m.overlay != listOverlayHelp {
		t.Fatal("? should open help")
	}
	out := m.View()
	for _, k := range m.shownKeys() {
		if !strings.Contains(out, k.help) {
			t.Errorf("help missing %q:\n%s", k.help, out)
		}
	}
	for _, closeKey := range []string{"esc", "?", "x", "q"} {
		mm := listPress(m, closeKey)
		if mm.overlay != listOverlayNone || mm.quitting {
			t.Errorf("%s should close help without quitting: overlay=%v quitting=%v", closeKey, mm.overlay, mm.quitting)
		}
	}
	next, cmd := m.Update(listKeyMsg("ctrl+c"))
	if !next.(listTUIModel).quitting || cmd == nil {
		t.Error("ctrl+c in help should quit")
	}
}

func TestListHelp_ShortTerminalKeepsDismissHint(t *testing.T) {
	for _, h := range []int{16, 12, 8, 6} {
		m := listPress(sized(orgFixtureModel(), 60, h), "?")
		out := m.View()
		uitest.AssertBounded(t, out, 60, h)
		if !strings.Contains(out, "any key closes help") {
			t.Errorf("60x%d: help lost its dismiss hint:\n%s", h, out)
		}
	}
}

// noOrgRemoteModel is remote rows from an older camp whose list --json has no
// org field, with no local rows, the first row selected.
func noOrgRemoteModel(n int) listTUIModel {
	var entries []campaignEntry
	for i := range n {
		id := fmt.Sprintf("r%02d", i+1)
		org := ""
		if i >= n/2 {
			org = "web"
		}
		entries = append(entries, campaignEntry{ID: id, Name: id, Org: org, Status: config.StatusActive, Machine: "oldbox", Path: "/srv/" + id})
	}
	ti := textinput.New()
	ti.Prompt = "> "
	m := listTUIModel{ctx: context.Background(), fallback: "default", orgs: []string{"default"}, all: entries, input: ti, remoteOn: true}
	m.rebuildVisible()
	return m
}

func TestListView_NoOrgFirstRowStaysSelected(t *testing.T) {
	for _, h := range []int{30, 16, 12, 10, 8, 6} {
		for _, cur := range []int{0, 1, 9, 19} {
			m := sized(noOrgRemoteModel(20), 85, h)
			m.cursor = cur
			out := m.View()
			uitest.AssertBounded(t, out, 85, h)
			if got := strings.Count(out, "> oldbox"); got != 1 {
				t.Fatalf("85x%d cursor %d: %d selected rows drawn, want 1:\n%s", h, cur, got, out)
			}
			if !strings.Contains(out, fmt.Sprintf("> oldbox · r%02d", cur+1)) {
				t.Fatalf("85x%d cursor %d: wrong or missing selected row:\n%s", h, cur, out)
			}
			if m.layout().listRows-1 >= listMinHeaderWindow && cur < 10 && !strings.Contains(out, listNoOrgLabel) {
				t.Fatalf("85x%d cursor %d: rows without an org have no header:\n%s", h, cur, out)
			}
		}
	}
}

func TestListOrgPicker_NoOrgRowCountsAndFilters(t *testing.T) {
	m := noOrgRemoteModel(6)
	rows := m.orgPickerRows()
	sum := 0
	for _, r := range rows[1:] {
		sum += r.total
	}
	if sum != rows[0].total {
		t.Fatalf("per-org counts sum to %d, all orgs says %d: %+v", sum, rows[0].total, rows)
	}
	last := rows[len(rows)-1]
	if last.label != listNoOrgLabel || last.total != 3 {
		t.Fatalf("last picker row = %+v, want %s with 3 camps", last, listNoOrgLabel)
	}
	if strings.Contains(m.orgNamesCSV(), listNoOrgLabel) {
		t.Errorf("move overlay should not offer %s as a target: %q", listNoOrgLabel, m.orgNamesCSV())
	}

	m = listPress(m, "o", "k", "enter")
	if m.orgFilter != listNoOrgLabel {
		t.Fatalf("filter = %q, want %q", m.orgFilter, listNoOrgLabel)
	}
	if got := names(m.visible); !reflect.DeepEqual(got, []string{"r01", "r02", "r03"}) {
		t.Fatalf("visible = %v, want the camps without an org", got)
	}
	if got := m.topBar(); !strings.Contains(got, "3 of 6 camps") || !strings.Contains(got, "org: "+listNoOrgLabel) {
		t.Errorf("top bar = %q", got)
	}
}

func TestListView_EmptyStateNamesActiveOnly(t *testing.T) {
	m := responsiveModel()
	m.orgs = []string{"default", "obey", "platform", "research"}
	m = listPress(m, "f")
	m.applyOrgFilter("research") // zeta is inactive
	if out := sized(m, 80, 24).View(); !strings.Contains(out, `no active camps in org "research"`) {
		t.Errorf("active-only empty org view:\n%s", out)
	}
	m.applyOrgFilter("")
	m.all = []campaignEntry{{ID: "Z", Name: "zeta", Org: "research", Status: config.StatusInactive}}
	m.rebuildVisible()
	if out := sized(m, 80, 24).View(); !strings.Contains(out, "no active camps to show") {
		t.Errorf("active-only empty view:\n%s", out)
	}
	m = listPress(m, "f")
	m.orgFilter = "archive"
	m.rebuildVisible()
	if out := sized(m, 80, 24).View(); !strings.Contains(out, `no camps in org "archive"`) {
		t.Errorf("all-status empty org view:\n%s", out)
	}
}

func TestListTUI_ActiveOnlyToggleKeepsSameCamp(t *testing.T) {
	m := responsiveModel()
	m.cursor = 3 // delta, active
	m = listPress(m, "f")
	if m.visible[m.cursor].Name != "delta" {
		t.Fatalf("f moved the cursor to %q, want delta", m.visible[m.cursor].Name)
	}
	m = listPress(m, "f")
	if m.visible[m.cursor].Name != "delta" {
		t.Fatalf("second f moved the cursor to %q, want delta", m.visible[m.cursor].Name)
	}
	m.cursor = 1 // inactive, hidden by f
	m = listPress(m, "f")
	if m.cursor != 0 {
		t.Errorf("hidden selection should move the cursor to the top, got %d", m.cursor)
	}
}

func TestListView_HeaderWindowRefusesWindowWithoutCursor(t *testing.T) {
	m := noOrgRemoteModel(6)
	headerless := []listBodyLine{{row: 0}, {row: 1}, {row: 2}, {header: true, org: "web"}, {org: "web", row: 3}}
	if out, ok := m.headerWindow(headerless, 3, 80); ok {
		t.Fatalf("a window whose only cursor position is hidden by a sticky header was accepted:\n%s", strings.Join(out, "\n"))
	}
}
