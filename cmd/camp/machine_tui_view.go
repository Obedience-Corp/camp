package main

import (
	"fmt"
	"strings"
	"unicode/utf8"

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
	if m.empty() {
		return m.onboardingView()
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
			lines = append(lines, ui.ClampWidth(status, lay.width))
		}
		lines = append(lines, m.footer(lay.width))
	}
	return ui.FitFullscreenView(strings.Join(ui.CapFrame(lines, lay.width, lay.height), "\n"), lay.height)
}

func (m *machineTUIModel) topBar(width int) string {
	line := machineTitleStyle.Render("Machines") + "  " +
		machineMuted.Render("computers camp can open campaigns on, over ssh")
	return ui.ClampWidth(line, width)
}

func (m *machineTUIModel) renderFleetPane(lay machineLayout) string {
	inner := max(lay.leftWidth-4, 1)
	lines := []string{
		ui.ClampWidth(machineTitleStyle.Render("Machines"), inner),
		machineMuted.Render(ui.CountLabel(len(m.file.Machines), "configured", "configured")),
	}
	rows := max(lay.bodyRows-2, 1)
	start, end := ui.WindowRange(m.cursor, len(m.rows), rows)
	for i := start; i < end; i++ {
		selected := i == m.cursor
		prefix := ui.CursorGlyph(selected)
		width := max(inner-machinePaneTextInset-lipgloss.Width(prefix), 1)
		lines = append(lines, prefix+m.fleetRow(m.rows[i], selected, width))
	}
	return m.finishPane(lines, inner, lay.bodyRows, true)
}

// fleetRow renders one row of the list: the machine, and whether camp could
// reach it. Reachability leads because it is the question the screen exists to
// answer; the ssh session-reuse state is a detail of how, and lives in the
// pane rather than competing for the row.
func (m *machineTUIModel) fleetRow(row machineRow, selected bool, width int) string {
	name := row.id()
	column := m.nameColumnWidth()
	style := machinePrimary
	if selected {
		style = machineSelected
	}

	if row.Local {
		badge := machineMuted.Render("this computer")
		if column+lipgloss.Width("this computer") <= width {
			return style.Render(pad(name, column)) + badge
		}
		return style.Render(ui.Truncate(name, width))
	}

	glyph, label, badgeStyle := healthBadge(m.health[row.Machine.ID].State)
	if m.health[row.Machine.ID].State == healthTesting {
		glyph = strings.TrimSpace(m.spin.View())
	}
	if column+lipgloss.Width(glyph+" "+label) > width {
		return style.Render(ui.Truncate(name, width))
	}
	return style.Render(pad(name, column)) + badgeStyle.Render(glyph+" "+label)
}

// nameColumnWidth sizes the id column to the widest id in the fleet, so a long
// machine name cannot run into the status badge beside it.
func (m *machineTUIModel) nameColumnWidth() int {
	widest := len(machines.LocalMachineID)
	for _, mach := range m.file.Machines {
		if w := lipgloss.Width(mach.ID); w > widest {
			widest = w
		}
	}
	return widest + 2
}

// healthBadge maps a health state to its glyph, word, and style. "not tested"
// is deliberately neutral rather than a warning: an untested machine is not a
// problem, it is simply a question nobody has asked yet.
func healthBadge(state healthState) (string, string, lipgloss.Style) {
	switch state {
	case healthReachable:
		return "●", "reachable", machineOKStyle
	case healthUnreachable:
		return "✗", "unreachable", machineErrorStyle
	case healthAuthDenied:
		return "✗", "login denied", machineErrorStyle
	case healthCampMissing:
		return "✗", "camp not found", machineErrorStyle
	case healthUnsupported:
		return "!", "unsupported", machineWarn
	case healthTesting:
		return "◐", "testing...", machineMuted
	default:
		return "○", "not tested", machineMuted
	}
}

