package org

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Obedience-Corp/camp/internal/config"
)

func newTestOrgModel(t *testing.T) orgTUIModel {
	t.Helper()
	setOrgRegistry(t, orgFixture)
	reg, err := config.LoadRegistry(context.Background())
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	return newOrgTUIModel(context.Background(), reg)
}

func key(m orgTUIModel, s string) orgTUIModel {
	var msg tea.KeyMsg
	switch s {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
	next, _ := m.Update(msg)
	return next.(orgTUIModel)
}

func TestOrgTUI_OpensFallbackFirst(t *testing.T) {
	m := newTestOrgModel(t)
	if len(m.orgs) == 0 || m.orgs[0].Org != "default" {
		t.Fatalf("orgs = %v, want default first", m.orgs)
	}
	if m.focusedOrg != "default" {
		t.Errorf("focusedOrg = %q, want default", m.focusedOrg)
	}
	if len(m.members) != 1 || m.members[0].Name != "alpha" {
		t.Errorf("default members = %v, want [alpha]", m.members)
	}
}

func TestOrgTUI_NavigateAndEnter(t *testing.T) {
	m := newTestOrgModel(t)
	m = key(m, "j")
	if m.focusedOrg != "obey" {
		t.Fatalf("after j, focusedOrg = %q, want obey", m.focusedOrg)
	}
	m = key(m, "enter")
	if m.pane != paneMembers {
		t.Error("enter should focus the member pane")
	}
	m = key(m, "esc")
	if m.pane != paneOrgs {
		t.Error("esc should return to the org pane")
	}
}

func TestOrgTUI_MoveMember(t *testing.T) {
	m := newTestOrgModel(t)
	m = key(m, "enter") // default -> members (alpha)
	m = key(m, "m")
	if m.overlay != overlayMove {
		t.Fatal("expected move overlay")
	}
	m = key(m, "newco")
	m = key(m, "enter")
	if m.overlay != overlayNone {
		t.Error("overlay should close after commit")
	}
	if m.statusErr {
		t.Fatalf("unexpected error: %s", m.status)
	}
	if got := orgOf(t, "A-1"); got != "newco" {
		t.Errorf("alpha org = %q, want newco", got)
	}
}

func TestOrgTUI_MoveToTypedNewOrg_Appears(t *testing.T) {
	m := newTestOrgModel(t)
	m = key(m, "enter")
	m = key(m, "m")
	m = key(m, "fresh")
	m = key(m, "enter")
	found := false
	for _, o := range m.orgs {
		if o.Org == "fresh" {
			found = true
		}
	}
	if !found {
		t.Errorf("new org 'fresh' not present after move: %v", m.orgs)
	}
}

func TestOrgTUI_CreateOrg(t *testing.T) {
	m := newTestOrgModel(t)
	m = key(m, "enter") // default -> members (alpha)
	m = key(m, "c")
	if m.overlay != overlayCreate {
		t.Fatal("expected create overlay")
	}
	m = key(m, "fresh")
	m = key(m, "enter")
	if m.statusErr {
		t.Fatalf("unexpected error: %s", m.status)
	}
	if got := orgOf(t, "A-1"); got != "fresh" {
		t.Errorf("alpha org = %q, want fresh", got)
	}
	found := false
	for _, o := range m.orgs {
		if o.Org == "fresh" {
			found = true
		}
	}
	if !found {
		t.Errorf("new org 'fresh' not present after create: %v", m.orgs)
	}
}

func TestOrgTUI_ReturnToDefault(t *testing.T) {
	m := newTestOrgModel(t)
	m = key(m, "j")     // obey
	m = key(m, "enter") // members: beta, gamma
	m = key(m, "d")     // return beta to default
	if m.statusErr {
		t.Fatalf("unexpected error: %s", m.status)
	}
	if got := orgOf(t, "B-2"); got != "default" {
		t.Errorf("beta org = %q, want default", got)
	}
}

func TestOrgTUI_RenameOrg(t *testing.T) {
	m := newTestOrgModel(t)
	m = key(m, "j") // obey
	m = key(m, "r")
	if m.overlay != overlayRename {
		t.Fatal("expected rename overlay")
	}
	m = key(m, "obedience")
	m = key(m, "enter")
	if m.statusErr {
		t.Fatalf("unexpected error: %s", m.status)
	}
	if got := orgOf(t, "B-2"); got != "obedience" {
		t.Errorf("beta org = %q, want obedience", got)
	}
}

func TestOrgTUI_RenameError_SurfacedNoMutation(t *testing.T) {
	m := newTestOrgModel(t)
	m = key(m, "j") // obey
	m = key(m, "r")
	m = key(m, "obey") // renaming to the same name is rejected
	m = key(m, "enter")
	if !m.statusErr {
		t.Errorf("expected error status surfaced, got status=%q", m.status)
	}
	if got := orgOf(t, "B-2"); got != "obey" {
		t.Errorf("beta org = %q, want obey (unchanged after error)", got)
	}
}

func TestOrgTUI_Quit(t *testing.T) {
	m := newTestOrgModel(t)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if !next.(orgTUIModel).quitting {
		t.Error("q should set quitting")
	}
	if cmd == nil {
		t.Error("q should return a quit command")
	}
}

func TestOrgTUI_ViewSmoke(t *testing.T) {
	m := newTestOrgModel(t)
	m.width = 100
	out := m.View()
	for _, want := range []string{"Orgs", "default", "obey", "rename"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q:\n%s", want, out)
		}
	}
	m.width = 40 // below the single-pane floor must still render
	if m.View() == "" {
		t.Error("narrow view should not be empty")
	}
}

