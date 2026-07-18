package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/Obedience-Corp/camp/internal/machines"
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
	m.devices = []discoveredDevice{{HostName: "archdtop", Host: "archdtop.tail37114b.ts.net", DNSName: "archdtop.tail37114b.ts.net", Online: true}}
	m.overlay = machineDiscoverOverlay

	m.prefillFromDevice()

	if m.overlay != machineFormOverlay {
		t.Fatalf("overlay = %v, want the form", m.overlay)
	}
	if got := m.form.value(machineFieldHost); got != "archdtop.tail37114b.ts.net" {
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

func TestMachineRowTextShedsBadgeBeforeTruncatingID(t *testing.T) {
	row := machineRow{Machine: &machines.Machine{ID: "buildbox"}}
	if got := machineRowText(row, "live", 20); got != "buildbox · live" {
		t.Errorf("wide row = %q", got)
	}
	if got := machineRowText(row, "live", 10); got != "buildbox" {
		t.Errorf("narrow row = %q, want the badge dropped", got)
	}
	if got := machineRowText(row, "live", 5); got != "bu..." {
		t.Errorf("very narrow row = %q", got)
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
