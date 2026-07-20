package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/machines"
	"github.com/Obedience-Corp/camp/internal/remote"
)

func fleetFile() *machines.File {
	return &machines.File{
		Version: 1,
		Machines: []machines.Machine{
			{ID: "devbox", Label: "Devbox", Host: "devbox.tailnet.ts.net", AuthMethod: machines.AuthTailscaleSSH},
			{ID: "buildbox", Label: "CI builder", Host: "10.0.0.12", AuthMethod: machines.AuthSSHAgent, SSHUser: "ci"},
		},
	}
}

// isolateMachines points machines.Load/Save at a temp file so a test can never
// read or write the developer's own ~/.obey/machines.yaml.
func isolateMachines(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "machines.yaml")
	t.Setenv("CAMP_MACHINES_PATH", path)
	return path
}

func TestFleetRowsPutLocalFirstAndSortTheRest(t *testing.T) {
	m := newMachineTUIModel(t.Context(), fleetFile())

	var ids []string
	for _, row := range m.rows {
		ids = append(ids, row.id())
	}
	want := []string{"local", "buildbox", "devbox"}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Fatalf("rows = %v, want %v", ids, want)
	}
	if !m.rows[0].Local {
		t.Error("the first row must be the local machine")
	}
}

// local is the current machine rather than a configured entry, so the actions
// that write to machines.yaml have to refuse it the way the CLI does.
func TestLocalRowRefusesEditRemoveAndSocketReset(t *testing.T) {
	m := newMachineTUIModel(t.Context(), fleetFile())
	m.cursor = 0

	for _, tc := range []struct {
		name string
		run  func()
		want string
	}{
		{"edit", func() { m.openEditForm() }, "cannot edit"},
		{"remove", func() { m.openDeleteConfirm() }, "cannot remove"},
		{"reset socket", func() { m.resetSelectedSocket() }, "no ControlMaster socket"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m.status, m.statusErr, m.overlay = "", false, machineNoOverlay
			tc.run()
			if !m.statusErr || !strings.Contains(m.status, tc.want) {
				t.Errorf("status = %q (err=%v), want an error containing %q", m.status, m.statusErr, tc.want)
			}
			if m.overlay != machineNoOverlay {
				t.Error("a refused action must not open an overlay")
			}
		})
	}
}

func TestSaveFormWritesNewMachine(t *testing.T) {
	path := isolateMachines(t)
	m := newMachineTUIModel(t.Context(), &machines.File{Version: 1})
	m.form = newMachineForm()
	m.form.input(machineFieldID).SetValue("gpu-rig")
	m.form.input(machineFieldHost).SetValue("gpu.tailnet.ts.net")
	m.form.input(machineFieldLabel).SetValue("GPU rig")
	m.form.auth = machines.AuthTailscaleSSH

	m.saveForm()

	if m.form.err != "" {
		t.Fatalf("saveForm reported %q", m.form.err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading machines.yaml: %v", err)
	}
	for _, want := range []string{"id: gpu-rig", "host: gpu.tailnet.ts.net", "auth_method: tailscale-ssh"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("machines.yaml missing %q:\n%s", want, data)
		}
	}
	if m.selectedRow().id() != "gpu-rig" {
		t.Errorf("cursor landed on %q, want the machine just saved", m.selectedRow().id())
	}
}

// Upsert is keyed on id, so an add that reuses an existing id would replace
// that entry without saying so. Editing is the explicit way to change one.
func TestSaveFormRejectsDuplicateIDOnAdd(t *testing.T) {
	isolateMachines(t)
	m := newMachineTUIModel(t.Context(), fleetFile())
	m.form = newMachineForm()
	m.form.input(machineFieldID).SetValue("devbox")
	m.form.input(machineFieldHost).SetValue("elsewhere.ts.net")

	m.saveForm()

	if !strings.Contains(m.form.err, "already exists") {
		t.Fatalf("form error = %q, want a duplicate-id message", m.form.err)
	}
	if !strings.Contains(m.form.err, "press e") {
		t.Errorf("form error = %q, want it to point at the edit path", m.form.err)
	}
	if m.form.field != machineFieldID {
		t.Errorf("focus = %v, want the id field", m.form.field)
	}
}

