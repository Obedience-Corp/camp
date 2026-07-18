package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Obedience-Corp/camp/internal/intent/tui"
	"github.com/Obedience-Corp/camp/internal/machines"
	"github.com/Obedience-Corp/camp/internal/pathutil"
	"github.com/Obedience-Corp/camp/internal/remote"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
)

var machineTUIPal = theme.TUI()

// machinePaneTextInset and machineOverlayTextInset are the horizontal padding
// the pane and overlay styles add inside their borders. lipgloss counts that
// padding against the width given to Style.Width, so content must be clamped to
// the width minus the inset or lipgloss wraps it and the box outgrows its row
// budget.
const (
	machinePaneTextInset    = 2
	machineOverlayTextInset = 4
)

var (
	machineTitleStyle  = tui.TitleStyle
	machineHelpStyle   = tui.HelpStyle
	machineErrorStyle  = tui.ErrorStyle
	machineOKStyle     = tui.SuccessStyle
	machinePaneFocused = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(machineTUIPal.BorderFocus).Padding(0, 1)
	machinePaneBlurred = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(machineTUIPal.Border).Padding(0, 1)
	machineSelected    = lipgloss.NewStyle().Foreground(machineTUIPal.Accent).Bold(true)
	machinePrimary     = lipgloss.NewStyle().Foreground(machineTUIPal.TextPrimary)
	machineMuted       = lipgloss.NewStyle().Foreground(machineTUIPal.TextMuted)
	machineWarn        = lipgloss.NewStyle().Foreground(machineTUIPal.Warning)
	// The overlay box deliberately carries no background of its own. lipgloss
	// applies a container background to a line's first segment only, so any
	// row built from several styled pieces (a picker row is name, host, and
	// state) loses it at the first nested reset and comes out as a patchwork.
	// The dimmed backdrop behind the box does the separating instead, which
	// leaves every cell inside the box at one uniform color.
	machineOverlayStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(machineTUIPal.BorderFocus).Padding(1, 2)
)

type machineLayout struct {
	width      int
	height     int
	dual       bool
	leftWidth  int
	rightWidth int
	bodyRows   int
	showFooter bool
}

func (m *machineTUIModel) layout() machineLayout {
	w, h := m.width, m.height
	if w <= 0 {
		w = 100
	}
	if h <= 0 {
		h = 30
	}
	dual := w >= 78
	left, right := w, w
	if dual {
		left = max(w/3, 26)
		right = max(w-left-2, 30)
	}
	return machineLayout{
		width:      w,
		height:     h,
		dual:       dual,
		leftWidth:  left,
		rightWidth: right,
		bodyRows:   max(h-8, 4),
		showFooter: h >= 8,
	}
}

func (m *machineTUIModel) View() string {
	if m.quitting {
		return ""
	}
	if m.overlay != machineNoOverlay {
		return m.overlayView()
	}

	lay := m.layout()
	lines := []string{m.topBar(lay.width)}
	if lay.dual {
		joined := lipgloss.JoinHorizontal(lipgloss.Top, m.renderFleetPane(lay), " ", m.renderDetailPane(lay))
		lines = append(lines, strings.Split(joined, "\n")...)
	} else {
		lines = append(lines, strings.Split(m.renderFleetPane(lay), "\n")...)
	}
	if lay.showFooter {
		if status := m.statusLine(); status != "" {
			lines = append(lines, status)
		}
		lines = append(lines, m.footer(lay.width))
	}
	return ui.FitFullscreenView(strings.Join(ui.CapFrame(lines, lay.width, lay.height), "\n"), lay.height)
}

func (m *machineTUIModel) topBar(width int) string {
	line := machineTitleStyle.Render("Machines") + "  " +
		machineMuted.Render("the fleet camp can reach for switch and list --remote")
	return ui.ClampWidth(line, width)
}

func (m *machineTUIModel) renderFleetPane(lay machineLayout) string {
	inner := max(lay.leftWidth-4, 1)
	lines := []string{
		ui.ClampWidth(machineTitleStyle.Render("Fleet"), inner),
		machineMuted.Render(ui.CountLabel(len(m.rows), "machine", "machines")),
	}
	rows := max(lay.bodyRows-2, 1)
	start, end := ui.WindowRange(m.cursor, len(m.rows), rows)
	for i := start; i < end; i++ {
		selected := i == m.cursor
		prefix := ui.CursorGlyph(selected)
		style := machinePrimary
		if selected {
			style = machineSelected
		}
		text := machineRowText(m.rows[i], m.socketState(m.rows[i]), max(inner-machinePaneTextInset-lipgloss.Width(prefix), 1))
		lines = append(lines, prefix+style.Render(text))
	}
	return m.finishPane(lines, inner, lay.bodyRows, true)
}