func (m *machineTUIModel) renderDetailPane(lay machineLayout) string {
	inner := max(lay.rightWidth-4, 1)
	row := m.selectedRow()

	if row.Local {
		return m.finishPane([]string{
			ui.ClampWidth(machineTitleStyle.Render(row.id()), inner),
			machineMuted.Render("this computer"),
			"",
			machinePrimary.Render("Always available. camp uses it whenever you do not"),
			machinePrimary.Render("name a machine, and it is never saved to a file."),
			"",
			machineMuted.Render("Every other row is a computer reached over ssh."),
		}, inner, lay.bodyRows, false)
	}

	machine := row.Machine
	lines := []string{ui.ClampWidth(machineTitleStyle.Render(machine.ID), inner)}
	if machine.Label != "" && machine.Label != machine.ID {
		lines = append(lines, machineMuted.Render(machine.Label))
	} else {
		lines = append(lines, machineMuted.Render("stored in ~/.obey/machines.yaml"))
	}

	lines = append(lines, "")
	lines = append(lines, m.healthSection(machine.ID, inner)...)
	lines = append(lines, "", machinePrimary.Render("Work on it"))
	lines = append(lines,
		machineMuted.Render("  camp switch ")+machineSelected.Render(machine.ID)+machineMuted.Render(":<campaign>"),
		machineMuted.Render("  camp list --remote"),
	)

	lines = append(lines, "", machinePrimary.Render("Connection"))
	lines = append(lines,
		machineDetailRow("Host", machine.Host, ""),
		machineDetailRow("Sign-in", authLabel(machine.AuthMethod), ""),
		machineDetailRow("SSH user", machine.SSHUser, "your login name"),
	)
	if machine.IdentityFile != "" {
		lines = append(lines, machineDetailRow("Key", machine.IdentityFile, ""))
	}

	lines = append(lines, "")
	lines = append(lines, m.reuseSection(machine.ID, inner)...)
	return m.finishPane(lines, inner, lay.bodyRows, false)
}

// healthSection leads the pane with whether camp can reach the machine, and
// when it cannot, with why and what to press next.
func (m *machineTUIModel) healthSection(id string, width int) []string {
	health := m.health[id]
	glyph, _, style := healthBadge(health.State)

	switch health.State {
	case healthReachable:
		headline := style.Render(glyph + " Reachable")
		if health.Version != "" {
			headline += machineMuted.Render("  ·  camp " + health.Version)
		}
		return []string{headline}
	case healthTesting:
		return []string{style.Render(m.spin.View() + " Testing the connection...")}
	case healthUnreachable:
		detailWidth := max(width-4, 20)
		if health.CheckURL != "" {
			// Check-mode is auth policy, not network failure — do not frame it
			// as unreachable or operators chase connectivity.
			return append([]string{style.Render(glyph + " Needs Tailscale SSH check")},
				checkURLLines(health.CheckURL, max(width-machinePaneTextInset, 20),
					"Tailscale wants a one-time browser approval.",
					"o open it · c copy it · t try again")...)
		}
		lines := []string{style.Render(glyph + " Could not reach it")}
		for _, line := range healthDetailLines(health.Detail, detailWidth, false) {
			lines = append(lines, machineMuted.Render("  "+line))
		}
		return append(lines, machineMuted.Render("  e edits it · t tries again"))
	case healthAuthDenied:
		lines := []string{style.Render(glyph + " SSH login denied")}
		for _, line := range healthDetailLines(health.Detail, max(width-4, 20), false) {
			lines = append(lines, machineMuted.Render("  "+line))
		}
		lines = append(lines, machineMuted.Render("  e edit login/key · t retry"))
		for _, line := range wrapDisplayWidth(pairFromPeerHint(id), max(width-4, 20)) {
			lines = append(lines, machineMuted.Render("  "+line))
		}
		return lines
	case healthCampMissing:
		// ssh got in; the machine simply has no camp where camp looks. Say
		// that, not "unreachable", or the operator debugs the network.
		lines := []string{style.Render(glyph + " Reached it, but camp is not installed there")}
		for _, line := range healthDetailLines(health.Detail, max(width-4, 20), false) {
			lines = append(lines, machineMuted.Render("  "+line))
		}
		return append(lines, machineMuted.Render("  install camp on it (or set CAMP_REMOTE_CAMP_PATH) · t tries again"))
	case healthUnsupported:
		return []string{
			style.Render(glyph + " Cannot be used yet"),
			machineMuted.Render("  " + ui.Truncate(health.Detail, max(width-4, 20))),
			machineMuted.Render("  Switch it to Tailscale SSH or an agent key with e."),
		}
	default:
		return []string{
			style.Render(glyph + " Not tested yet"),
			machineMuted.Render("  t checks whether camp can reach it."),
		}
	}
}

