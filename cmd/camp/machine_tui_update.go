package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/machines"
	"github.com/Obedience-Corp/camp/internal/remote"
)

func (m *machineTUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case socketsMsg:
		m.sockets = msg
		return m, nil
	case devicesMsg:
		return m.applyDevices(msg)
	case spinner.TickMsg:
		if !m.busy() {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case healthMsg:
		m.health[msg.id] = msg.health
		m.setStatus(healthStatusLine(msg.id, msg.health))
		m.statusErr = msg.health.State != healthReachable
		return m, nil
	case tea.KeyMsg:
		if m.overlay != machineNoOverlay {
			return m.updateOverlay(msg)
		}
		if m.empty() {
			return m.updateOnboarding(msg)
		}
		return m.updateBrowse(msg)
	}
	return m, nil
}

func healthStatusLine(id string, health machineHealth) string {
	switch health.State {
	case healthReachable:
		if health.Version != "" {
			return id + " is reachable · camp " + health.Version
		}
		return id + " is reachable"
	case healthUnsupported:
		return health.Detail
	default:
		return "could not reach " + id + ": " + health.Detail
	}
}

func (m *machineTUIModel) applyDevices(msg devicesMsg) (tea.Model, tea.Cmd) {
	// Drop results from a scan that was superseded by a later beginScan.
	if msg.gen != m.scanGen {
		return m, nil
	}
	m.scanning = false
	if msg.err != nil {
		// A failed scan on the onboarding screen is not an error state: the
		// screen still has a manual path, and saying why the scan found
		// nothing is more useful there than an error banner over an empty list.
		m.scanErr = firstLine(msg.err.Error())
		m.devices = nil
		if msg.overlay {
			m.overlay = machineNoOverlay
			m.setError(msg.err)
		}
		return m, nil
	}
	m.scanErr = ""
	m.devices = msg.devices
	m.deviceCursor = 0
	if msg.overlay {
		if len(msg.devices) == 0 {
			m.overlay = machineNoOverlay
			m.setError(camperrors.New("no tailnet devices found"))
			return m, nil
		}
		m.overlay = machineDiscoverOverlay
	}
	return m, nil
}

// updateOnboarding drives the screen an empty fleet opens on. Its list is the
// tailnet, not the fleet, so enter adds a device rather than opening a detail
// pane, and the keys are limited to the two that can start a fleet.
func (m *machineTUIModel) updateOnboarding(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc", "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "up", "k":
		if len(m.devices) > 0 {
			m.deviceCursor = (m.deviceCursor - 1 + len(m.devices)) % len(m.devices)
		}
	case "down", "j":
		if len(m.devices) > 0 {
			m.deviceCursor = (m.deviceCursor + 1) % len(m.devices)
		}
	case "enter":
		if len(m.devices) > 0 {
			return m, m.prefillFromDevice()
		}
	case "a":
		return m, m.openAddForm()
	case "s":
		if m.tailscaleReady {
			m.status = ""
			return m, m.beginScan(false)
		}
		m.setError(camperrors.New("tailscale is not installed; press a to add a machine by hand"))
	case "?":
		m.overlay = machineHelpOverlay
	}
	return m, nil
}

func (m *machineTUIModel) openAddForm() tea.Cmd {
	m.form = newMachineForm()
	m.form.field = machineFieldID
	m.focusFormField()
	m.overlay = machineFormOverlay
	return textinput.Blink
}

func (m *machineTUIModel) updateBrowse(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if m.status != "" {
		m.status = ""
	}

	switch key {
	case "q", "esc", "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "a":
		return m, m.openAddForm()
	case "s":
		if !m.tailscaleReady {
			m.setError(camperrors.New("tailscale is not installed; press a to add a machine by hand"))
			return m, nil
		}
		return m, m.beginScan(true)
	case "t", "enter":
		return m, m.testSelected()
	case "e":
		return m, m.openEditForm()
	case "d":
		return m, m.openDeleteConfirm()
	case "r":
		m.setStatus("re-checking connection reuse")
		return m, m.probeSockets()
	case "R":
		return m, m.resetSelectedSocket()
	case "?":
		m.overlay = machineHelpOverlay
	}
	return m, nil
}

// testSelected runs a connection test against the highlighted machine.
func (m *machineTUIModel) testSelected() tea.Cmd {
	row := m.selectedRow()
	if row.Local {
		m.setError(camperrors.New("local is this computer; there is nothing to connect to"))
		return nil
	}
	m.health[row.Machine.ID] = machineHealth{State: healthTesting}
	m.setStatus("testing " + row.Machine.ID + "...")
	return tea.Batch(m.spin.Tick, m.testMachine(*row.Machine))
}

func (m *machineTUIModel) moveCursor(delta int) {
	if len(m.rows) == 0 {
		return
	}
	m.cursor = (m.cursor + delta + len(m.rows)) % len(m.rows)
}

// errLocalRow is the shared refusal for actions that only make sense against a
// configured entry. It matches the CLI's wording for the same attempts.
func errLocalRow(action string) error {
	return camperrors.New(`cannot ` + action + ` "local"; it is the current machine, not a configured entry`)
}

