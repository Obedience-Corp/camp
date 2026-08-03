package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/machines"
)

func key(s string) tea.KeyMsg {
	if len(s) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	default:
		panic("unmapped key " + s)
	}
}

// hopReadyModel is a fleet screen with shell integration present and the cursor
// on a configured machine, which is the state every hop assertion starts from.
func hopReadyModel(t *testing.T) *machineTUIModel {
	t.Helper()
	m := newMachineTUIModel(t.Context(), fleetFile())
	m.hopEnabled = true
	m.cursor = 1 // buildbox: ssh-agent auth, so EnsureKeyAuth passes
	return m
}

// stubRemoteCampaigns points the live fetch at a fake so no test dials ssh.
func stubRemoteCampaigns(t *testing.T, names []string, err error) {
	t.Helper()
	prev := listRemoteCampaignsFor
	listRemoteCampaignsFor = func(context.Context, *machines.Machine) ([]string, error) {
		return names, err
	}
	t.Cleanup(func() { listRemoteCampaignsFor = prev })
}

// isolateMachineCache redirects the completion cache into a temp dir so these
// tests can neither read a developer's warm cache (which would make a
// cold-cache assertion pass for the wrong reason) nor write into the real
// ~/.obey. XDG_CONFIG_HOME is set explicitly rather than relying on the HOME
// fallback: machineCacheDir prefers XDG when it is set, so a machine that
// exports it would otherwise route these writes straight at the real cache.
func isolateMachineCache(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	return home
}

// The hop is the whole point of the key: the payload has to be the same
// ssh-hop vocabulary `camp list` already writes, because one shell arm reads both.
func TestHopSelectionUsesTheSharedSSHHopVocabulary(t *testing.T) {
	got := machineHopSelection("buildbox", "notes")
	if want := "ssh-hop:buildbox:notes"; got != want {
		t.Fatalf("machineHopSelection = %q, want %q", got, want)
	}
	if got != gotoSelectionFor(campaignEntry{Machine: "buildbox", Name: "notes"}) {
		t.Error("the machine screen and the list screen must emit an identical payload")
	}
}

// Without the wrapper the key cannot finish, so it must say so rather than
// appear to work. This is the failure an operator hits on a fresh install.
func TestHopWithoutShellIntegrationRefusesAndNamesTheFix(t *testing.T) {
	m := hopReadyModel(t)
	m.hopEnabled = false

	m.openHopPicker()

	if m.overlay == machineHopOverlay {
		t.Error("the campaign picker must not open when the hop cannot be completed")
	}
	if m.statusKind != statusError {
		t.Errorf("statusKind = %v, want statusError", m.statusKind)
	}
	if !strings.Contains(m.status, "camp shell-init") {
		t.Errorf("status = %q, want it to name the shell-init fix", m.status)
	}
}

// local is this computer. Offering a campaign list for it would produce an ssh
// hop to itself, which is the one machine an operator is guaranteed to be at.
func TestHopRefusesTheLocalRow(t *testing.T) {
	m := hopReadyModel(t)
	m.cursor = 0

	m.openHopPicker()

	if m.overlay == machineHopOverlay {
		t.Error("local must not open the campaign picker")
	}
	if m.statusKind != statusError {
		t.Errorf("statusKind = %v, want statusError", m.statusKind)
	}
}

// Refuse before opening, not after choosing: a password-auth machine can never
// be hopped to, so a list of its campaigns leads nowhere.
func TestHopRefusesPasswordAuthBeforeOpeningTheList(t *testing.T) {
	m := newMachineTUIModel(t.Context(), &machines.File{
		Version:  1,
		Machines: []machines.Machine{{ID: "pw", Host: "pw.example", AuthMethod: machines.AuthSSHPassword}},
	})
	m.hopEnabled = true
	m.cursor = 1

	m.openHopPicker()

	if m.overlay == machineHopOverlay {
		t.Error("a password-auth machine must not open the campaign picker")
	}
	if !strings.Contains(m.status, "password-auth") {
		t.Errorf("status = %q, want it to name password auth", m.status)
	}
}

