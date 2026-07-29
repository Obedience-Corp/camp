package main

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Obedience-Corp/camp/internal/ui"
)

// onboardingView is what `camp machine` shows before any machine is
// configured. The fleet layout is wrong for this state: two panes framing a
// single un-actionable "local" row tell a first-time user nothing about what a
// machine is, why they would add one, or what they get afterwards.
//
// This screen answers those three in order, and when tailscale is installed it
// answers the third with the user's own devices rather than an instruction to
// go find them.
func (m *machineTUIModel) onboardingView() string {
	lay := m.layout()
	width := min(lay.width, 96)

	lines := []string{
		machineTitleStyle.Render("Machines"),
		"",
		machinePrimary.Render("Work on campaigns that live on your other computers."),
		machinePrimary.Render("Add one here and you can reach it from this terminal:"),
		"",
		machineCommand("camp switch devbox:my-campaign", "open a campaign over there"),
		machineCommand("camp list --remote", "campaigns on every machine at once"),
		"",
		machineMuted.Render(strings.Repeat("─", max(width-4, 10))),
		"",
	}
	lines = append(lines, m.onboardingBody(width)...)
	lines = append(lines,
		"",
		machineMuted.Render(strings.Repeat("─", max(width-4, 10))),
		"",
		machineMuted.Render("This computer is always available as \"local\" and needs no setup."),
	)

	body := strings.Join(ui.ClampLines(lines, max(width-4, 10)), "\n")
	framed := []string{"", lipgloss.NewStyle().PaddingLeft(2).Render(body)}
	if status := m.statusLine(); status != "" {
		framed = append(framed, "", "  "+status)
	}
	framed = append(framed, "", "  "+machineHelpStyle.Render(m.onboardingFooter()))
	return ui.FitFullscreenView(strings.Join(ui.CapFrame(framed, lay.width, lay.height), "\n"), lay.height)
}

func (m *machineTUIModel) onboardingFooter() string {
	if len(m.devices) > 0 {
		return "enter add the highlighted device · a add by hand · s rescan · ? what is this · q quit"
	}
	// Rescan is live whenever tailscale is ready: the body tells the user
	// "s tries the scan again" on a failed scan, and the empty-device case
	// still accepts s. The footer must list the same key the body teaches.
	if m.tailscaleReady {
		if m.scanning {
			return "scanning · a add by hand · ? what is this · q quit"
		}
		return "a add a machine · s rescan · ? what is this · q quit"
	}
	return "a add a machine · ? what is this · q quit"
}

// onboardingBody is the part of the screen that changes with what camp can
// see: the tailnet when there is one, and the reason there is not otherwise.
func (m *machineTUIModel) onboardingBody(width int) []string {
	switch {
	case m.scanning:
		return []string{
			machinePrimary.Render(m.spin.View() + " Looking for machines on your Tailscale network..."),
		}

	case !m.tailscaleReady:
		return []string{
			machineSelected.Render("No machines yet"),
			"",
			machineAction("a", "Add a machine", "Its hostname, your ssh user, and a key your agent already holds."),
			"",
			machineMuted.Render("Tailscale is not installed. With it, camp can list the machines on"),
			machineMuted.Render("your network and fill this in for you."),
		}

	case m.scanErr != "":
		return []string{
			machineSelected.Render("No machines yet"),
			"",
			machineAction("a", "Add a machine", "Its hostname, your ssh user, and a key your agent already holds."),
			"",
			machineWarn.Render("Tailscale did not answer:"),
			machineMuted.Render("  " + ui.Truncate(m.scanErr, max(width-8, 20))),
			machineMuted.Render("  s tries the scan again."),
		}

	case len(m.devices) == 0:
		return []string{
			machineSelected.Render("No machines yet"),
			"",
			machineMuted.Render("Tailscale is running but reports no other devices."),
			machineMuted.Render("s tries the scan again in case a machine just came online."),
			"",
			machineAction("a", "Add a machine by hand", "For any ssh host, on the tailnet or not."),
		}

	default:
		return m.onboardingDeviceList(width)
	}
}

// onboardingDeviceList is the payoff of having tailscale installed: the user's
// own machines, ready to add, instead of a form asking for a hostname they
// would have to go look up.
func (m *machineTUIModel) onboardingDeviceList(width int) []string {
	lines := []string{
		machineSelected.Render(ui.CountLabel(len(m.devices), "machine", "machines") + " on your Tailscale network"),
		machineMuted.Render("Pick one to add. Nothing is saved until you confirm the details."),
		"",
	}
	lines = append(lines, m.deviceRows(width)...)
	return append(lines,
		"",
		machineAction("a", "Add a machine by hand", "For an ssh host that is not on your tailnet."),
	)
}

// deviceRows renders the tailnet device list shared by the onboarding screen
// and the scan overlay, marking any device already in the fleet so the same
// machine is not added twice under a second id.
func (m *machineTUIModel) deviceRows(width int) []string {
	configured := m.configuredHosts()
	nameWidth, hostWidth := 16, min(max(width-42, 20), 34)

	rows := make([]string, 0, len(m.devices))
	for i, device := range m.devices {
		prefix := ui.CursorGlyph(i == m.deviceCursor)

		name := pad(ui.Truncate(device.HostName, nameWidth), nameWidth)
		if i == m.deviceCursor {
			name = machineSelected.Render(name)
		} else {
			name = machinePrimary.Render(name)
		}

		host := machineMuted.Render(pad(ui.Truncate(device.Host, hostWidth), hostWidth))

		state := machineMuted.Render("offline")
		if device.Online {
			state = machineOKStyle.Render("online ")
		}

		row := prefix + name + " " + host + " " + state
		if id, ok := configured[device.Host]; ok {
			row += machineMuted.Render("  already added as " + id)
		}
		rows = append(rows, row)
	}
	return rows
}

// machineCommand renders a command the user can copy, with what it does.
func machineCommand(command, explanation string) string {
	return "  " + machineSelected.Render(pad(command, 34)) + machineMuted.Render(explanation)
}

// machineAction renders a keybinding as an offer rather than a legend entry:
// the key, what it does, and what it will ask for.
func machineAction(key, title, detail string) string {
	lines := machineSelected.Render("  "+key) + "  " + machinePrimary.Render(title)
	if detail == "" {
		return lines
	}
	return lines + "\n     " + machineMuted.Render(detail)
}