func (m *machineTUIModel) openEditForm() tea.Cmd {
	row := m.selectedRow()
	if row.Local {
		m.setError(errLocalRow("edit"))
		return nil
	}

	form := newMachineForm()
	form.editID = row.Machine.ID
	form.auth = row.Machine.AuthMethod
	if form.auth == "" {
		form.auth = machines.AuthSSHAgent
	}
	form.input(machineFieldID).SetValue(row.Machine.ID)
	form.input(machineFieldLabel).SetValue(row.Machine.Label)
	form.input(machineFieldHost).SetValue(row.Machine.Host)
	form.input(machineFieldUser).SetValue(row.Machine.SSHUser)
	form.input(machineFieldIdentity).SetValue(row.Machine.IdentityFile)
	// The id is the key the entry is stored under, so editing starts on the
	// first field that can actually change.
	form.field = machineFieldLabel

	m.form = form
	m.focusFormField()
	m.overlay = machineFormOverlay
	return textinput.Blink
}

func (m *machineTUIModel) openDeleteConfirm() tea.Cmd {
	row := m.selectedRow()
	if row.Local {
		m.setError(errLocalRow("remove"))
		return nil
	}
	m.pendingID = row.Machine.ID
	m.overlay = machineDeleteOverlay
	return nil
}

func (m *machineTUIModel) resetSelectedSocket() tea.Cmd {
	row := m.selectedRow()
	if row.Local {
		m.setError(camperrors.New(`"local" is this machine and has no ControlMaster socket`))
		return nil
	}
	if err := remote.ResetControlMaster(m.ctx, row.Machine); err != nil {
		m.setError(err)
		return nil
	}
	m.setStatus(fmt.Sprintf("cleared the ControlMaster socket for %q", row.Machine.ID))
	return m.probeSockets()
}

func (m *machineTUIModel) updateOverlay(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.overlay {
	case machineHelpOverlay:
		switch msg.String() {
		case "esc", "?", "enter", "q":
			m.overlay = machineNoOverlay
		}
		return m, nil
	case machineDeleteOverlay:
		return m.updateDelete(msg)
	case machineDiscoverOverlay:
		return m.updateDiscover(msg)
	case machineFormOverlay:
		return m.updateForm(msg)
	default:
		return m, nil
	}
}

func (m *machineTUIModel) updateDelete(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "esc", "n", "q":
		m.overlay = machineNoOverlay
		m.pendingID = ""
	case "y", "enter":
		return m, m.removePending()
	}
	return m, nil
}

func (m *machineTUIModel) removePending() tea.Cmd {
	id := m.pendingID
	m.pendingID = ""
	m.overlay = machineNoOverlay

	kept := make([]machines.Machine, 0, len(m.file.Machines))
	for _, mach := range m.file.Machines {
		if mach.ID != id {
			kept = append(kept, mach)
		}
	}
	m.file.Machines = kept
	if err := m.file.Save(); err != nil {
		m.setError(err)
		return nil
	}
	if err := m.reload(""); err != nil {
		m.setError(err)
		return nil
	}
	m.setStatus(fmt.Sprintf("removed %q", id))
	// Removing the last machine lands on the onboarding screen mid-session.
	// Init already scanned for a cold empty start; mirror that here so the
	// body does not claim "Tailscale reports no other devices" when we never
	// scanned this session.
	if m.empty() && m.tailscaleReady {
		return m.beginScan(false)
	}
	return nil
}

func (m *machineTUIModel) updateDiscover(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.scanning {
		if msg.String() == "ctrl+c" {
			m.quitting = true
			return m, tea.Quit
		}
		return m, nil
	}

	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "esc", "q":
		m.overlay = machineNoOverlay
	case "up", "k":
		if len(m.devices) > 0 {
			m.deviceCursor = (m.deviceCursor - 1 + len(m.devices)) % len(m.devices)
		}
	case "down", "j":
		if len(m.devices) > 0 {
			m.deviceCursor = (m.deviceCursor + 1) % len(m.devices)
		}
	case "enter":
		return m, m.prefillFromDevice()
	}
	return m, nil
}

// prefillFromDevice opens the add form on the picked tailnet device rather than
// saving it outright. Discovery supplies a host and a derived id; the label,
// user, and identity are still the operator's call, and seeing them before the
// write is what keeps a discovered machine from landing half-configured.
//
// A host already present in the fleet is refused rather than prefilled: the
// row already teaches "already added as X", and opening the form would let the
// user save a second id for the same machine.
func (m *machineTUIModel) prefillFromDevice() tea.Cmd {
	if len(m.devices) == 0 {
		return nil
	}
	device := m.devices[clampIndex(m.deviceCursor, len(m.devices))]
	if existing, ok := m.configuredHosts()[device.Host]; ok {
		m.setStatus(fmt.Sprintf("already added as %s · select it in the fleet and press e to edit", existing))
		return nil
	}
	id, err := deriveMachineID(device)
	if err != nil {
		m.setError(err)
		m.overlay = machineNoOverlay
		return nil
	}

	form := newMachineForm()
	form.fromDiscovery = true
	// D2: discover pre-fills OpenSSH (keys/agent); operator can cycle to
	// Tailscale SSH before saving.
	form.auth = machines.AuthSSHAgent
	form.input(machineFieldID).SetValue(id)
	form.input(machineFieldLabel).SetValue(device.HostName)
	form.input(machineFieldHost).SetValue(device.Host)
	form.field = machineFieldID

	m.form = form
	m.focusFormField()
	m.overlay = machineFormOverlay
	return textinput.Blink
}