func TestSaveFormRejectsReservedIDAndEmptyHost(t *testing.T) {
	isolateMachines(t)

	t.Run("reserved id", func(t *testing.T) {
		m := newMachineTUIModel(t.Context(), &machines.File{Version: 1})
		m.form = newMachineForm()
		m.form.input(machineFieldID).SetValue("local")
		m.form.input(machineFieldHost).SetValue("host.ts.net")
		m.saveForm()
		if !strings.Contains(m.form.err, "reserved") {
			t.Errorf("form error = %q, want the reserved-id message", m.form.err)
		}
	})

	t.Run("empty host", func(t *testing.T) {
		m := newMachineTUIModel(t.Context(), &machines.File{Version: 1})
		m.form = newMachineForm()
		m.form.input(machineFieldID).SetValue("devbox")
		m.saveForm()
		if !strings.Contains(m.form.err, "host") {
			t.Errorf("form error = %q, want a host message", m.form.err)
		}
		if m.form.field != machineFieldHost {
			t.Errorf("focus = %v, want the host field", m.form.field)
		}
	})
}

func TestEditFormKeepsIDFixedAndSavesChanges(t *testing.T) {
	path := isolateMachines(t)
	m := newMachineTUIModel(t.Context(), fleetFile())
	if err := m.file.Save(); err != nil {
		t.Fatalf("seeding machines.yaml: %v", err)
	}
	m.cursor = 1 // buildbox

	m.openEditForm()
	if !m.form.editing() || m.form.editID != "buildbox" {
		t.Fatalf("edit form opened on %q", m.form.editID)
	}
	// The id field is skipped on edit, so the form opens on the first field
	// that can actually change.
	if m.form.field != machineFieldLabel {
		t.Errorf("edit focus = %v, want the label field", m.form.field)
	}
	m.moveFormField(-1)
	if m.form.field == machineFieldID {
		t.Error("moving back parked the cursor on the fixed id field")
	}

	m.form.input(machineFieldHost).SetValue("10.0.0.99")
	m.saveForm()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading machines.yaml: %v", err)
	}
	if !strings.Contains(string(data), "host: 10.0.0.99") {
		t.Errorf("edit did not persist:\n%s", data)
	}
	if strings.Count(string(data), "id: buildbox") != 1 {
		t.Errorf("edit duplicated the entry instead of updating it:\n%s", data)
	}
}

func TestRemovePendingDropsOnlyThatMachine(t *testing.T) {
	path := isolateMachines(t)
	m := newMachineTUIModel(t.Context(), fleetFile())
	if err := m.file.Save(); err != nil {
		t.Fatalf("seeding machines.yaml: %v", err)
	}
	m.pendingID = "devbox"

	m.removePending()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading machines.yaml: %v", err)
	}
	if strings.Contains(string(data), "id: devbox") {
		t.Errorf("devbox survived removal:\n%s", data)
	}
	if !strings.Contains(string(data), "id: buildbox") {
		t.Errorf("removal took buildbox with it:\n%s", data)
	}
}

func TestPrefillFromDeviceOpensFormWithoutSaving(t *testing.T) {
	path := isolateMachines(t)
	m := newMachineTUIModel(t.Context(), &machines.File{Version: 1})
	m.devices = []discoveredDevice{{HostName: "workstation", Host: "workstation.example-net.ts.net", DNSName: "workstation.example-net.ts.net", Online: true}}
	m.overlay = machineDiscoverOverlay

	m.prefillFromDevice()

	if m.overlay != machineFormOverlay {
		t.Fatalf("overlay = %v, want the form", m.overlay)
	}
	if got := m.form.value(machineFieldHost); got != "workstation.example-net.ts.net" {
		t.Errorf("host = %q, want the device host", got)
	}
	if m.form.auth != machines.AuthTailscaleSSH {
		t.Errorf("auth = %q, want tailscale-ssh for a discovered device", m.form.auth)
	}
	if !m.form.fromDiscovery {
		t.Error("the form should record that it came from discovery")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("picking a device wrote machines.yaml before the form was confirmed")
	}
}

func TestCycleAuthWrapsThroughEveryMethod(t *testing.T) {
	m := newMachineTUIModel(t.Context(), &machines.File{Version: 1})
	m.form = newMachineForm()

	seen := map[string]bool{}
	for range machineAuthCycle {
		seen[m.form.auth] = true
		m.cycleAuth(1)
	}
	for _, auth := range machineAuthCycle {
		if !seen[auth] {
			t.Errorf("cycling never offered %q", auth)
		}
	}
	if m.form.auth != machines.AuthSSHAgent {
		t.Errorf("a full cycle ended on %q, want the starting value", m.form.auth)
	}
}

