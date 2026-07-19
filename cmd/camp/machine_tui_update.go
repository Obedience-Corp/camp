package main

import (
	"fmt"
	"strings"

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
	case tea.KeyMsg:
		if m.overlay != machineNoOverlay {
			return m.updateOverlay(msg)
		}
		return m.updateBrowse(msg)
	}
	return m, nil
}

func (m *machineTUIModel) applyDevices(msg devicesMsg) (tea.Model, tea.Cmd) {
	m.scanning = false
	if msg.err != nil {
		m.overlay = machineNoOverlay
		m.setError(msg.err)
		return m, nil
	}
	if len(msg.devices) == 0 {
		m.overlay = machineNoOverlay
		m.setError(camperrors.New("no tailnet devices found"))
		return m, nil
	}
	m.devices = msg.devices
	m.deviceCursor = 0
	m.overlay = machineDiscoverOverlay
	return m, nil
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
		m.form = newMachineForm()
		m.form.field = machineFieldID
		m.focusFormField()
		m.overlay = machineFormOverlay
		return m, textinput.Blink
	case "s":
		m.scanning = true
		m.overlay = machineDiscoverOverlay
		return m, m.scanTailnet()
	case "e":
		return m, m.openEditForm()
	case "d":
		return m, m.openDeleteConfirm()
	case "r":
		m.setStatus("refreshing socket state")
		return m, m.probeSockets()
	case "R":
		return m, m.resetSelectedSocket()
	case "?":
		m.overlay = machineHelpOverlay
	}
	return m, nil
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
func (m *machineTUIModel) prefillFromDevice() tea.Cmd {
	if len(m.devices) == 0 {
		return nil
	}
	device := m.devices[clampIndex(m.deviceCursor, len(m.devices))]
	id, err := deriveMachineID(device)
	if err != nil {
		m.setError(err)
		m.overlay = machineNoOverlay
		return nil
	}

	form := newMachineForm()
	form.fromDiscovery = true
	form.auth = machines.AuthTailscaleSSH
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

	action := "added"
	if m.form.editing() {
		action = "updated"
	}
	m.overlay = machineNoOverlay
	m.form.err = ""
	if err := m.reload(id); err != nil {
		m.setError(err)
		return nil
	}
	m.setStatus(fmt.Sprintf("%s %q (%s, %s)", action, id, host, auth))
	return m.probeSockets()
}