// checkURLLines renders a Tailscale approval prompt: why, the bare URL on a
// line of its own, and the keys that act on it. lineWidth is the columns a
// finished line gets before the frame clamps it, indent included.
//
// The URL gets its own line because that is the only way it survives. Inline in
// a sentence it was wrapped mid-token in the detail pane and truncated away
// entirely in the hop overlay, so the one thing the operator had to act on was
// the one thing they could not read or copy.
//
// It also gives up its indent rather than wrap. Two columns decide whether a
// check URL fits on one line in an 80-column terminal, and one line is what
// makes it selectable with a double-click; alignment is worth less than that.
func checkURLLines(url string, lineWidth int, reason, keys string) []string {
	textWidth := max(lineWidth-2, 20)

	lines := make([]string, 0, 5)
	for _, line := range wrapDisplayWidth(reason, textWidth) {
		lines = append(lines, machineMuted.Render("  "+line))
	}
	if lipgloss.Width(url) <= lineWidth {
		indent := "  "
		if lipgloss.Width(url) > textWidth {
			indent = ""
		}
		lines = append(lines, machineSelected.Render(indent+url))
	} else {
		for _, line := range hardWrapDisplayWidth(url, lineWidth) {
			lines = append(lines, machineSelected.Render(line))
		}
	}
	return append(lines, machineMuted.Render("  "+keys))
}

// wrapDisplayWidth breaks text onto lines of at most width columns, at spaces.
// A word longer than width is left alone rather than split: the callers wrap
// prose here, and prose has no token worth cutting mid-word.
func wrapDisplayWidth(text string, width int) []string {
	if width < 8 {
		width = 8
	}
	var lines []string
	current := ""
	for _, word := range strings.Fields(text) {
		switch {
		case current == "":
			current = word
		case lipgloss.Width(current)+1+lipgloss.Width(word) <= width:
			current += " " + word
		default:
			lines = append(lines, current)
			current = word
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

// healthDetailLines formats a connection-failure detail for the detail pane.
// Tailscale check messages keep the full URL, hard-wrapped at maxWidth so a
// narrow pane still shows the whole token. Other details still truncate.
//
// maxWidth is a terminal-column budget (lipgloss/display width), not raw
// bytes, so multi-byte runes (e.g. the em dash in Tailscale guidance) are
// not split into invalid UTF-8.
func healthDetailLines(detail string, maxWidth int, keepFullURL bool) []string {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return nil
	}
	if maxWidth < 8 {
		maxWidth = 8
	}
	if !keepFullURL {
		return []string{ui.Truncate(detail, maxWidth)}
	}
	return hardWrapDisplayWidth(detail, maxWidth)
}

// hardWrapDisplayWidth breaks text into lines of at most maxWidth columns,
// splitting mid-token when it has to. Unlike wrapDisplayWidth it never lets a
// line overflow, because its job is the case where the token IS the content: a
// URL too long for the pane must still be readable in full, and an overflowing
// line is clipped by the frame rather than shown.
//
// maxWidth is a terminal-column budget (lipgloss/display width), not raw bytes,
// so multi-byte runes (e.g. the em dash in Tailscale guidance) are not split
// into invalid UTF-8.
func hardWrapDisplayWidth(text string, maxWidth int) []string {
	if maxWidth < 8 {
		maxWidth = 8
	}
	// Prefer breaking after path separators so "https://…/a/…" remains readable.
	var lines []string
	for lipgloss.Width(text) > maxWidth {
		cut := cutDisplayWidth(text, maxWidth)
		if cut <= 0 {
			_, size := utf8.DecodeRuneInString(text)
			cut = size
		} else if soft := strings.LastIndexAny(text[:cut], "/ ?&="); soft > cut/3 {
			// soft is a byte index of a break char; advance past it.
			cut = soft + 1
		}
		lines = append(lines, text[:cut])
		text = text[cut:]
	}
	if text != "" {
		lines = append(lines, text)
	}
	return lines
}

// cutDisplayWidth returns a byte index into s such that s[:i] fits in at most
// maxCols display columns and ends on a rune boundary.
func cutDisplayWidth(s string, maxCols int) int {
	if maxCols <= 0 || s == "" {
		return 0
	}
	width := 0
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		rw := lipgloss.Width(string(r))
		if rw < 1 {
			rw = 1
		}
		if width+rw > maxCols {
			if i == 0 {
				// Single rune wider than budget — still emit it.
				return size
			}
			return i
		}
		width += rw
		i += size
	}
	return len(s)
}

// reuseSection explains the ControlMaster socket in terms of what it does for
// the user, and only raises it as a problem when it actually is one.
func (m *machineTUIModel) reuseSection(id string, width int) []string {
	diagnosis := m.sockets[id]
	switch diagnosis.State {
	case remote.ControlLive:
		return []string{
			machinePrimary.Render("Connection reuse  ") + machineOKStyle.Render("open"),
			machineMuted.Render("  Later hops to this machine are instant."),
		}
	case remote.ControlStale:
		return []string{
			machinePrimary.Render("Connection reuse  ") + machineWarn.Render("stuck"),
			machineMuted.Render("  A sleep or network drop left this behind. It can hang"),
			machineMuted.Render("  the next hop until R clears it."),
			machineMuted.Render("  " + ui.Truncate(pathutil.AbbreviateHome(diagnosis.Socket), max(width-4, 20))),
		}
	default:
		return []string{
			machinePrimary.Render("Connection reuse  ") + machineMuted.Render("idle"),
			machineMuted.Render("  The first hop opens a session camp keeps warm."),
		}
	}
}

// machineDetailRow renders a label/value pair, showing what camp will fall
// back to when the value is unset rather than the bare word "not set".
func machineDetailRow(label, value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		if fallback == "" {
			fallback = "not set"
		}
		return machineMuted.Render(fmt.Sprintf("  %-9s %s", label, fallback))
	}
	return machineMuted.Render(fmt.Sprintf("  %-9s ", label)) + machinePrimary.Render(value)
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
	// A failed hop carries ssh's whole complaint. Ending it in an ellipsis
	// reads as a message that continues, which it does: the pane beside the
	// list shows the same reason with room to breathe.
	message := ui.Truncate(m.status, max(m.layout().width-4, 20))
	switch m.statusKind {
	case statusError:
		return machineErrorStyle.Render("✗ " + message)
	case statusAdvice:
		return machineMuted.Render("· " + message)
	default:
		return machineOKStyle.Render("✓ " + message)
	}
}

