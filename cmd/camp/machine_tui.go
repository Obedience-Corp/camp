package main

import (
	"context"
	"errors"
	"os/exec"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
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
// machineForm.inputs; machineForm.input maps the rest.
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
// three values ~/.obey/machines.yaml accepts. OpenSSH first (D2 default).
var machineAuthCycle = []string{
	machines.AuthSSHAgent,
	machines.AuthTailscaleSSH,
	machines.AuthSSHPassword,
}

// authLabel names an auth method the way it is explained to a person rather
// than the way it is spelled in the file. The raw value is still what gets
// written; this is display only (D7 labels).
func authLabel(auth string) string {
	return remote.AuthDisplayName(auth)
}

// healthState is what the last connection attempt established. It is
// deliberately separate from the ControlMaster socket state: a socket is an
// ssh multiplexing detail, while this answers the question a person actually
// has, which is whether camp can reach the machine at all.
type healthState int

const (
	healthUntested healthState = iota
	healthTesting
	healthReachable
	healthUnreachable
	// healthUnsupported is a machine camp will not hop to regardless of
	// whether ssh could reach it, currently password auth.
	healthUnsupported
)

type machineHealth struct {
	State healthState
	// Version is the camp version reported by the remote, when reachable.
	Version string
	// Detail is the failure, kept to one line so it renders in the pane.
	Detail string
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
	health  map[string]machineHealth

	// tailscaleReady reports whether the tailscale CLI is installed. The
	// onboarding screen offers a scan only when it is, and says what
	// installing it would buy when it is not.
	tailscaleReady bool
	devices        []discoveredDevice
	deviceCursor   int
	scanning       bool
	scanErr        string
	// scanGen is a monotonic token stamped on each beginScan. applyDevices
	// ignores results whose gen does not match, so a late finish from an
	// earlier overlapping scan cannot clobber a newer one.
	scanGen uint64

	overlay   machineOverlay
	form      machineForm
	pendingID string

	// spin animates while a connection test or tailnet scan is out. An ssh
	// connect can take the full ConnectTimeout before it fails, and a
	// tailscale status call can sit still just as long; a screen that does
	// not move for eight seconds reads as hung rather than busy.
	spin spinner.Model

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
// overlay reports whether the scan was for the picker; a scan that backs the
// onboarding screen fills its list in place instead. gen is the scanGen the
// scan was started with, so a stale finish can be dropped.
type devicesMsg struct {
	devices []discoveredDevice
	err     error
	overlay bool
	gen     uint64
}

// healthMsg carries one machine's connection-test result.
type healthMsg struct {
	id     string
	health machineHealth
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
		ctx:            ctx,
		file:           mf,
		sockets:        map[string]remote.SocketDiagnosis{},
		health:         map[string]machineHealth{},
		tailscaleReady: tailscaleInstalled(),
		spin:           newMachineSpinner(),
	}
	m.rebuildRows("")
	return m
}

func newMachineSpinner() spinner.Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	return s
}

// testing reports whether any machine has a test in flight, which is what
// keeps the spinner ticking and stops it once every result is in.
func (m *machineTUIModel) testing() bool {
	for _, health := range m.health {
		if health.State == healthTesting {
			return true
		}
	}
	return false
}

// busy reports whether the spinner should keep ticking: either a connection
// test or a tailnet scan is in flight.
func (m *machineTUIModel) busy() bool {
	return m.testing() || m.scanning
}

// tailscaleInstalled reports whether the tailscale CLI is on PATH. Discovery
// shells out to it, so its absence is the difference between offering a scan
// and explaining why there is nothing to scan.
func tailscaleInstalled() bool {
	_, err := exec.LookPath("tailscale")
	return err == nil
}

