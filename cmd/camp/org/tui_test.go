package org

import (
	"context"
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
