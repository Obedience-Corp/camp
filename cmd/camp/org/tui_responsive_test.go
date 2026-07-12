package org

import (
	"fmt"
	"strings"
	"testing"

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