// footer groups keys by what they are for rather than listing eight of them at
// equal weight. The action that answers "does this work" comes first, because
// on a screen full of untested machines it is the one worth pressing.
func (m *machineTUIModel) footer(width int) string {
	// o/c are listed only while an approval link is actually waiting. They act
	// on the selected machine's last test, so advertising them permanently would
	// promise something most rows cannot do.
	if m.selectedCheckURL() != "" {
		full := "o open the approval link · c copy it  ·  t retry  ·  enter hop · e edit · a add · ? help · q quit"
		mid := "o open link · c copy · t retry · enter hop · e edit · ? help · q quit"
		short := "o open link · c copy · t retry · q quit"
		return machineHelpStyle.Render(ui.CollapseHelp(width, full, mid, short, "o: open link"))
	}
	full := "enter hop  ·  t test connection  ·  p pair · e edit · d remove  ·  a add · s scan tailnet  ·  ? help · q quit"
	mid := "enter hop · t test · p pair · e edit · d remove · a add · ? help · q quit"
	short := "enter hop · t test · p pair · ? help · q quit"
	return machineHelpStyle.Render(ui.CollapseHelp(width, full, mid, short, "q: quit"))
}

// overlayInnerWidth is the width of the overlay box, and overlayTextWidth the
// columns a body line actually gets inside it. Bodies that wrap their own text
// must measure with the same numbers the frame clamps with, or they wrap to a
// width the frame then truncates again.
func (m *machineTUIModel) overlayInnerWidth() int {
	boxWidth := min(max(m.layout().width-6, 30), 76)
	return max(boxWidth-6, 24)
}

func (m *machineTUIModel) overlayTextWidth() int {
	return max(m.overlayInnerWidth()-machineOverlayTextInset, 1)
}

