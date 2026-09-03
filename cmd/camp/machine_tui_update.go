package main

import (
	"fmt"
	"os/exec"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/machines"
	"github.com/Obedience-Corp/camp/internal/remote"
	"github.com/Obedience-Corp/camp/internal/shell"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
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
	case hopCampaignsMsg:
		return m.applyHopCampaigns(msg)
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
		m.statusKind = statusOK
		if msg.health.State != healthReachable {
			m.statusKind = statusError
		}
		return m, nil
	case pairBootstrapPreparedMsg:
		return m.applyPairBootstrapPrepared(msg)
	case pairBootstrapFinishedMsg:
		return m.applyPairBootstrapFinished(msg)
	case pairFlowFinishedMsg:
		return m.applyPairFlowFinished(msg)
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
	case healthCampMissing:
		return "reached " + id + ", but camp is not installed there: " + health.Detail
	case healthAuthDenied:
		return "SSH login denied by " + id + ": " + health.Detail
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
	case "enter":
		return m.openHopPicker()
	case "t":
		return m, m.testSelected()
	case "p":
		return m, m.pairSelected()
	case "e":
		return m, m.openEditForm()
	case "d":
		return m, m.openDeleteConfirm()
	case "o":
		return m, m.openCheckURL(m.selectedCheckURL())
	case "c":
		return m, m.copyCheckURL(m.selectedCheckURL())
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

func (m *machineTUIModel) pairSelected() tea.Cmd {
	row := m.selectedRow()
	if row.Local {
		m.setError(camperrors.New(`"local" is this machine; select the other machine to pair`))
		return nil
	}
	m.pair = machinePairState{
		machineID: row.id(),
		bootstrap: m.health[row.id()].State == healthAuthDenied,
	}
	m.overlay = machinePairOverlay
	return nil
}

func (m *machineTUIModel) preparePairBootstrap() tea.Cmd {
	target, ok := m.machineByID(m.pair.machineID)
	if !ok {
		return func() tea.Msg {
			return pairBootstrapPreparedMsg{err: camperrors.New("selected machine is no longer configured")}
		}
	}
	m.pair.busy = true
	m.pair.err = ""
	return func() tea.Msg {
		keyPath, err := localKeyPath(target.ID)
		if err != nil {
			return pairBootstrapPreparedMsg{err: err}
		}
		_, err = machinePairGenerateKey(keyPath, "camp-to-"+target.ID)
		return pairBootstrapPreparedMsg{machine: target, keyPath: keyPath, err: err}
	}
}

func (m *machineTUIModel) applyPairBootstrapPrepared(msg pairBootstrapPreparedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.pair.busy = false
		m.pair.err = msg.err.Error()
		return m, nil
	}
	m.pair.keyPath = msg.keyPath
	command := machinePairCopyID(m.ctx, msg.machine, msg.keyPath)
	return m, tea.ExecProcess(command, func(err error) tea.Msg {
		return pairBootstrapFinishedMsg{machineID: msg.machine.ID, keyPath: msg.keyPath, err: err}
	})
}

func (m *machineTUIModel) applyPairBootstrapFinished(msg pairBootstrapFinishedMsg) (tea.Model, tea.Cmd) {
	m.pair.busy = false
	if msg.err != nil {
		m.pair.err = "Password setup did not finish: " + msg.err.Error()
		return m, nil
	}
	target, ok := m.machineByID(msg.machineID)
	if !ok {
		m.pair.err = "Machine was removed before access setup finished."
		return m, nil
	}
	target.AuthMethod = machines.AuthSSHAgent
	target.IdentityFile = msg.keyPath
	m.file.Upsert(target)
	if err := m.file.Save(); err != nil {
		m.pair.err = err.Error()
		return m, nil
	}
	m.pair.bootstrap = false
	m.pair.err = ""
	return m, m.runPairFlow()
}