func (m *machineTUIModel) focusFormField() {
	for i := range m.form.inputs {
		m.form.inputs[i].Blur()
	}
	if in := m.form.input(m.form.field); in != nil {
		in.Focus()
	}
}

func (m *machineTUIModel) moveFormField(delta int) {
	next := m.form.field
	for {
		next = (next + machineFormField(delta) + machineFieldCount) % machineFieldCount
		// The id cannot change on an edit, so skip over it rather than parking
		// the cursor on a field that ignores every keystroke.
		if !m.form.editing() || next != machineFieldID {
			break
		}
	}
	m.form.field = next
	m.focusFormField()
}

func (m *machineTUIModel) cycleAuth(delta int) {
	current := 0
	for i, auth := range machineAuthCycle {
		if auth == m.form.auth {
			current = i
			break
		}
	}
	m.form.auth = machineAuthCycle[(current+delta+len(machineAuthCycle))%len(machineAuthCycle)]
}

func (m *machineTUIModel) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "esc":
		m.overlay = machineNoOverlay
		m.form.err = ""
		return m, nil
	case "tab", "down":
		m.moveFormField(1)
		return m, nil
	case "shift+tab", "up":
		m.moveFormField(-1)
		return m, nil
	case "left":
		if m.form.field == machineFieldAuth {
			m.cycleAuth(-1)
			return m, nil
		}
	case "right", " ":
		if m.form.field == machineFieldAuth {
			m.cycleAuth(1)
			return m, nil
		}
	case "enter":
		// Enter saves from any field rather than walking to the last one.
		// Four of the six fields are optional, so advancing through all of
		// them to reach a save would be the common path, not the exception.
		// A missing required field comes back as an inline error focused on
		// that field, so an early enter costs nothing.
		return m, m.saveForm()
	}

	if m.form.editing() && m.form.field == machineFieldID {
		return m, nil
	}
	in := m.form.input(m.form.field)
	if in == nil {
		return m, nil
	}
	var cmd tea.Cmd
	*in, cmd = in.Update(msg)
	return m, cmd
}

func (m *machineTUIModel) saveForm() tea.Cmd {
	id := strings.TrimSpace(m.form.value(machineFieldID))
	host := strings.TrimSpace(m.form.value(machineFieldHost))

	if err := validateMachineID(id); err != nil {
		m.form.err = err.Error()
		m.form.field = machineFieldID
		m.focusFormField()
		return nil
	}
	if host == "" {
		m.form.err = "host must not be empty"
		m.form.field = machineFieldHost
		m.focusFormField()
		return nil
	}
	auth, err := normalizeAuthMethod(m.form.auth)
	if err != nil {
		m.form.err = err.Error()
		return nil
	}
	// Adding an id that already exists would silently replace that entry, since
	// Upsert is keyed on id. Editing it is the explicit way to do that.
	if !m.form.editing() {
		if _, _, found := m.file.Lookup(id); found {
			m.form.err = fmt.Sprintf("machine %q already exists; select it and press e to change it", id)
			m.form.field = machineFieldID
			m.focusFormField()
			return nil
		}
	}

	m.file.Upsert(machines.Machine{
		ID:           id,
		Label:        strings.TrimSpace(m.form.value(machineFieldLabel)),
		Host:         host,
		AuthMethod:   auth,
		SSHUser:      strings.TrimSpace(m.form.value(machineFieldUser)),
		IdentityFile: strings.TrimSpace(m.form.value(machineFieldIdentity)),
	})
	if err := m.file.Save(); err != nil {
		m.form.err = err.Error()
		return nil
	}

	m.overlay = machineNoOverlay
	m.form.err = ""
	if err := m.reload(id); err != nil {
		m.setError(err)
		return nil
	}

	// Test the machine straight away. The question a person has the moment
	// they finish this form is whether what they typed actually works, and
	// making them find and press another key to learn that is how a fleet ends
	// up holding entries nobody has ever successfully connected to.
	saved := machines.Machine{
		ID: id, Host: host, AuthMethod: auth,
		SSHUser:      strings.TrimSpace(m.form.value(machineFieldUser)),
		IdentityFile: strings.TrimSpace(m.form.value(machineFieldIdentity)),
	}
	m.health[id] = machineHealth{State: healthTesting}
	m.setStatus("saved " + id + " · testing the connection...")
	return tea.Batch(m.spin.Tick, m.probeSockets(), m.testMachine(saved))
}