func (m *machineTUIModel) overlayView() string {
	lay := m.layout()
	var body []string

	switch m.overlay {
	case machineHelpOverlay:
		body = []string{
			machineTitleStyle.Render("What this is"),
			"",
			machinePrimary.Render("A machine is another computer camp can reach over ssh."),
			machinePrimary.Render("Once one is listed here you can work on campaigns that"),
			machinePrimary.Render("live on it, without leaving this terminal:"),
			"",
			machineCommand("enter", "pick a campaign and hop there"),
			machineCommand("camp switch devbox:my-campaign", "the same hop, typed"),
			machineCommand("camp list --remote", "campaigns everywhere"),
			"",
			machineMuted.Render("Nothing runs on a machine until you hop to it."),
			"",
			machineTitleStyle.Render("Keys"),
			machinePrimary.Render("  enter  pick a campaign on the selected machine and hop"),
			machinePrimary.Render("  t  test whether camp can reach the selected machine"),
			machinePrimary.Render("  p  pair by exchanging dedicated ssh keys (one direction must work)"),
			machinePrimary.Render("  a  add a machine by hand"),
			machinePrimary.Render("  s  scan your Tailscale network and pick a device"),
			machinePrimary.Render("  e  edit      d  remove      j/k  move"),
			machinePrimary.Render("  r  re-check connection reuse    R  clear a stuck one"),
			machinePrimary.Render("  o  open a waiting Tailscale approval link   c  copy it"),
			"",
			machineTitleStyle.Render("Sign-in methods"),
			machineMuted.Render("  Tailscale SSH   Tailscale handles the keys for you."),
			machineMuted.Render("  ssh agent key   Uses a key your ssh agent already holds."),
			machineMuted.Render("  password        Not supported for camp hops yet."),
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
	case machineHopOverlay:
		body = m.hopBody()
	}

	inner := m.overlayInnerWidth()
	box := machineOverlayStyle.Width(inner).
		Render(strings.Join(ui.ClampLines(body, m.overlayTextWidth()), "\n"))
	canvas := lipgloss.Place(lay.width, lay.height, lipgloss.Center, lipgloss.Center, box,
		lipgloss.WithWhitespaceBackground(machineTUIPal.BgOverlay))
	return ui.FitFullscreenView(canvas, lay.height)
}

// hopBody renders the campaign picker for a hop. It always says where the list
// came from: a snapshot can be up to 24h old, and an operator who cannot find a
// campaign they just created needs to know refreshing is the fix rather than
// concluding the machine is broken.
func (m *machineTUIModel) hopBody() []string {
	title := machineTitleStyle.Render("Hop to " + m.hop.machineID)

	if m.hop.loading {
		return []string{
			title,
			"",
			machineMuted.Render(m.spin.View() + " Asking " + m.hop.machineID + " for its campaigns..."),
			"",
			machineHelpStyle.Render("esc cancel"),
		}
	}

	body := []string{title}
	switch {
	case m.hop.checkURL != "":
		// Check mode is not a hop failure the operator debugs, it is one they
		// finish. Lead with the link and the key that opens it; the diagnose
		// pointer below is for the failures that need investigating, not this one.
		body = append(body, "", machineErrorStyle.Render("✗ Needs Tailscale SSH check"))
		body = append(body, checkURLLines(m.hop.checkURL, m.overlayTextWidth(),
			"Tailscale wants a one-time browser approval before "+m.hop.machineID+" will let camp in.",
			"o open it · c copy it · r retry · esc cancel")...)
		return body
	case m.hop.err != "":
		// Wrapped, not truncated: the overlay is ~66 columns of text and ssh's
		// complaint is routinely longer, so a single line ended mid-sentence.
		body = append(body, "")
		for _, line := range wrapDisplayWidth("✗ "+m.hop.err, m.overlayTextWidth()) {
			body = append(body, machineErrorStyle.Render(line))
		}
		body = append(body,
			"",
			machineMuted.Render("'camp machine diagnose "+m.hop.machineID+"' reports auth, probe, and socket state."),
			"",
			machineHelpStyle.Render("r retry · esc cancel"),
		)
		return body
	case len(m.hop.campaigns) == 0:
		body = append(body,
			"",
			machineMuted.Render("No campaigns found on "+m.hop.machineID+"."),
			"",
			machineHelpStyle.Render("r refresh · esc cancel"),
		)
		return body
	}

	if m.hop.cached {
		body = append(body, machineMuted.Render("From the last snapshot; press r to ask the machine now."))
	} else {
		body = append(body, machineMuted.Render("Live from "+m.hop.machineID+"."))
	}
	body = append(body, "")

	for i, name := range m.hop.campaigns {
		line := "  " + name
		if i == clampIndex(m.hop.cursor, len(m.hop.campaigns)) {
			body = append(body, machineSelected.Render("› "+name))
			continue
		}
		body = append(body, machinePrimary.Render(line))
	}

	return append(body, "", machineHelpStyle.Render("j/k move · enter hop · r refresh · esc cancel"))
}

func (m *machineTUIModel) discoverBody() []string {
	if m.scanning {
		return []string{
			machineTitleStyle.Render("Scanning the tailnet"),
			"",
			machineMuted.Render(m.spin.View() + " Running 'tailscale status --json'..."),
		}
	}

	body := []string{
		machineTitleStyle.Render("Your Tailscale network"),
		machineMuted.Render("Pick one to fill in the form. Nothing is saved until you confirm."),
		"",
	}
	// Tailnet host names repeat (phones and tablets both report "localhost"),
	// so the columns are aligned and the DNS name is always shown: it is what
	// actually tells two devices apart, and what the machine id derives from.
	body = append(body, m.deviceRows(64)...)
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
		// Named the way the rest of the screen names it. The file still gets
		// the raw value; only the reading of it changes.
		value := machinePrimary.Render(authLabel(m.form.auth))
		if focused {
			value = machineSelected.Render("‹ " + authLabel(m.form.auth) + " ›")
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
