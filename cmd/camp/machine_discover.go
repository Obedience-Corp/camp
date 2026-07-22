package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/machines"
	"github.com/Obedience-Corp/camp/internal/ui"
)

// discoveredDevice is camp's mirror of the festival app's DiscoveredDevice
// (projects/festival-app/src-tauri/src/commands/tailscale.rs:15-23): the same
// fields the app surfaces to its own picker (host_name/host/dns_name/online/os),
// so a terminal user and an app user see the same tailnet device shape. The
// app's struct carries no node-id field — Tailscale node keys are only used
// internally (as the Peer map key) to exclude Self, never surfaced — so this
// type doesn't invent one either.
type discoveredDevice struct {
	HostName string
	Host     string
	DNSName  string
	Online   bool
	OS       string
}

// tailscaleStatus is the subset of `tailscale status --json` camp reads. It
// mirrors the app's TailscaleStatus/TailscaleNode (tailscale.rs:33-63): Self is
// excluded from the result by DNSName match, and a Peer entry with neither a
// usable DNSName nor a TailscaleIP is skipped (mirroring into_discovered_device,
// tailscale.rs:243-273).
type tailscaleStatus struct {
	BackendState string                   `json:"BackendState"`
	Self         *tailscaleNode           `json:"Self"`
	Peer         map[string]tailscaleNode `json:"Peer"`
}

type tailscaleNode struct {
	HostName     string   `json:"HostName"`
	DNSName      string   `json:"DNSName"`
	TailscaleIPs []string `json:"TailscaleIPs"`
	Online       bool     `json:"Online"`
	OS           string   `json:"OS"`
}

// parseTailscaleStatus mirrors the app's interpret_tailscale_status
// (tailscale.rs:182-228): skip any warning banner before the first '{', reject
// a non-"Running" backend with the same actionable messages
// (backend_state_message, tailscale.rs:230-237), drop Self from the peer set by
// DNSName match, and sort by host name then host for a stable picker order.
func parseTailscaleStatus(data []byte) ([]discoveredDevice, error) {
	start := bytes.IndexByte(data, '{')
	if start < 0 {
		return nil, camperrors.New("could not parse tailscale status output")
	}
	var status tailscaleStatus
	if err := json.Unmarshal(data[start:], &status); err != nil {
		return nil, camperrors.Wrap(err, "could not parse tailscale status")
	}
	if status.BackendState != "" && status.BackendState != "Running" {
		return nil, camperrors.New(backendStateMessage(status.BackendState))
	}

	selfDNS := ""
	if status.Self != nil {
		selfDNS = normalizeDNSName(status.Self.DNSName)
	}

	devices := make([]discoveredDevice, 0, len(status.Peer))
	for _, node := range status.Peer {
		dev, ok := node.toDiscoveredDevice()
		if !ok {
			continue
		}
		if selfDNS != "" && dev.DNSName == selfDNS {
			continue
		}
		devices = append(devices, dev)
	}
	sortDevices(devices)
	return devices, nil
}

func sortDevices(devices []discoveredDevice) {
	sort.Slice(devices, func(i, j int) bool {
		li, lj := strings.ToLower(devices[i].HostName), strings.ToLower(devices[j].HostName)
		if li != lj {
			return li < lj
		}
		return devices[i].Host < devices[j].Host
	})
}

// backendStateMessage mirrors the app's backend_state_message (tailscale.rs:230-237).
func backendStateMessage(state string) string {
	switch state {
	case "NeedsLogin":
		return "tailscale is logged out; run `tailscale up` to sign in"
	case "Stopped":
		return "tailscale is stopped; start tailscale and try again"
	case "NoState", "":
		return "tailscale is not ready yet; make sure tailscale is running"
	default:
		return fmt.Sprintf("tailscale is not running (state: %s)", state)
	}
}

// normalizeDNSName mirrors the app's normalize_dns_name: trim whitespace and
// the trailing FQDN dot MagicDNS names carry ("devbox.tailnet.ts.net.").
func normalizeDNSName(v string) string {
	return strings.TrimSuffix(strings.TrimSpace(v), ".")
}