// machineRowText fits one fleet row to width, dropping the socket badge before
// it truncates the machine id.
func machineRowText(row machineRow, socket remote.ControlMasterState, width int) string {
	name := row.id()
	badge := ""
	switch {
	case row.Local:
		badge = "this machine"
	case socket == remote.ControlLive:
		badge = "live"
	case socket == remote.ControlStale:
		badge = "stale"
	}
	if badge != "" {
		full := name + " · " + badge
		if lipgloss.Width(full) <= width {
			return full
		}
	}
	return ui.Truncate(name, width)
}

func (m *machineTUIModel) socketState(row machineRow) remote.ControlMasterState {
	if row.Local || row.Machine == nil {
		return remote.ControlNone
	}
	return m.sockets[row.Machine.ID].State
}

func (m *machineTUIModel) renderDetailPane(lay machineLayout) string {
	inner := max(lay.rightWidth-4, 1)
	row := m.selectedRow()

	lines := []string{ui.ClampWidth(machineTitleStyle.Render("Detail · "+row.id()), inner)}
	if row.Local {
		lines = append(lines,
			machineMuted.Render("the current machine"),
			"",
			machinePrimary.Render("Always reachable, and never written to machines.yaml."),
			machineMuted.Render("Every other row is an ssh target camp can hop to."),
		)
		return m.finishPane(lines, inner, lay.bodyRows, false)
	}

	machine := row.Machine
	diagnosis := m.sockets[machine.ID]
	lines = append(lines,
		machineMuted.Render("stored in ~/.obey/machines.yaml"),
		"",
		machineDetailRow("Label", machine.Label),
		machineDetailRow("Host", machine.Host),
		machineDetailRow("Auth", machine.AuthMethod),
		machineDetailRow("SSH user", machine.SSHUser),
		machineDetailRow("Identity", machine.IdentityFile),
		"",
		machineSocketRow(diagnosis),
		// Abbreviated like every other path this pane shows, and because an
		// absolute one spells out the operator's home directory and account
		// name in any screenshot or recording of this screen.
		machineMuted.Render(ui.Truncate(pathutil.AbbreviateHome(diagnosis.Socket), max(inner-machineOverlayTextInset, 1))),
	)
	if diagnosis.State == remote.ControlStale {
		lines = append(lines, "", machineWarn.Render("A stale socket can hang the next hop. Press R to clear it."))
	}
	if machine.AuthMethod == machines.AuthSSHPassword {
		lines = append(lines, "", machineWarn.Render("Password auth cannot switch or list remotely yet."))
	}
	return m.finishPane(lines, inner, lay.bodyRows, false)
}

func machineDetailRow(label, value string) string {
	if strings.TrimSpace(value) == "" {
		return machineMuted.Render(fmt.Sprintf("%-9s not set", label))
	}
	return machinePrimary.Render(fmt.Sprintf("%-9s ", label)) + machineSelected.Render(value)
}

func machineSocketRow(d remote.SocketDiagnosis) string {
	switch d.State {
	case remote.ControlLive:
		return machinePrimary.Render(fmt.Sprintf("%-9s ", "Socket")) + machineOKStyle.Render("live")
	case remote.ControlStale:
		return machinePrimary.Render(fmt.Sprintf("%-9s ", "Socket")) + machineWarn.Render("stale")
	default:
		return machinePrimary.Render(fmt.Sprintf("%-9s ", "Socket")) + machineMuted.Render("none · a hop opens a fresh one")
	}
}

func (m *machineTUIModel) finishPane(lines []string, width, rows int, focused bool) string {
	want := rows + 2
	for len(lines) < want {
		lines = append(lines, "")
	}
	if len(lines) > want {
		lines = lines[:want]
	}
	if m.layout().dual || m.width <= 0 {
		style := machinePaneBlurred
		if focused {
			style = machinePaneFocused
		}
		return style.Width(width).Render(strings.Join(ui.ClampLines(lines, max(width-machinePaneTextInset, 1)), "\n"))
	}
	return strings.Join(ui.ClampLines(lines, width), "\n")
}

func (m *machineTUIModel) statusLine() string {
	if m.status == "" {
		return ""
	}
	if m.statusErr {
		return machineErrorStyle.Render("✗ " + m.status)
	}
	return machineOKStyle.Render("✓ " + m.status)
}

func (m *machineTUIModel) footer(width int) string {
	full := "j/k: select · a: add · s: scan tailnet · e: edit · d: remove · r: refresh · R: clear socket · ?: help · q: quit"
	mid := "j/k select · a add · s scan · e edit · d remove · r refresh · ? help · q quit"
	short := "j/k · a/s/e/d · r · ? · q"
	return machineHelpStyle.Render(ui.CollapseHelp(width, full, mid, short, "q: quit"))
}