func TestOrgTUI_CreateEmptyOrg(t *testing.T) {
	// 08-T1
	m := newTestOrgModel(t)
	m = key(m, "n")
	if m.overlay != overlayCreateEmpty {
		t.Fatalf("overlay = %v, want create empty", m.overlay)
	}
	m = key(m, "client-acme")
	m = key(m, "enter")
	if m.statusErr {
		t.Fatalf("error: %s", m.status)
	}
	if m.overlay != overlayNone {
		t.Fatal("overlay should close")
	}
	found := false
	for _, o := range m.orgs {
		if o.Org == "client-acme" && o.Campaigns == 0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("empty org not listed: %v", m.orgs)
	}
}

func TestOrgTUI_CreateEmptyOrg_InvalidKeepsOverlay(t *testing.T) {
	m := newTestOrgModel(t)
	m = key(m, "n")
	m = key(m, "Bad Name")
	m = key(m, "enter")
	if !m.statusErr {
		t.Fatal("expected error for invalid name")
	}
	if m.overlay != overlayCreateEmpty {
		t.Fatalf("overlay should stay open, got %v", m.overlay)
	}
	m = key(m, "esc")
	if m.overlay != overlayNone {
		t.Fatal("esc should cancel")
	}
}

func TestOrgTUI_DeleteEmptyOrg_Confirm(t *testing.T) {
	// seed empty org
	setOrgRegistry(t, fixtureWithEmptyOrg)
	reg, err := config.LoadRegistry(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	m := newOrgTUIModel(context.Background(), reg)

	// find client-acme cursor
	for i, o := range m.orgs {
		if o.Org == "client-acme" {
			m.orgCursor = i
			m.syncFocusedOrg()
			break
		}
	}
	m = key(m, "x")
	if m.overlay != overlayConfirmDelete {
		t.Fatalf("overlay = %v, want confirm delete", m.overlay)
	}
	// before confirm, org still present
	regMid, _ := config.LoadRegistry(context.Background())
	if !orgExists(regMid, "client-acme") {
		t.Fatal("org deleted before confirm")
	}
	m = key(m, "y")
	if m.statusErr {
		t.Fatalf("delete error: %s", m.status)
	}
	regAfter, _ := config.LoadRegistry(context.Background())
	if orgExists(regAfter, "client-acme") {
		t.Fatal("org still present after confirm")
	}
}

func TestOrgTUI_DeleteGuards(t *testing.T) {
	// 08-T3
	m := newTestOrgModel(t)
	// fallback first
	m = key(m, "x")
	if !m.statusErr || !strings.Contains(m.status, "fallback") {
		t.Fatalf("expected fallback guard, status=%q", m.status)
	}
	// non-empty obey
	m = key(m, "j") // obey
	before, _ := config.LoadRegistry(context.Background())
	m = key(m, "x")
	if !m.statusErr || !strings.Contains(m.status, "member") {
		t.Fatalf("expected members guard, status=%q", m.status)
	}
	after, _ := config.LoadRegistry(context.Background())
	if len(before.Orgs) != len(after.Orgs) {
		t.Fatal("registry mutated on guarded delete")
	}
}

func TestOrgTUI_NewCampaign_CallsSeam(t *testing.T) {
	// 08-T4
	var gotName, gotOrg string
	prev := createCampaignInOrg
	createCampaignInOrg = func(ctx context.Context, name, org string) error {
		gotName, gotOrg = name, org
		return nil
	}
	t.Cleanup(func() { createCampaignInOrg = prev })

	m := newTestOrgModel(t)
	m = key(m, "j") // obey
	m = key(m, "N")
	if m.overlay != overlayNewCampaign {
		t.Fatalf("overlay = %v", m.overlay)
	}
	if m.pendingOrg != "obey" {
		t.Fatalf("pendingOrg = %q", m.pendingOrg)
	}
	m = key(m, "new-demo")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(orgTUIModel)
	if cmd == nil {
		t.Fatal("expected async create cmd")
	}
	// Run the cmd synchronously
	msg := cmd()
	next, _ = m.Update(msg)
	m = next.(orgTUIModel)
	if gotName != "new-demo" || gotOrg != "obey" {
		t.Fatalf("seam called with name=%q org=%q", gotName, gotOrg)
	}
	if m.statusErr {
		t.Fatalf("status error: %s", m.status)
	}
}

func TestDefaultCreateCampaignInOrgRejectsPathLikeNames(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "campaigns")
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	t.Setenv("CAMP_REGISTRY_PATH", filepath.Join(dir, "registry.json"))

	cases := []struct {
		name       string
		shouldMiss string
	}{
		{name: "../outside", shouldMiss: filepath.Join(dir, "outside")},
		{name: "nested/name", shouldMiss: filepath.Join(base, "nested")},
		{name: `nested\name`, shouldMiss: filepath.Join(base, `nested\name`)},
		{name: ".hidden", shouldMiss: filepath.Join(base, ".hidden")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := defaultCreateCampaignInOrg(context.Background(), tc.name, "obey")
			if err == nil {
				t.Fatalf("defaultCreateCampaignInOrg(%q) = nil, want validation error", tc.name)
			}
			if _, statErr := os.Stat(tc.shouldMiss); !os.IsNotExist(statErr) {
				t.Fatalf("invalid name %q created %s (stat err=%v)", tc.name, tc.shouldMiss, statErr)
			}
		})
	}
	if _, statErr := os.Stat(base); !os.IsNotExist(statErr) {
		t.Fatalf("invalid names should not create campaigns base %s (stat err=%v)", base, statErr)
	}
	reg, err := config.LoadRegistry(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Campaigns) != 0 {
		t.Fatalf("invalid names wrote registry campaigns: %d", len(reg.Campaigns))
	}
}

func TestOrgTUI_HelpAdvertisesNewKeys(t *testing.T) {
	m := newTestOrgModel(t)
	m.width = 120
	out := m.View()
	for _, want := range []string{"n: new org", "N: new campaign", "x: delete"} {
		if !strings.Contains(out, want) {
			t.Errorf("help missing %q:\n%s", want, out)
		}
	}
}