func TestFleetRowShowsHealthAndShedsItWhenNarrow(t *testing.T) {
	m := newMachineTUIModel(t.Context(), fleetFile())
	m.health["buildbox"] = machineHealth{State: healthReachable, Version: "0.9.2"}
	row := machineRow{Machine: &m.file.Machines[0]}
	if row.id() != "buildbox" {
		row = machineRow{Machine: &m.file.Machines[1]}
	}

	wide := m.fleetRow(row, false, 40)
	if !strings.Contains(wide, "reachable") {
		t.Errorf("wide row = %q, want the health badge", wide)
	}
	narrow := m.fleetRow(row, false, 10)
	if strings.Contains(narrow, "reachable") {
		t.Errorf("narrow row = %q, want the badge dropped", narrow)
	}
	if !strings.Contains(narrow, "bu") {
		t.Errorf("narrow row = %q, want the machine id kept", narrow)
	}
}

// Every pane row has to stay inside its box: a wrapped line costs the pane a
// row it has not budgeted and pushes the bottom border off the terminal.
func TestMachinePanesStayWithinTheirRowBudget(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })

	m := newMachineTUIModel(t.Context(), &machines.File{
		Version: 1,
		Machines: []machines.Machine{
			{ID: "a-very-long-machine-identifier", Host: "a-very-long-host-name.tailnet.ts.net", AuthMethod: machines.AuthSSHAgent, IdentityFile: "/Users/someone/.ssh/a_long_identity_file_name"},
			{ID: "buildbox", Host: "10.0.0.12", AuthMethod: machines.AuthSSHPassword},
		},
	})

	for _, size := range [][2]int{{110, 26}, {90, 20}, {80, 14}, {160, 40}} {
		m.width, m.height = size[0], size[1]
		for _, cursor := range []int{0, 1, 2} {
			m.cursor = cursor
			lay := m.layout()
			for _, pane := range []struct {
				label  string
				render string
			}{
				{"fleet", m.renderFleetPane(lay)},
				{"detail", m.renderDetailPane(lay)},
			} {
				lines := strings.Split(pane.render, "\n")
				if want := lay.bodyRows + 4; len(lines) != want {
					t.Errorf("%dx%d cursor %d %s pane: %d rows, want %d", size[0], size[1], cursor, pane.label, len(lines), want)
				}
				for i, line := range lines {
					if w := lipgloss.Width(line); w > size[0] {
						t.Errorf("%dx%d %s pane row %d width %d exceeds terminal: %q", size[0], size[1], pane.label, i, w, line)
					}
				}
			}
		}
	}
}

// The socket path is rendered in a pane people screenshot and in output people
// paste into issues. An absolute path there spells out the operator's account
// name, so both surfaces abbreviate $HOME the way the rest of the pane does.
func TestSocketPathIsAbbreviatedInHumanOutput(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}

	socket := filepath.Join(home, ".obey", "ssh-ctl", "buildbox.sock")
	m := newMachineTUIModel(t.Context(), fleetFile())
	m.width, m.height = 120, 34
	m.cursor = 1

	// The path is only worth showing when the session is stuck, since that is
	// the only state where the file itself is the thing to go look at.
	for _, state := range []remote.ControlMasterState{remote.ControlNone, remote.ControlLive, remote.ControlStale} {
		m.sockets = map[string]remote.SocketDiagnosis{
			"buildbox": {MachineID: "buildbox", Socket: socket, State: state},
		}
		pane := m.renderDetailPane(m.layout())
		if strings.Contains(pane, home) {
			t.Errorf("detail pane leaked the home directory with a %q socket:\n%s", state, pane)
		}
		if state == remote.ControlStale && !strings.Contains(pane, "~/.obey/ssh-ctl/buildbox.sock") {
			t.Errorf("a stuck session should name its socket file:\n%s", pane)
		}
	}

	var table strings.Builder
	if err := renderMachineDiagnoseTable(&table, []machineDiagnoseRow{
		{ID: "buildbox", Socket: filepath.Join(home, ".obey", "ssh-ctl", "buildbox.sock"), State: "none"},
	}); err != nil {
		t.Fatalf("rendering diagnose table: %v", err)
	}
	if strings.Contains(table.String(), home) {
		t.Errorf("diagnose table leaked the home directory:\n%s", table.String())
	}
}