func (m *machineTUIModel) overlayView() string {
	lay := m.layout()
	var body []string

	switch m.overlay {
	case machineHelpOverlay:
		body = []string{
			machineTitleStyle.Render("Managing the fleet"),
			"",
			machinePrimary.Render("These machines are what 'camp switch machine:campaign' and"),
			machinePrimary.Render("'camp list --remote' can reach, over ssh."),
			"",
			machinePrimary.Render("a  add a machine by hand"),
			machinePrimary.Render("s  scan the tailnet and pick a device to prefill"),
			machinePrimary.Render("e  edit the selected machine"),
			machinePrimary.Render("d  remove the selected machine"),
			machinePrimary.Render("r  re-check every ControlMaster socket"),
			machinePrimary.Render("R  clear the selected machine's socket"),
			"",
			machineMuted.Render("A socket goes stale after a sleep or a network flap."),
			machineMuted.Render("Clearing it makes the next hop reconnect instead of hang."),
			"",
			machineHelpStyle.Render("esc or ?  close help"),
		}
	case machineDeleteOverlay:
		body = []string{
			machineTitleStyle.Render("Remove machine?"),
			"",
			machinePrimary.Render(fmt.Sprintf("Remove %q from machines.yaml?", m.pendingID)),
			machineMuted.Render("The machine itself is untouched; only camp forgets how to reach it."),
			"",
			machineHelpStyle.Render("y/enter remove · n/esc cancel"),
		}
	case machineDiscoverOverlay:
		body = m.discoverBody()
	case machineFormOverlay:
		body = m.formBody()
	}

	boxWidth := min(max(lay.width-6, 30), 76)
	inner := max(boxWidth-6, 24)
	box := machineOverlayStyle.Width(inner).
		Render(strings.Join(ui.ClampLines(body, max(inner-machineOverlayTextInset, 1)), "\n"))
	canvas := lipgloss.Place(lay.width, lay.height, lipgloss.Center, lipgloss.Center, box,
		lipgloss.WithWhitespaceBackground(machineTUIPal.BgOverlay))
	return ui.FitFullscreenView(canvas, lay.height)
}

func (m *machineTUIModel) discoverBody() []string {
	if m.scanning {
		return []string{
			machineTitleStyle.Render("Scanning the tailnet"),
			"",
			machineMuted.Render("Running 'tailscale status --json'..."),
		}
	}

	body := []string{
		machineTitleStyle.Render("Tailnet devices"),
		machineMuted.Render("Pick one to prefill the form; nothing is saved yet."),
		"",
	}
	// Tailnet host names repeat (phones and tablets both report "localhost"),
	// so the columns are aligned and the DNS name is always shown: it is what
	// actually tells two devices apart, and what the machine id derives from.
	for i, device := range m.devices {
		prefix := ui.CursorGlyph(i == m.deviceCursor)
		state := machineMuted.Render("offline")
		if device.Online {
			state = machineOKStyle.Render("online")
		}
		name := pad(ui.Truncate(device.HostName, 16), 16)
		if i == m.deviceCursor {
			name = machineSelected.Render(name)
		} else {
			name = machinePrimary.Render(name)
		}
		host := machineMuted.Render(pad(ui.Truncate(device.Host, 34), 34))
		body = append(body, prefix+name+" "+host+" "+state)
	}
	return append(body, "", machineHelpStyle.Render("j/k move · enter use · esc cancel"))
}

// pad right-pads s to width so picker columns line up. Widths are measured the
// way the terminal draws them, not in bytes.
func pad(s string, width int) string {
	if gap := width - lipgloss.Width(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}

func (m *machineTUIModel) formBody() []string {
	title := "Add machine"
	if m.form.editing() {
		title = "Edit machine · " + m.form.editID
	}
	body := []string{machineTitleStyle.Render(title)}
	if m.form.fromDiscovery {
		body = append(body, machineMuted.Render("Prefilled from the tailnet device you picked."))
	} else if m.form.editing() {
		body = append(body, machineMuted.Render("The id is the key this entry is stored under and cannot change."))
	} else {
		body = append(body, machineMuted.Render("camp reaches this machine over ssh; nothing runs until you hop."))
	}
	body = append(body, "")

	for _, field := range []struct {
		field machineFormField
		label string
	}{
		{machineFieldID, "Id"},
		{machineFieldLabel, "Label"},
		{machineFieldHost, "Host"},
		{machineFieldAuth, "Auth method"},
		{machineFieldUser, "SSH user"},
		{machineFieldIdentity, "Identity file"},
	} {
		body = append(body, m.formFieldLines(field.field, field.label)...)
	}

	if m.form.err != "" {
		body = append(body, machineErrorStyle.Render("✗ "+m.form.err))
	}
	return append(body, "", machineHelpStyle.Render("tab/shift+tab move · ←/→ auth · enter save · esc cancel"))
}

func (m *machineTUIModel) formFieldLines(field machineFormField, label string) []string {
	focused := m.form.field == field

	if field == machineFieldAuth {
		value := machinePrimary.Render(m.form.auth)
		if focused {
			value = machineSelected.Render("‹ " + m.form.auth + " ›")
		}
		return []string{machinePrimary.Render(label), "  " + value}
	}

	if field == machineFieldID && m.form.editing() {
		return []string{machineMuted.Render(label + " (fixed)"), machineMuted.Render("  " + m.form.editID)}
	}

	heading := machinePrimary.Render(label)
	if focused {
		heading = machineSelected.Render(label)
	}
	return []string{heading, m.form.input(field).View()}
}