func (m *machineTUIModel) runPairFlow() tea.Cmd {
	id := m.pair.machineID
	m.pair.busy = true
	executable, err := machinePairExecutable()
	if err != nil {
		return func() tea.Msg { return pairFlowFinishedMsg{machineID: id, err: err} }
	}
	command := exec.CommandContext(m.ctx, executable, "machine", "pair", id)
	return tea.ExecProcess(command, func(err error) tea.Msg {
		return pairFlowFinishedMsg{machineID: id, err: err}
	})
}

func (m *machineTUIModel) applyPairFlowFinished(msg pairFlowFinishedMsg) (tea.Model, tea.Cmd) {
	m.pair.busy = false
	m.overlay = machineNoOverlay
	if err := m.reload(msg.machineID); err != nil {
		m.setError(err)
		return m, nil
	}
	if msg.err != nil {
		m.setAdvice("Pairing did not finish; testing the access that was established.")
	} else {
		m.setStatus("Pair flow complete · testing both access and camp...")
	}
	target, ok := m.machineByID(msg.machineID)
	if !ok {
		return m, nil
	}
	m.health[msg.machineID] = machineHealth{State: healthTesting}
	return m, tea.Batch(m.spin.Tick, m.probeSockets(), m.testMachine(target))
}

// openHopPicker turns the highlighted machine into a hop by asking which
// campaign to land in. It refuses before opening rather than after choosing, so
// a machine that cannot be hopped to never presents a list that leads nowhere.
func (m *machineTUIModel) openHopPicker() (tea.Model, tea.Cmd) {
	row := m.selectedRow()
	if row.Local {
		m.setError(camperrors.New("local is this computer; use 'camp list' to open a camp here"))
		return m, nil
	}
	if !m.hopEnabled {
		m.setError(camperrors.New("hop needs shell integration: run " + shell.InitHint()))
		return m, nil
	}
	if err := remote.EnsureKeyAuth(row.Machine); err != nil {
		m.setError(camperrors.New("camp cannot hop to a password-auth machine yet"))
		return m, nil
	}

	m.hop.gen++
	m.hop = machineHopState{machineID: row.Machine.ID, gen: m.hop.gen}
	m.overlay = machineHopOverlay

	// Cache first: this is the keystroke path, and the snapshot is exactly the
	// data `csw <id>:<tab>` completes from. A cold cache is the only case that
	// pays for ssh, and it pays once because the fetch warms the cache too.
	if names, ok := readMachineCacheCampaigns(row.Machine.ID); ok && len(names) > 0 {
		m.hop.campaigns = names
		m.hop.cached = true
		return m, nil
	}
	m.hop.loading = true
	return m, tea.Batch(m.spin.Tick, m.fetchHopCampaigns(*row.Machine, m.hop.gen))
}

// updateHop drives the campaign picker. r re-fetches, which is how a stale
// snapshot gets corrected without leaving the screen.
func (m *machineTUIModel) updateHop(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "esc", "q":
		m.overlay = machineNoOverlay
		m.hop = machineHopState{gen: m.hop.gen}
		return m, nil
	case "up", "k":
		if n := len(m.hop.campaigns); n > 0 {
			m.hop.cursor = (m.hop.cursor - 1 + n) % n
		}
		return m, nil
	case "down", "j":
		if n := len(m.hop.campaigns); n > 0 {
			m.hop.cursor = (m.hop.cursor + 1) % n
		}
		return m, nil
	case "o":
		return m, m.openCheckURL(m.hop.checkURL)
	case "c":
		return m, m.copyCheckURL(m.hop.checkURL)
	case "r":
		if m.hop.loading {
			return m, nil
		}
		m.hop.gen++
		m.hop.loading = true
		m.hop.err = ""
		m.hop.checkURL = ""
		target, ok := m.machineByID(m.hop.machineID)
		if !ok {
			m.hop.loading = false
			return m, nil
		}
		return m, tea.Batch(m.spin.Tick, m.fetchHopCampaigns(target, m.hop.gen))
	case "enter":
		if m.hop.loading || len(m.hop.campaigns) == 0 {
			return m, nil
		}
		name := m.hop.campaigns[clampIndex(m.hop.cursor, len(m.hop.campaigns))]
		// The wrapper resolves this through `camp switch <id>:<name>`, so the
		// remote registry still decides the path and this never guesses one.
		m.hopSelection = machineHopSelection(m.hop.machineID, name)
		m.quitting = true
		return m, tea.Quit
	}
	return m, nil
}