// An empty fleet is the state a first-time user is in, and the old screen met
// them with two empty panes and a row they could not act on. The onboarding
// screen has to say what a machine is for, show the commands it unlocks, and
// offer a way to start.
func TestOnboardingScreenExplainsAndOffersAStart(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	m := newMachineTUIModel(t.Context(), &machines.File{Version: 1})
	m.width, m.height = 100, 30
	m.tailscaleReady = false

	view := m.onboardingView()
	for _, want := range []string{
		"Work on campaigns that live on your other computers",
		"camp switch",
		"camp list --remote",
		"No machines yet",
		"Add a machine",
		"Tailscale is not installed",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("onboarding screen missing %q:\n%s", want, view)
		}
	}
}

// With tailscale present the screen's job is to show the user their own
// machines rather than tell them to go find a hostname.
func TestOnboardingListsTailnetDevices(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	m := newMachineTUIModel(t.Context(), &machines.File{Version: 1})
	m.width, m.height = 100, 30
	m.tailscaleReady = true
	m.devices = []discoveredDevice{
		{HostName: "workstation", Host: "workstation.example-net.ts.net", Online: true},
		{HostName: "buildfarm", Host: "buildfarm.example-net.ts.net", Online: false},
	}

	view := m.onboardingView()
	for _, want := range []string{"workstation.example-net.ts.net", "buildfarm.example-net.ts.net", "online", "offline", "enter"} {
		if !strings.Contains(view, want) {
			t.Errorf("device list missing %q:\n%s", want, view)
		}
	}
}

// A device already in the fleet has to be marked, or the same machine gets
// added twice under two ids.
func TestDeviceRowsMarkAlreadyConfiguredHosts(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	m := newMachineTUIModel(t.Context(), &machines.File{
		Version:  1,
		Machines: []machines.Machine{{ID: "devbox", Host: "devbox.example-net.ts.net"}},
	})
	m.devices = []discoveredDevice{
		{HostName: "devbox", Host: "devbox.example-net.ts.net", Online: true},
		{HostName: "gpu-box", Host: "gpu-box.example-net.ts.net", Online: true},
	}

	rows := strings.Join(m.deviceRows(70), "\n")
	if !strings.Contains(rows, "already added as devbox") {
		t.Errorf("configured device not marked:\n%s", rows)
	}
	if strings.Count(rows, "already added") != 1 {
		t.Errorf("only the configured device should be marked:\n%s", rows)
	}
}

func TestTestSelectedRefusesLocal(t *testing.T) {
	m := newMachineTUIModel(t.Context(), fleetFile())
	m.cursor = 0

	if cmd := m.testSelected(); cmd != nil {
		t.Fatal("testing local should not run a command")
	}
	if !m.statusErr || !strings.Contains(m.status, "nothing to connect to") {
		t.Errorf("status = %q, want a refusal naming local", m.status)
	}
}

func TestHealthBadgeAndStatusWording(t *testing.T) {
	if _, label, _ := healthBadge(healthReachable); label != "reachable" {
		t.Errorf("reachable label = %q", label)
	}
	if _, label, _ := healthBadge(healthUntested); label != "not tested" {
		t.Errorf("untested label = %q", label)
	}

	got := healthStatusLine("devbox", machineHealth{State: healthReachable, Version: "0.9.2"})
	if !strings.Contains(got, "reachable") || !strings.Contains(got, "0.9.2") {
		t.Errorf("reachable status = %q", got)
	}
	got = healthStatusLine("devbox", machineHealth{State: healthUnreachable, Detail: "timed out"})
	if !strings.Contains(got, "could not reach devbox") || !strings.Contains(got, "timed out") {
		t.Errorf("unreachable status = %q", got)
	}
}

func TestParseRemoteVersionAndFailureDetail(t *testing.T) {
	if got := parseRemoteVersion("camp version 0.9.2 (built 2026-01-01, commit abc)"); got != "0.9.2" {
		t.Errorf("parseRemoteVersion = %q, want 0.9.2", got)
	}
	if got := parseRemoteVersion("something unexpected"); got != "something unexpected" {
		t.Errorf("parseRemoteVersion fallback = %q", got)
	}

	err := camperrors.New(`command "ssh ci@10.0.0.12" exited with code 255: ssh: connect to host 10.0.0.12 port 22: Operation timed out`)
	if got := connectionFailureDetail(err); got != "connect to host 10.0.0.12 port 22: Operation timed out" {
		t.Errorf("connectionFailureDetail = %q", got)
	}
}

