package org

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/ui/uitest"
)

func sizedOrg(m orgTUIModel, w, h int) orgTUIModel {
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return next.(orgTUIModel)
}

func manyOrgsModel(t *testing.T, nOrgs, membersPer int) orgTUIModel {
	t.Helper()
	m := newTestOrgModel(t)
	orgs := make([]orgCount, 0, nOrgs)
	for i := 0; i < nOrgs; i++ {
		name := fmt.Sprintf("org-%02d-with-a-longish-name", i)
		orgs = append(orgs, orgCount{Org: name, Campaigns: membersPer, Active: membersPer / 2})
	}
	m.orgs = orgs
	m.orgCursor = 0
	m.focusedOrg = orgs[0].Org
	members := make([]orgMember, 0, membersPer)
	for i := 0; i < membersPer; i++ {
		members = append(members, orgMember{
			ID:     fmt.Sprintf("id-%d", i),
			Name:   fmt.Sprintf("campaign-with-long-name-%02d", i),
			Status: config.StatusActive,
		})
	}
	m.members = members
	return m
}

func TestOrgView_BoundedAtEverySize(t *testing.T) {
	ui.SetNoColor(true)
	t.Cleanup(func() { ui.SetNoColor(false) })

	sizes := []struct{ w, h int }{
		{120, 40}, {80, 24}, {72, 20}, {60, 20}, {40, 10}, {30, 8}, {24, 6}, {20, 5}, {15, 6},
	}
	for _, s := range sizes {
		m := sizedOrg(manyOrgsModel(t, 20, 15), s.w, s.h)
		m.orgCursor = 12
		m.syncFocusedOrg()
		uitest.AssertBounded(t, m.View(), s.w, s.h)

		m.pane = paneMembers
		m.memCursor = 10
		uitest.AssertBounded(t, m.View(), s.w, s.h)
	}
}

func TestOrgView_SelectionVisibleWhenScrolled(t *testing.T) {
	ui.SetNoColor(true)
	t.Cleanup(func() { ui.SetNoColor(false) })

	m := sizedOrg(manyOrgsModel(t, 30, 5), 40, 10)
	m.orgCursor = len(m.orgs) - 1
	m.syncFocusedOrg()
	out := m.View()
	uitest.AssertBounded(t, out, 40, 10)
	want := m.orgs[m.orgCursor].Org
	// Name may be truncated; require a distinctive prefix.
	prefix := ui.Truncate(want, 12)
	if prefix == "" || !strings.Contains(out, strings.TrimSuffix(prefix, "...")) && !strings.Contains(out, prefix) {
		// Accept either full or truncated form.
		if !strings.Contains(out, "org-29") {
			t.Fatalf("cursor org not visible at 40x10 (want prefix of %q):\n%s", want, out)
		}
	}
}

func TestOrgView_NoPanicAtAbsurdSizes(t *testing.T) {
	ui.SetNoColor(true)
	t.Cleanup(func() { ui.SetNoColor(false) })

	sizes := []struct{ w, h int }{
		{10, 3}, {8, 2}, {5, 2}, {3, 3}, {1, 1}, {2, 1}, {0, 0}, {40, 1}, {1, 40},
	}
	for _, s := range sizes {
		m := sizedOrg(manyOrgsModel(t, 10, 8), s.w, s.h)
		m.orgCursor = 5
		m.syncFocusedOrg()
		out := m.View()
		if out == "" {
			t.Fatalf("%dx%d produced empty view", s.w, s.h)
		}
		if s.w > 0 && s.h > 0 {
			uitest.AssertBounded(t, out, s.w, s.h)
		}
	}
}