// empty reports whether the fleet holds nothing but the implicit local
// machine, which is the state the onboarding screen exists for.
func (m *machineTUIModel) empty() bool {
	return len(m.file.Machines) == 0
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

// configuredHosts indexes the fleet by host so a tailnet device already in the
// list can be marked as such rather than offered again as if it were new.
func (m *machineTUIModel) configuredHosts() map[string]string {
	hosts := make(map[string]string, len(m.file.Machines))
	for _, mach := range m.file.Machines {
		hosts[mach.Host] = mach.ID
	}
	return hosts
}

func (m *machineTUIModel) Init() tea.Cmd {
	cmds := []tea.Cmd{textinput.Blink, m.probeSockets()}
	// An empty fleet opens on the onboarding screen, whose whole value is
	// showing the machines the user could add. Scanning up front is what
	// turns that screen from an instruction into a list they can act on.
	if m.empty() && m.tailscaleReady {
		cmds = append(cmds, m.beginScan(false))
	}
	return tea.Batch(cmds...)
}

// beginScan starts a tailnet scan with a generation token so a late result
// from an earlier scan cannot clobber a newer one. Calling it while a scan is
// already in flight replaces that work rather than stacking results.
func (m *machineTUIModel) beginScan(intoOverlay bool) tea.Cmd {
	m.scanGen++
	gen := m.scanGen
	m.scanning = true
	m.scanErr = ""
	if intoOverlay {
		m.overlay = machineDiscoverOverlay
	}
	return tea.Batch(m.spin.Tick, m.scanTailnet(intoOverlay, gen))
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

// scanTailnet runs discovery in the background so the screen stays responsive
// while `tailscale status --json` is out. gen is stamped onto the result so
// applyDevices can drop stale finishes.
func (m *machineTUIModel) scanTailnet(intoOverlay bool, gen uint64) tea.Cmd {
	ctx := m.ctx
	return func() tea.Msg {
		devices, err := discoverTailnet(ctx, runTailscaleStatus)
		return devicesMsg{devices: devices, err: err, overlay: intoOverlay, gen: gen}
	}
}

// testMachine asks the machine's own camp for its version over ssh. That one
// round trip answers everything the screen needs: whether ssh authenticates,
// whether the host resolves, and whether camp is installed over there, which
// is the failure a socket state can never show.
func (m *machineTUIModel) testMachine(target machines.Machine) tea.Cmd {
	ctx := m.ctx
	return func() tea.Msg {
		if err := remote.EnsureKeyAuth(&target); err != nil {
			return healthMsg{id: target.ID, health: machineHealth{
				State:  healthUnsupported,
				Detail: "camp cannot hop to a password-auth machine yet",
			}}
		}
		out, err := remote.RunCampCommand(ctx, &target, "--version")
		if err != nil {
			return healthMsg{id: target.ID, health: machineHealth{
				State:  healthUnreachable,
				Detail: connectionFailureDetailFor(err, &target),
			}}
		}
		return healthMsg{id: target.ID, health: machineHealth{
			State:   healthReachable,
			Version: parseRemoteVersion(string(out)),
		}}
	}
}

// parseRemoteVersion pulls the version out of `camp --version` output, which
// reads "camp version 0.9.2 (built ..., commit ...)". Anything unexpected
// degrades to the trimmed first line rather than guessing.
func parseRemoteVersion(out string) string {
	line := firstLine(out)
	if _, rest, found := strings.Cut(line, "version "); found {
		if version, _, ok := strings.Cut(rest, " "); ok {
			return version
		}
		return rest
	}
	return line
}

// connectionFailureDetail reduces a failed hop to the part a person can act
// on. The wrapper says which command ran and what it exited with, which the
// screen already implies; what matters is ssh's own line, or camp's hint when
// the binary is missing on the far side.
//
// Tailscale SSH check mode is special: the host is reachable, but BatchMode
// cannot complete the browser approval. Prefer the extracted check URL over a
// generic "timed out" / "unreachable" line so the operator knows what to do.
//
// Exit 127 is special: RunCampCommand wraps it with login-shell PATH context
// and CAMP_REMOTE_CAMP_PATH guidance. Digging past that wrap to the bare
// "command not found" stderr line is exactly the failure mode the health
// check exists to surface, so preserve the outer message instead.
func connectionFailureDetail(err error) string {
	return connectionFailureDetailFor(err, nil)
}

// connectionFailureDetailFor reduces a failed hop to an actionable line.
// When m is set, the detail is prefixed with the auth mode label (D3/D7).
func connectionFailureDetailFor(err error, m *machines.Machine) string {
	if detail := remote.FormatHopFailure(err, m); detail != "" {
		return detail
	}
	if detail := remote.HopFailureDetail(err); detail != "" {
		return detail
	}

	var cmdErr *camperrors.CommandError
	if errors.As(err, &cmdErr) && cmdErr.ExitCode == 127 {
		return firstLine(err.Error())
	}

	message := firstLine(err.Error())
	if _, rest, found := strings.Cut(message, "exited with code "); found {
		if _, detail, ok := strings.Cut(rest, ": "); ok && strings.TrimSpace(detail) != "" {
			message = detail
		}
	}
	// Drop a trailing ": context deadline exceeded" from wrapped timeouts so
	// the pane leads with the real cause when stderr was preserved.
	message = strings.TrimSpace(message)
	if before, found := strings.CutSuffix(message, ": context deadline exceeded"); found {
		message = strings.TrimSpace(before)
	}
	if before, found := strings.CutSuffix(message, ": context canceled"); found {
		message = strings.TrimSpace(before)
	}
	return strings.TrimPrefix(message, "ssh: ")
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(line)
}

// newMachineForm builds an empty add form.
func newMachineForm() machineForm {
	form := machineForm{auth: machines.AuthSSHAgent}
	placeholders := [machineFieldCount - 1]string{
		"devbox",
		"optional, shown in the list",
		"devbox.tailnet.ts.net",
		"optional, defaults to your login name",
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

// input maps a field to its text input, or nil for the auth cycle.
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