// toDiscoveredDevice mirrors TailscaleNode::into_discovered_device
// (tailscale.rs:243-273): prefer the normalized DNSName as the ssh host,
// falling back to the first non-empty TailscaleIP; a node with neither is not
// usable and is skipped (ok=false).
func (n tailscaleNode) toDiscoveredDevice() (discoveredDevice, bool) {
	dnsName := normalizeDNSName(n.DNSName)
	host := dnsName
	if host == "" {
		host = firstNonEmpty(n.TailscaleIPs)
	}
	if host == "" {
		return discoveredDevice{}, false
	}
	hostName := strings.TrimSpace(n.HostName)
	if hostName == "" {
		hostName = host
	}
	return discoveredDevice{
		HostName: hostName,
		Host:     host,
		DNSName:  dnsName,
		Online:   n.Online,
		OS:       strings.TrimSpace(n.OS),
	}, true
}

func firstNonEmpty(values []string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// sanitizeID derives a machine-id candidate from a MagicDNS name by keeping
// only the leftmost DNS label: "devbox.tailnet.ts.net" -> "devbox". Callers
// still validate the result with config.ValidateName (via deriveMachineID)
// before using it, since a HostName fallback (e.g. "MacBook Pro (2)") won't
// necessarily produce a valid id.
func sanitizeID(name string) string {
	name = normalizeDNSName(name)
	if before, _, found := strings.Cut(name, "."); found {
		name = before
	}
	return strings.ToLower(name)
}

// deriveMachineID picks a default id for a discovered device (DNSName
// preferred, HostName as fallback) and validates it, so a device whose name
// can't sanitize into a valid id (spaces, punctuation) fails with a clear
// message instead of silently producing an unusable machines.yaml entry.
func deriveMachineID(d discoveredDevice) (string, error) {
	source := d.DNSName
	if source == "" {
		source = d.HostName
	}
	id := sanitizeID(source)
	if err := validateMachineID(id); err != nil {
		return "", camperrors.Wrapf(err,
			"could not derive a valid machine id from %q; pass one explicitly: camp machine add --discover <id>", source)
	}
	return id, nil
}

// tailscaleStatusFunc runs `tailscale status --json` and returns raw stdout.
// Production code uses runTailscaleStatus (real exec); tests inject a fake so
// discoverTailnet is exercised without ever invoking a real tailscale binary.
type tailscaleStatusFunc func(ctx context.Context) ([]byte, error)

func discoverTailnet(ctx context.Context, run tailscaleStatusFunc) ([]discoveredDevice, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out, err := run(ctx)
	if err != nil {
		return nil, err
	}
	return parseTailscaleStatus(out)
}

// runTailscaleStatus execs the local tailscale binary. A missing binary or a
// non-zero exit becomes a clear, actionable error instead of a raw exec error
// or stack trace.
func runTailscaleStatus(ctx context.Context) ([]byte, error) {
	if _, err := exec.LookPath("tailscale"); err != nil {
		return nil, camperrors.Wrap(err, "tailscale CLI not found; install Tailscale and ensure `tailscale` is on PATH")
	}
	cmd := exec.CommandContext(ctx, "tailscale", "status", "--json")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return nil, camperrors.New("tailscale status --json failed (is tailscale installed and up?): " + detail)
	}
	return stdout.Bytes(), nil
}

func runMachineAddDiscover(cmd *cobra.Command, args []string) error {
	return runMachineAddDiscoverWith(cmd, args, runTailscaleStatus)
}