func TestOrgView_DualVsSingleReflow(t *testing.T) {
	ui.SetNoColor(true)
	t.Cleanup(func() { ui.SetNoColor(false) })

	m := sizedOrg(newTestOrgModel(t), 100, 24)
	wide := m.View()
	uitest.AssertBounded(t, wide, 100, 24)
	// Wide dual should mention both pane titles.
	if !strings.Contains(wide, "Orgs") || !strings.Contains(wide, "Members") {
		t.Fatalf("wide view missing pane titles:\n%s", wide)
	}

	m = sizedOrg(m, 40, 24)
	narrow := m.View()
	uitest.AssertBounded(t, narrow, 40, 24)
	// Single-pane still usable; focused org list remains.
	if !strings.Contains(narrow, "default") && !strings.Contains(narrow, "Orgs") {
		t.Fatalf("narrow view missing org content:\n%s", narrow)
	}
}

func TestOrgView_OverlayBounded(t *testing.T) {
	ui.SetNoColor(true)
	t.Cleanup(func() { ui.SetNoColor(false) })

	for _, s := range []struct{ w, h int }{{80, 24}, {40, 10}, {20, 5}, {10, 3}} {
		m := sizedOrg(newTestOrgModel(t), s.w, s.h)
		m.overlay = overlayCreateEmpty
		m.input.SetValue("new-org")
		out := m.View()
		if out == "" {
			t.Fatalf("%dx%d overlay empty", s.w, s.h)
		}
		uitest.AssertBounded(t, out, s.w, s.h)
	}
}

func TestOrgView_FooterCollapsesWithWidth(t *testing.T) {
	ui.SetNoColor(true)
	t.Cleanup(func() { ui.SetNoColor(false) })

	m := sizedOrg(newTestOrgModel(t), 120, 40)
	wide := m.footer(116)
	if !strings.Contains(wide, "rename") {
		t.Fatalf("wide footer should show full help, got %q", wide)
	}
	narrow := m.footer(26)
	if lipgloss.Width(narrow) > 26 {
		t.Fatalf("narrow footer width %d exceeds 26: %q", lipgloss.Width(narrow), narrow)
	}
	if strings.Contains(narrow, "rename") {
		t.Fatalf("narrow footer should have collapsed, got %q", narrow)
	}
}

func TestOrgView_EmptyRegistryBounded(t *testing.T) {
	ui.SetNoColor(true)
	t.Cleanup(func() { ui.SetNoColor(false) })

	m := newTestOrgModel(t)
	m.orgs = nil
	m.members = nil
	for _, s := range []struct{ w, h int }{{80, 24}, {20, 5}, {10, 3}} {
		mm := sizedOrg(m, s.w, s.h)
		out := mm.View()
		if !strings.Contains(out, "No campaigns") && !strings.Contains(out, "Orgs") {
			t.Fatalf("%dx%d empty view missing placeholder:\n%s", s.w, s.h, out)
		}
		uitest.AssertBounded(t, out, s.w, s.h)
	}
}

type orgShape struct {
	name          string
	total, active int
}

// userShapedOrgs mirrors a real registry whose names were truncated at ~74 cols.
var userShapedOrgs = []orgShape{
	{"default", 7, 7}, {"author", 1, 1}, {"blockhead", 5, 3}, {"devtools", 2, 2},
	{"examples", 1, 1}, {"festival-demo", 2, 2}, {"job-search", 2, 1}, {"obc", 7, 7},
	{"personal", 3, 3},
}

// shapedOrgModel builds a browser over an in-memory registry; nothing touches disk.
func shapedOrgModel(shapes []orgShape) orgTUIModel {
	reg := &config.Registry{Version: 2, Campaigns: map[string]config.RegisteredCampaign{}}
	for _, s := range shapes {
		for i := 0; i < s.total; i++ {
			id := fmt.Sprintf("%s-%02d", s.name, i)
			status := config.StatusActive
			if i >= s.active {
				status = config.StatusInactive
			}
			reg.Campaigns[id] = config.RegisteredCampaign{
				ID: id, Name: fmt.Sprintf("%s-camp-%d", s.name, i+1), Org: s.name, Status: status,
			}
		}
	}
	m := orgTUIModel{ctx: context.Background(), input: textinput.New()}
	m.loadFromRegistry(reg)
	return m
}

