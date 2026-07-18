package main

import (
	"context"
	"sort"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/machines"
	"github.com/Obedience-Corp/camp/internal/remote"
	"github.com/Obedience-Corp/camp/internal/ui"
)

type machineOverlay int

const (
	machineNoOverlay machineOverlay = iota
	machineFormOverlay
	machineDeleteOverlay
	machineDiscoverOverlay
	machineHelpOverlay
)

// machineFormField indexes the fields of the add/edit form in the order they
// are rendered. Auth is a cycle rather than a text input, so it has no entry in
// machineForm.inputs; machineFormInput maps the rest.
type machineFormField int

const (
	machineFieldID machineFormField = iota
	machineFieldLabel
	machineFieldHost
	machineFieldAuth
	machineFieldUser
	machineFieldIdentity
	machineFieldCount
)

// machineAuthCycle is the order the auth field cycles through, matching the
// three values ~/.obey/machines.yaml accepts.
var machineAuthCycle = []string{
	machines.AuthTailscaleSSH,
	machines.AuthSSHAgent,
	machines.AuthSSHPassword,
}

// machineRow is one row of the fleet list. Machine is nil for the synthetic
// "local" row, which is the current machine and is never persisted to
// machines.yaml, so it can be neither edited nor removed.
type machineRow struct {
	Machine *machines.Machine
	Local   bool
}

func (r machineRow) id() string {
	if r.Local {
		return machines.LocalMachineID
	}
	return r.Machine.ID
}

// machineForm holds the add/edit overlay's state. editID is empty when adding.
// On edit the id is fixed: it is the key Upsert writes under, so letting it
// change here would leave the original entry behind rather than rename it.
type machineForm struct {
	editID string
	inputs [machineFieldCount - 1]textinput.Model
	auth   string
	field  machineFormField
	err    string
	// fromDiscovery records that the form was prefilled from a tailnet device,
	// so the view can say where the values came from.
	fromDiscovery bool
}

type machineTUIModel struct {
	ctx  context.Context
	file *machines.File
	rows []machineRow

	cursor  int
	sockets map[string]remote.SocketDiagnosis

	overlay      machineOverlay
	form         machineForm
	pendingID    string
	devices      []discoveredDevice
	deviceCursor int
	scanning     bool

	status    string
	statusErr bool
	width     int
	height    int
	quitting  bool
}

// socketsMsg carries the result of probing every machine's ControlMaster
// socket. The probe runs as a command rather than inline so opening the TUI
// never blocks on it.
type socketsMsg map[string]remote.SocketDiagnosis

// devicesMsg carries the result of a tailnet scan, or the error that ended it.
type devicesMsg struct {
	devices []discoveredDevice
	err     error
}

// runMachineTUI is the human-facing entry point for a bare `camp machine`. The
// subcommands stay the interface for scripts and agents, so a non-terminal
// invocation keeps printing help exactly as it did before this TUI existed.
func runMachineTUI(cmd *cobra.Command, _ []string) error {
	if !ui.IsTerminal() {
		return cmd.Help()
	}

	ctx := cmd.Context()
	mf, err := machines.Load()
	if err != nil {
		return err
	}

	program := tea.NewProgram(newMachineTUIModel(ctx, mf), tea.WithContext(ctx), tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		return camperrors.Wrap(err, "running machine TUI")
	}
	return nil
}

func newMachineTUIModel(ctx context.Context, mf *machines.File) *machineTUIModel {
	m := &machineTUIModel{
		ctx:     ctx,
		file:    mf,
		sockets: map[string]remote.SocketDiagnosis{},
	}
	m.rebuildRows("")
	return m
}

// rebuildRows rebuilds the fleet list from the loaded file and restores the
// cursor to selectID when it is still present.
func (m *machineTUIModel) rebuildRows(selectID string) {
	sort.SliceStable(m.file.Machines, func(i, j int) bool {
		return m.file.Machines[i].ID < m.file.Machines[j].ID
	})

	rows := []machineRow{{Local: true}}
	for i := range m.file.Machines {
		rows = append(rows, machineRow{Machine: &m.file.Machines[i]})
	}
	m.rows = rows

	m.cursor = 0
	for i, row := range rows {
		if row.id() == selectID {
			m.cursor = i
			break
		}
	}
}

func (m *machineTUIModel) reload(selectID string) error {
	mf, err := machines.Load()
	if err != nil {
		return err
	}
	m.file = mf
	m.rebuildRows(selectID)
	return nil
}

func (m *machineTUIModel) selectedRow() machineRow {
	if len(m.rows) == 0 {
		return machineRow{Local: true}
	}
	return m.rows[clampIndex(m.cursor, len(m.rows))]
}

func clampIndex(i, n int) int {
	if n <= 0 {
		return 0
	}
	if i < 0 {
		return 0
	}
	if i >= n {
		return n - 1
	}
	return i
}

func (m *machineTUIModel) setStatus(message string) {
	m.status, m.statusErr = message, false
}

func (m *machineTUIModel) setError(err error) {
	m.status, m.statusErr = err.Error(), true
}

func (m *machineTUIModel) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.probeSockets())
}

// probeSockets checks every configured machine's ControlMaster socket. A
// machine with no socket file resolves without touching ssh at all, and a
// present socket is probed against the local multiplex process, so this stays
// off the network entirely.
func (m *machineTUIModel) probeSockets() tea.Cmd {
	targets := append([]machines.Machine(nil), m.file.Machines...)
	ctx := m.ctx
	return func() tea.Msg {
		out := make(map[string]remote.SocketDiagnosis, len(targets))
		for i := range targets {
			out[targets[i].ID] = remote.CheckControlMaster(ctx, &targets[i])
		}
		return socketsMsg(out)
	}
}

// scanTailnet runs discovery in the background so the list stays responsive
// while `tailscale status --json` is out.
func (m *machineTUIModel) scanTailnet() tea.Cmd {
	ctx := m.ctx
	return func() tea.Msg {
		devices, err := discoverTailnet(ctx, runTailscaleStatus)
		return devicesMsg{devices: devices, err: err}
	}
}

// newMachineForm builds an empty add form.
func newMachineForm() machineForm {
	form := machineForm{auth: machines.AuthSSHAgent}
	placeholders := [machineFieldCount - 1]string{
		"devbox",
		"optional, shown in the list",
		"devbox.tailnet.ts.net",
		"optional ssh user",
		"optional, path to an ssh key",
	}
	for i := range form.inputs {
		form.inputs[i] = textinput.New()
		form.inputs[i].Prompt = "  "
		form.inputs[i].CharLimit = 256
		form.inputs[i].Placeholder = placeholders[i]
	}
	return form
}

// machineFormInput maps a field to its text input, or nil for the auth cycle.
func (f *machineForm) input(field machineFormField) *textinput.Model {
	switch field {
	case machineFieldID:
		return &f.inputs[0]
	case machineFieldLabel:
		return &f.inputs[1]
	case machineFieldHost:
		return &f.inputs[2]
	case machineFieldUser:
		return &f.inputs[3]
	case machineFieldIdentity:
		return &f.inputs[4]
	default:
		return nil
	}
}

func (f *machineForm) value(field machineFormField) string {
	if in := f.input(field); in != nil {
		return in.Value()
	}
	return ""
}

func (f *machineForm) editing() bool { return f.editID != "" }