func TestHealthDetailLinesWrapsTailscaleURL(t *testing.T) {
	// Em dash is multi-byte; wrapping must stay on rune boundaries and honor
	// display width (not raw byte length).
	detail := "Tailscale SSH requires a one-time browser check — open https://login.tailscale.com/a/testhashlongtoken, approve, then retry"
	lines := healthDetailLines(detail, 40, true)
	if len(lines) < 2 {
		t.Fatalf("expected wrap into multiple lines, got %v", lines)
	}
	joined := strings.Join(lines, "")
	if !strings.Contains(joined, "https://login.tailscale.com/a/testhashlongtoken") {
		t.Errorf("wrapped lines lost URL: %v", lines)
	}
	if !strings.Contains(joined, "—") {
		t.Errorf("em dash corrupted by wrap: %v", lines)
	}
	for _, line := range lines {
		if !utf8.ValidString(line) {
			t.Errorf("invalid UTF-8 line: %q", line)
		}
		if w := lipgloss.Width(line); w > 40 {
			t.Errorf("line display width %d > 40: %q", w, line)
		}
	}
	// Non-URL details still truncate to one line.
	got := healthDetailLines("operation timed out waiting for peer", 20, false)
	if len(got) != 1 || lipgloss.Width(got[0]) > 20 {
		t.Errorf("truncate path = %v", got)
	}
}

func TestHealthSectionTailscaleCheckHeadline(t *testing.T) {
	m := newMachineTUIModel(t.Context(), fleetFile())
	m.health["devbox"] = machineHealth{
		State:  healthUnreachable,
		Detail: "Tailscale SSH requires a one-time browser check — open https://login.tailscale.com/a/x, approve, then retry",
	}
	// healthSection for unreachable with tailscale detail
	// Find devbox id index - fleet has devbox first remote; health map is by id
	lines := m.healthSection("devbox", 36)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Needs Tailscale SSH check") {
		t.Errorf("headline missing check framing: %q", joined)
	}
	if strings.Contains(joined, "Could not reach it") {
		t.Errorf("still uses network-unreachable headline: %q", joined)
	}
	if !strings.Contains(joined, "login.tailscale.com") {
		t.Errorf("URL missing from pane: %q", joined)
	}
}

func TestConnectionFailureDetailSurfacesTailscaleCheck(t *testing.T) {
	stderr := "# Tailscale SSH requires an additional check.\n# To authenticate, visit: https://login.tailscale.com/a/testhash\n"
	err := camperrors.NewCommand("ssh lance@archdtop", 255, stderr, nil)
	// Even raw CommandError stderr (pre-annotation path) should surface the URL.
	got := connectionFailureDetail(err)
	if !strings.Contains(got, "https://login.tailscale.com/a/testhash") {
		t.Errorf("connectionFailureDetail missing check URL: %q", got)
	}
	if !strings.Contains(got, "browser check") {
		t.Errorf("connectionFailureDetail missing guidance: %q", got)
	}

	// Timeout wrap path: context deadline must not hide the URL.
	timeoutErr := camperrors.Wrapf(context.DeadlineExceeded,
		"%s (while connecting to lance@archdtop)",
		"Tailscale SSH requires a one-time browser check — open https://login.tailscale.com/a/testhash, approve, then retry (camp cannot complete this interactively)")
	got = connectionFailureDetail(timeoutErr)
	if !strings.Contains(got, "https://login.tailscale.com/a/testhash") {
		t.Errorf("timeout wrap detail missing URL: %q", got)
	}
	if strings.Contains(got, "context deadline exceeded") {
		t.Errorf("detail should strip deadline noise: %q", got)
	}
}

// Exit 127 is the camp-not-found case RunCampCommand wraps with login-shell
// PATH context and CAMP_REMOTE_CAMP_PATH. The detail must keep that guidance;
// stripping down to bare "command not found" is the regression this guards.
func TestConnectionFailureDetailPreservesCampNotFoundHint(t *testing.T) {
	inner := camperrors.NewCommand("ssh buildbox -- camp --version", 127, "bash: camp: command not found", nil)
	err := camperrors.Wrapf(inner,
		"remote camp not found on buildbox (tried %q via sh -lc, i.e. the machine's login-shell PATH); "+
			"if camp lives outside that PATH, set %s to its exact path on that machine",
		"camp", remote.RemoteCampPathEnv)

	got := connectionFailureDetail(err)
	if !strings.Contains(got, remote.RemoteCampPathEnv) {
		t.Errorf("connectionFailureDetail dropped CAMP_REMOTE_CAMP_PATH guidance: %q", got)
	}
	if !strings.Contains(got, "remote camp not found") {
		t.Errorf("connectionFailureDetail dropped outer camp-not-found wrap: %q", got)
	}
	if got == "bash: camp: command not found" {
		t.Errorf("connectionFailureDetail reduced exit-127 to bare stderr: %q", got)
	}
}