func orgPaneLines(m orgTUIModel) []string {
	return strings.Split(m.renderOrgPane(m.layout()), "\n")
}

// labelColumns returns, per org shown in the pane, the display column where its
// count label starts; orgs scrolled out of view are absent.
func labelColumns(t *testing.T, lines []string, m orgTUIModel) map[string]int {
	t.Helper()
	cols := map[string]int{}
	for _, o := range m.orgs {
		for _, ln := range lines {
			at := strings.Index(ln, " "+o.Org+" ")
			if at < 0 {
				continue
			}
			label := orgCountLabel(o)
			rel := strings.Index(ln[at+len(o.Org)+1:], label)
			if rel < 0 {
				t.Fatalf("org %q row has no count label %q: %q", o.Org, label, ln)
			}
			cols[o.Org] = lipgloss.Width(ln[:at+len(o.Org)+1+rel])
		}
	}
	return cols
}

func assertOneColumn(t *testing.T, what string, cols map[string]int) int {
	t.Helper()
	want := -1
	for name, c := range cols {
		if want < 0 {
			want = c
		}
		if c != want {
			t.Fatalf("%s column ragged: %v (%q)", what, cols, name)
		}
	}
	return want
}

func TestOrgView_UserShapedOrgsFitUntruncated(t *testing.T) {
	ui.SetNoColor(true)
	t.Cleanup(func() { ui.SetNoColor(false) })

	for _, w := range []int{74, 80, 100, 120} {
		m := sizedOrg(shapedOrgModel(userShapedOrgs), w, 34)
		uitest.AssertBounded(t, m.View(), w, 34)
		lines := orgPaneLines(m)
		pane := strings.Join(lines, "\n")
		if strings.Contains(pane, "...") {
			t.Fatalf("width %d: orgs pane truncates a name:\n%s", w, pane)
		}
		cols := labelColumns(t, lines, m)
		if len(cols) != len(userShapedOrgs) {
			t.Fatalf("width %d: %d of %d orgs shown in full:\n%s", w, len(cols), len(userShapedOrgs), pane)
		}
		assertOneColumn(t, fmt.Sprintf("width %d count", w), cols)
	}
}

func TestOrgView_PaneEdgesIgnoreCursor(t *testing.T) {
	ui.SetNoColor(true)
	t.Cleanup(func() { ui.SetNoColor(false) })

	// Short names on top, one wide row below: scrolling changes what is visible.
	shapes := []orgShape{
		{"default", 1, 1}, {"a1", 1, 1}, {"a2", 2, 1}, {"a3", 1, 0}, {"a4", 3, 3},
		{"zeta-much-longer-org", 12, 10},
	}
	for _, s := range []struct{ w, h int }{{120, 10}, {74, 10}, {74, 34}} {
		m := sizedOrg(shapedOrgModel(shapes), s.w, s.h)
		var edges string
		for i := range m.orgs {
			if i > 0 {
				m = key(m, "j")
			}
			out := m.View()
			uitest.AssertBounded(t, out, s.w, s.h)
			top := strings.Split(out, "\n")[1]
			got := fmt.Sprintf("%d/%d", lipgloss.Width(top), strings.Count(top, "╭"))
			for c, r := range []rune(top) {
				if r == '╭' || r == '╮' {
					got += fmt.Sprintf(" %d", c)
				}
			}
			if i == 0 {
				edges = got
				continue
			}
			if got != edges {
				t.Fatalf("%dx%d: pane edges moved at cursor %d (%s): %q != %q\n%s",
					s.w, s.h, i, m.orgs[i].Org, got, edges, out)
			}
		}
	}
}