// The warm cache is what makes the picker open instantly. A cache hit must not
// dial ssh — that is the difference between a keystroke and an eight-second wait.
func TestHopOpensFromCacheWithoutDialing(t *testing.T) {
	isolateMachineCache(t)
	writeMachineCacheCampaigns("buildbox", []string{"notes", "platform"})

	dialed := false
	prev := listRemoteCampaignsFor
	listRemoteCampaignsFor = func(context.Context, *machines.Machine) ([]string, error) {
		dialed = true
		return nil, nil
	}
	t.Cleanup(func() { listRemoteCampaignsFor = prev })

	m := hopReadyModel(t)
	_, cmd := m.openHopPicker()

	if m.overlay != machineHopOverlay {
		t.Fatal("a cache hit must open the campaign picker")
	}
	if cmd != nil {
		t.Error("a cache hit must not schedule a fetch")
	}
	if dialed {
		t.Error("a cache hit must not dial the machine")
	}
	if !m.hop.cached {
		t.Error("the picker must record that the list came from the snapshot")
	}
	if strings.Join(m.hop.campaigns, ",") != "notes,platform" {
		t.Errorf("campaigns = %v, want [notes platform]", m.hop.campaigns)
	}
}

// A cold cache is the only case that pays for ssh, and it must actually pay
// rather than showing an empty list as if the machine had no campaigns.
func TestHopFetchesWhenTheCacheIsCold(t *testing.T) {
	isolateMachineCache(t)
	stubRemoteCampaigns(t, []string{"remote-only"}, nil)

	m := hopReadyModel(t)
	_, cmd := m.openHopPicker()

	if m.overlay != machineHopOverlay {
		t.Fatal("a cold cache must still open the picker")
	}
	if !m.hop.loading {
		t.Error("a cold cache must show the fetch as in flight")
	}
	if cmd == nil {
		t.Fatal("a cold cache must schedule a fetch")
	}

	m.applyHopCampaigns(hopCampaignsMsg{id: "buildbox", campaigns: []string{"remote-only"}, gen: m.hop.gen})
	if m.hop.loading {
		t.Error("the fetch result must clear the loading state")
	}
	if m.hop.cached {
		t.Error("a live result must not be labelled as a snapshot")
	}
	if strings.Join(m.hop.campaigns, ",") != "remote-only" {
		t.Errorf("campaigns = %v, want [remote-only]", m.hop.campaigns)
	}
}

// Choosing a campaign is what actually produces the hop, and it must quit so the
// wrapper gets the file. A selection that did not quit would strand the operator.
func TestChoosingACampaignEmitsTheHopAndQuits(t *testing.T) {
	isolateMachineCache(t)
	writeMachineCacheCampaigns("buildbox", []string{"notes", "platform"})

	m := hopReadyModel(t)
	m.openHopPicker()
	m.updateHop(key("down"))
	_, cmd := m.updateHop(key("enter"))

	if want := "ssh-hop:buildbox:platform"; m.hopSelection != want {
		t.Fatalf("hopSelection = %q, want %q", m.hopSelection, want)
	}
	if cmd == nil {
		t.Fatal("choosing a campaign must quit so the wrapper can act on the file")
	}
	if msg := cmd(); msg != tea.Quit() {
		t.Errorf("expected tea.Quit, got %T", msg)
	}
}

// Quitting without choosing must leave the file untouched, or the wrapper would
// hop somewhere the operator backed out of.
func TestCancellingTheHopWritesNothing(t *testing.T) {
	isolateMachineCache(t)
	writeMachineCacheCampaigns("buildbox", []string{"notes"})

	m := hopReadyModel(t)
	m.openHopPicker()
	m.updateHop(key("esc"))

	if m.hopSelection != "" {
		t.Errorf("hopSelection = %q, want empty after cancel", m.hopSelection)
	}
	if m.overlay != machineNoOverlay {
		t.Error("esc must close the picker")
	}

	out := filepath.Join(t.TempDir(), "hop")
	if err := writeMachineHopSelection(m, out); err != nil {
		t.Fatalf("writeMachineHopSelection: %v", err)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Error("a cancelled hop must not create the path-output file")
	}
}