// Removing the last configured machine lands mid-session on the empty
// onboarding screen. That transition must start a scan the way Init does;
// otherwise the body claims "no other devices" without ever looking.
func TestRemoveLastMachineStartsOnboardingScan(t *testing.T) {
	isolateMachines(t)

	m := newMachineTUIModel(t.Context(), &machines.File{
		Version:  1,
		Machines: []machines.Machine{{ID: "only", Host: "only.example-net.ts.net"}},
	})
	if err := m.file.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	m.tailscaleReady = true
	m.pendingID = "only"

	cmd := m.removePending()
	if cmd == nil {
		t.Fatal("removing the last machine with tailscale ready must start a scan")
	}
	if !m.empty() {
		t.Fatal("fleet should be empty after removing the only machine")
	}
	if !m.scanning {
		t.Error("onboarding transition must set scanning")
	}
	if m.scanGen == 0 {
		t.Error("beginScan must bump scanGen")
	}
}

// Enter on a device already in the fleet must refuse, not open a second form
// for the same host under a new id.
func TestPrefillFromDeviceRefusesAlreadyConfiguredHost(t *testing.T) {
	m := newMachineTUIModel(t.Context(), &machines.File{
		Version:  1,
		Machines: []machines.Machine{{ID: "devbox", Host: "devbox.example-net.ts.net"}},
	})
	m.devices = []discoveredDevice{
		{HostName: "devbox", Host: "devbox.example-net.ts.net", Online: true},
	}
	m.deviceCursor = 0

	if cmd := m.prefillFromDevice(); cmd != nil {
		t.Fatal("prefill of an already-configured host must not open the form")
	}
	if m.overlay != machineNoOverlay {
		t.Errorf("overlay = %v, want none", m.overlay)
	}
	if !strings.Contains(m.status, "already added as devbox") {
		t.Errorf("status = %q, want already-added guidance", m.status)
	}
}

// When tailscale is ready the footer must list s whenever the body does,
// including failed and empty-device scans (not only when devices are present).
func TestOnboardingFooterListsRescanWhenTailscaleReady(t *testing.T) {
	m := newMachineTUIModel(t.Context(), &machines.File{Version: 1})
	m.tailscaleReady = true
	m.devices = nil
	m.scanErr = "tailscale: failed to connect"

	footer := m.onboardingFooter()
	if !strings.Contains(footer, "s rescan") {
		t.Errorf("footer with scan error missing rescan: %q", footer)
	}

	m.scanErr = ""
	footer = m.onboardingFooter()
	if !strings.Contains(footer, "s rescan") {
		t.Errorf("footer with empty devices missing rescan: %q", footer)
	}

	m.tailscaleReady = false
	footer = m.onboardingFooter()
	if strings.Contains(footer, "s rescan") {
		t.Errorf("footer without tailscale should not list rescan: %q", footer)
	}
}

// A late finish from an earlier scan must not overwrite a newer result.
func TestApplyDevicesDropsStaleGeneration(t *testing.T) {
	m := newMachineTUIModel(t.Context(), &machines.File{Version: 1})
	m.scanGen = 2
	m.scanning = true
	m.devices = []discoveredDevice{{HostName: "fresh", Host: "fresh.ts.net", Online: true}}

	_, _ = m.applyDevices(devicesMsg{
		gen:     1,
		devices: []discoveredDevice{{HostName: "stale", Host: "stale.ts.net", Online: true}},
	})
	if !m.scanning {
		t.Error("stale result must not clear scanning for the in-flight scan")
	}
	if len(m.devices) != 1 || m.devices[0].HostName != "fresh" {
		t.Errorf("stale result clobbered devices: %+v", m.devices)
	}

	_, _ = m.applyDevices(devicesMsg{
		gen:     2,
		devices: []discoveredDevice{{HostName: "current", Host: "current.ts.net", Online: true}},
	})
	if m.scanning {
		t.Error("matching gen must clear scanning")
	}
	if len(m.devices) != 1 || m.devices[0].HostName != "current" {
		t.Errorf("current result not applied: %+v", m.devices)
	}
}