// applyHopCampaigns folds a live fetch into the overlay, dropping a result whose
// generation has been superseded by a close, a re-open, or a newer refresh.
func (m *machineTUIModel) applyHopCampaigns(msg hopCampaignsMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.hop.gen || m.overlay != machineHopOverlay {
		return m, nil
	}
	m.hop.loading = false
	if msg.err != nil {
		m.hop.err = connectionFailureDetail(msg.err)
		m.hop.checkURL = remote.TailscaleCheckURL(msg.err)
		// The error screen replaces the list, so the list must actually be gone:
		// keeping the previous campaigns here would let enter act on entries the
		// operator can no longer see, hopping from a failure screen.
		m.hop.campaigns = nil
		m.hop.cached = false
		m.hop.cursor = 0
		return m, nil
	}
	m.hop.err = ""
	m.hop.checkURL = ""
	m.hop.campaigns = msg.campaigns
	m.hop.cached = false
	m.hop.cursor = 0
	return m, nil
}

// machineByID finds a configured machine by id, copied so a background fetch
// never reads the slice the fleet list is rebuilt from.
func (m *machineTUIModel) machineByID(id string) (machines.Machine, bool) {
	for i := range m.file.Machines {
		if m.file.Machines[i].ID == id {
			return m.file.Machines[i], true
		}
	}
	return machines.Machine{}, false
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

// selectedCheckURL is the Tailscale approval URL for the highlighted machine,
// or "" when its last test did not end in check mode.
func (m *machineTUIModel) selectedCheckURL() string {
	row := m.selectedRow()
	if row.Local {
		return ""
	}
	return m.health[row.Machine.ID].CheckURL
}

// openCheckURL hands the approval URL to the operator's browser. Check mode is
// the one hop failure camp cannot resolve itself and the operator can, in one
// click — so the screen that reports it also completes it, rather than printing
// a URL and leaving them to select it out of a wrapped terminal line.
func (m *machineTUIModel) openCheckURL(url string) tea.Cmd {
	if url == "" {
		m.setAdvice("nothing to open here; o opens a Tailscale approval link when one is waiting")
		return nil
	}
	if err := ui.OpenInBrowser(url); err != nil {
		m.setError(camperrors.Wrap(err, "could not open your browser; copy the link with c"))
		return nil
	}
	m.setAdvice("opened the approval link; approve it, then press t (or r) to retry")
	return nil
}

// copyCheckURL is the fallback for a headless or remote terminal, where there
// is no browser to open but the operator can still paste the link somewhere
// that has one.
func (m *machineTUIModel) copyCheckURL(url string) tea.Cmd {
	if url == "" {
		m.setAdvice("nothing to copy here; c copies a Tailscale approval link when one is waiting")
		return nil
	}
	if err := ui.WriteClipboard(url); err != nil {
		m.setError(camperrors.Wrap(err, "could not copy the link"))
		return nil
	}
	m.setAdvice("copied the approval link; paste it into a browser, approve it, then press t (or r) to retry")
	return nil
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
	case machineHopOverlay:
		return m.updateHop(msg)
	case machinePairOverlay:
		return m.updatePairOverlay(msg)
	default:
		return m, nil
	}
}

func (m *machineTUIModel) updatePairOverlay(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.pair.busy {
		return m, nil
	}
	switch msg.String() {
	case "esc", "q":
		m.overlay = machineNoOverlay
		m.pair = machinePairState{}
	case "e":
		m.overlay = machineNoOverlay
		m.pair = machinePairState{}
		return m, m.openEditForm()
	case "enter":
		if m.pair.bootstrap {
			return m, m.preparePairBootstrap()
		}
		return m, m.runPairFlow()
	}
	return m, nil
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