func TestWriteHopSelectionPersistsTheChosenPayload(t *testing.T) {
	m := hopReadyModel(t)
	m.hopSelection = "ssh-hop:buildbox:notes"
	out := filepath.Join(t.TempDir(), "hop")

	if err := writeMachineHopSelection(m, out); err != nil {
		t.Fatalf("writeMachineHopSelection: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read path-output: %v", err)
	}
	if string(data) != "ssh-hop:buildbox:notes" {
		t.Errorf("path-output = %q, want the ssh-hop payload", data)
	}
}

// A fetch that lands after its overlay closed (or was re-pointed at another
// machine) must be dropped, or a slow devbox answer overwrites the buildbox list
// the operator is looking at.
func TestStaleFetchResultIsDropped(t *testing.T) {
	isolateMachineCache(t)
	stubRemoteCampaigns(t, []string{"late"}, nil)

	m := hopReadyModel(t)
	m.openHopPicker()
	stale := m.hop.gen

	m.updateHop(key("esc"))
	m.openHopPicker()

	m.applyHopCampaigns(hopCampaignsMsg{id: "buildbox", campaigns: []string{"late"}, gen: stale})
	if strings.Join(m.hop.campaigns, ",") == "late" {
		t.Error("a superseded fetch must not populate the reopened picker")
	}
}

// An unreachable machine must explain and offer the diagnostic, not show an
// empty list that reads as "this machine has no campaigns".
func TestUnreachableMachineReportsRatherThanShowingAnEmptyList(t *testing.T) {
	isolateMachineCache(t)
	stubRemoteCampaigns(t, nil, camperrors.New("ssh: connect to host buildbox port 22: no route to host"))

	m := hopReadyModel(t)
	m.openHopPicker()
	m.applyHopCampaigns(hopCampaignsMsg{
		id:  "buildbox",
		err: camperrors.New("ssh: connect to host buildbox port 22: no route to host"),
		gen: m.hop.gen,
	})

	if m.hop.err == "" {
		t.Fatal("an unreachable machine must record the failure")
	}
	body := strings.Join(m.hopBody(), "\n")
	if !strings.Contains(body, "camp machine diagnose buildbox") {
		t.Errorf("hop body must point at the diagnostic, got:\n%s", body)
	}
}

// A failed refresh replaces the list with the error screen, so the list must
// really be gone: enter (and j/k) acting on a stale invisible list would hop
// from a failure screen to a machine that just proved unreachable.
func TestFailedRefreshClearsTheListSoEnterCannotHop(t *testing.T) {
	isolateMachineCache(t)
	writeMachineCacheCampaigns("buildbox", []string{"notes", "platform"})

	m := hopReadyModel(t)
	m.openHopPicker()
	m.updateHop(key("down"))
	m.updateHop(key("r"))
	m.applyHopCampaigns(hopCampaignsMsg{
		id:  "buildbox",
		err: camperrors.New("ssh: connect to host buildbox port 22: no route to host"),
		gen: m.hop.gen,
	})

	if len(m.hop.campaigns) != 0 {
		t.Fatalf("campaigns = %v, want the stale list cleared alongside the error", m.hop.campaigns)
	}
	_, cmd := m.updateHop(key("enter"))
	if m.hopSelection != "" {
		t.Fatalf("hopSelection = %q, want no hop from a failure screen", m.hopSelection)
	}
	if cmd != nil {
		t.Fatal("enter on a failure screen must not quit into a hop")
	}
}

// t and enter are different gestures now. Regression guard: enter used to test
// the connection, and rebinding it must not have taken the tester with it.
func TestTestConnectionStillHasItsOwnKey(t *testing.T) {
	isolateMachines(t)
	m := hopReadyModel(t)

	m.updateBrowse(key("t"))
	if m.health["buildbox"].State != healthTesting {
		t.Error("t must still start a connection test")
	}
	if m.overlay == machineHopOverlay {
		t.Error("t must not open the hop picker")
	}
}