// runMachineAddDiscoverWith is the testable discover path: production passes
// runTailscaleStatus; tests inject a fixture so no live tailscale is required.
func runMachineAddDiscoverWith(cmd *cobra.Command, args []string, run tailscaleStatusFunc) error {
	if machineAddHost != "" {
		return camperrors.New("cannot combine --discover with --host")
	}
	devices, err := discoverTailnet(cmd.Context(), run)
	if err != nil {
		return err
	}
	if len(devices) == 0 {
		return camperrors.New("no tailnet devices found")
	}

	dev, id, err := selectDiscoveredDevice(cmd.Context(), devices, args)
	if err != nil {
		return err
	}

	label := machineAddLabel
	if label == "" {
		label = dev.HostName
	}

	// D2: discover fills network identity (host/id/label); auth defaults to
	// OpenSSH (ssh-agent) like manual add. --auth / --user / --identity are
	// honored (previously silently ignored on the discover path).
	auth, err := normalizeAuthMethod(machineAddAuth)
	if err != nil {
		return err
	}

	mf, err := machines.Load()
	if err != nil {
		return err
	}
	mf.Upsert(machines.Machine{
		ID:           id,
		Label:        label,
		Host:         dev.Host,
		AuthMethod:   auth,
		SSHUser:      machineAddUser,
		IdentityFile: machineAddIdentity,
	})
	if err := mf.Save(); err != nil {
		return err
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s machine %q saved (%s, %s)\n", ui.SuccessIcon(), id, dev.Host, auth)
	return err
}

// selectDiscoveredDevice resolves which discovered device to save and the id
// to save it under. A positional id argument matches a device by its derived
// id (non-interactive); --yes takes the first (sorted) device; otherwise the
// Bubble Tea picker runs interactively. Both non-picker paths let
// `machine add --discover` run in a container/CI job without a TTY.
func selectDiscoveredDevice(ctx context.Context, devices []discoveredDevice, args []string) (discoveredDevice, string, error) {
	if len(args) == 1 {
		return selectDiscoveredDeviceByID(devices, args[0])
	}
	if machineAddYes {
		id, err := deriveMachineID(devices[0])
		return devices[0], id, err
	}
	if !ui.IsTerminal() {
		return discoveredDevice{}, "", camperrors.New(
			"machine add --discover needs a terminal to pick a device; pass an id argument or --yes to run non-interactively")
	}
	dev, err := pickDevice(ctx, devices)
	if err != nil {
		return discoveredDevice{}, "", err
	}
	id, err := deriveMachineID(dev)
	return dev, id, err
}

func selectDiscoveredDeviceByID(devices []discoveredDevice, wantID string) (discoveredDevice, string, error) {
	if err := validateMachineID(wantID); err != nil {
		return discoveredDevice{}, "", err
	}
	for _, d := range devices {
		if sanitizeID(d.DNSName) == wantID || sanitizeID(d.HostName) == wantID {
			return d, wantID, nil
		}
	}
	return discoveredDevice{}, "", camperrors.New(fmt.Sprintf("no discovered device matches id %q", wantID))
}

// devicePickerModel is a minimal Bubble Tea list picker following the same
// items/selected/picked/aborted/value shape and j/k/enter/esc keybindings as
// camp's existing selector (cmd/camp/promote/selector.go:18-73), specialized to
// discoveredDevice since the promote selector is typed to workitem.WorkItem.
type devicePickerModel struct {
	items    []discoveredDevice
	selected int
	picked   bool
	aborted  bool
	value    discoveredDevice
}

func (m devicePickerModel) Init() tea.Cmd { return nil }

func (m devicePickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "ctrl+c":
		m.aborted = true
		return m, tea.Quit
	case "esc", "q":
		return m, tea.Quit
	case "enter":
		if len(m.items) > 0 {
			m.value = m.items[m.selected]
			m.picked = true
			return m, tea.Quit
		}
	case "down", "j", "tab":
		if len(m.items) > 0 {
			m.selected = (m.selected + 1) % len(m.items)
		}
	case "up", "k", "shift+tab":
		if len(m.items) > 0 {
			m.selected = (m.selected - 1 + len(m.items)) % len(m.items)
		}
	}
	return m, nil
}

func (m devicePickerModel) View() string {
	var b strings.Builder
	b.WriteString(ui.Header("Discovered tailnet devices") + "\n\n")
	for i, d := range m.items {
		marker := "  "
		if i == m.selected {
			marker = "> "
		}
		status := "offline"
		if d.Online {
			status = "online"
		}
		fmt.Fprintf(&b, "%s%-24s %-34s %s\n", marker, d.HostName, d.Host, status)
	}
	b.WriteString("\n" + ui.Dim("j/k: navigate . Enter: select . Esc: cancel"))
	return b.String()
}

func pickDevice(ctx context.Context, devices []discoveredDevice) (discoveredDevice, error) {
	final, err := tea.NewProgram(devicePickerModel{items: devices}, tea.WithContext(ctx)).Run()
	if err != nil {
		return discoveredDevice{}, camperrors.Wrap(err, "running machine picker")
	}
	dm, ok := final.(devicePickerModel)
	if !ok || dm.aborted || !dm.picked {
		return discoveredDevice{}, camperrors.New("machine discovery cancelled")
	}
	return dm.value, nil
}