func TestOrgView_LongOrgNameTruncatesWithinHalf(t *testing.T) {
	ui.SetNoColor(true)
	t.Cleanup(func() { ui.SetNoColor(false) })

	long := "an-organization-name-well-beyond-forty-columns"
	shapes := append([]orgShape{{long, 2, 1}}, userShapedOrgs...)
	m := sizedOrg(shapedOrgModel(shapes), 72, 20)
	uitest.AssertBounded(t, m.View(), 72, 20)

	lay := m.layout()
	usable := 72 - orgPaneGapW - 2*orgPaneOverheadW
	if lay.orgW != usable/2 || lay.memW < usable/2 || lay.orgW+lay.memW != usable {
		t.Fatalf("orgW=%d memW=%d, want orgW=%d and memW>=%d summing to %d",
			lay.orgW, lay.memW, usable/2, usable/2, usable)
	}
	lines := orgPaneLines(m)
	pane := strings.Join(lines, "\n")
	if strings.Contains(pane, long) || !strings.Contains(pane, long[:10]) || !strings.Contains(pane, "...") {
		t.Fatalf("long org name should be truncated with an ellipsis:\n%s", pane)
	}
	cols := labelColumns(t, lines, m)
	if len(cols) != len(userShapedOrgs) {
		t.Fatalf("short org names should stay whole, got %v:\n%s", cols, pane)
	}
	assertOneColumn(t, "count", cols)
}

func TestOrgView_StackedAlignsCountsAfterLongestName(t *testing.T) {
	ui.SetNoColor(true)
	t.Cleanup(func() { ui.SetNoColor(false) })

	for _, w := range []int{60, 40} {
		m := sizedOrg(shapedOrgModel(userShapedOrgs), w, 20)
		if m.layout().dual {
			t.Fatalf("width %d should stack", w)
		}
		uitest.AssertBounded(t, m.View(), w, 20)
		lines := orgPaneLines(m)
		cols := labelColumns(t, lines, m)
		if len(cols) != len(userShapedOrgs) {
			t.Fatalf("width %d: names truncated, got %v:\n%s", w, cols, strings.Join(lines, "\n"))
		}
		// Border + padding, row prefix, longest name ("festival-demo"), one space.
		if got, want := assertOneColumn(t, "count", cols), 2+orgRowPrefixW+len("festival-demo")+1; got != want {
			t.Fatalf("width %d: counts start at column %d, want %d (right after the longest name):\n%s",
				w, got, want, strings.Join(lines, "\n"))
		}
	}
}

func TestOrgView_MemberStatusAligned(t *testing.T) {
	ui.SetNoColor(true)
	t.Cleanup(func() { ui.SetNoColor(false) })

	m := sizedOrg(shapedOrgModel(userShapedOrgs), 74, 34)
	m.members = []orgMember{
		{ID: "1", Name: "bh", Status: config.StatusActive},
		{ID: "2", Name: "bh-site", Status: config.StatusInactive},
		{ID: "3", Name: "blockhead-ops-dash", Status: config.StatusActive},
		{ID: "4", Name: "blockhead-camp-5", Status: config.StatusReference},
	}
	m.pane = paneMembers
	uitest.AssertBounded(t, m.View(), 74, 34)
	cols := map[string]int{}
	for _, ln := range strings.Split(m.renderMemberPane(m.layout()), "\n") {
		for _, mem := range m.members {
			at := strings.Index(ln, " "+mem.Name+" ")
			if at < 0 {
				continue
			}
			rest := ln[at+len(mem.Name)+1:]
			cols[mem.Name] = lipgloss.Width(ln[:at+len(mem.Name)+1]) + len(rest) - len(strings.TrimLeft(rest, " "))
			if !strings.HasPrefix(strings.TrimLeft(rest, " "), mem.Status) {
				t.Fatalf("member %q row lacks status %q: %q", mem.Name, mem.Status, ln)
			}
		}
	}
	if len(cols) != len(m.members) {
		t.Fatalf("members missing or truncated: %v", cols)
	}
	assertOneColumn(t, "member status", cols)
}
