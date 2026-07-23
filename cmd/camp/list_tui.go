package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/machines"
	"github.com/Obedience-Corp/camp/internal/pathutil"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

// Overridden in tests so dispatch does not depend on the runner's terminal.
var stdoutIsTTY = func() bool { return term.IsTerminal(int(os.Stdout.Fd())) }

type listOverlay int

const (
	listOverlayNone listOverlay = iota
	listOverlayMove
)

type listTUIModel struct {
	ctx context.Context

	fallback   string
	orgFilter  string
	all        []campaignEntry
	visible    []campaignEntry
	cursor     int
	activeOnly bool

	// Remote toggle (R1): default local-only; key r loads/strips remote rows.
	remoteOn           bool
	remoteLoading      bool
	machinesConfigured bool
	localCount         int // length of m.all before remote rows were appended

	overlay listOverlay
	input   textinput.Model

	status    string
	statusErr bool

	gotoEnabled bool
	gotoPath    string

	width    int
	height   int
	quitting bool
}

// remoteLoadedMsg is delivered when the async fan-out for key r completes.
type remoteLoadedMsg struct {
	rows    []campaignEntry
	results []remoteResult
	err     error
}

// listTUIRequested decides whether bare `camp list` opens the browser. Machine or
// text output (--json, --count, non-table --format) never does; -i forces;
// otherwise an interactive terminal with no shaping flag opens it.
func listTUIRequested(cmd *cobra.Command, isTTY bool) bool {
	if listJSON || listCount {
		return false
	}
	if format, _ := cmd.Flags().GetString("format"); format != "table" {
		return false
	}
	if interactive, _ := cmd.Flags().GetBool("interactive"); interactive {
		return true
	}
	for _, f := range []string{"sort", "org", "tag", "status", "all", "group", "no-group", "remote"} {
		if cmd.Flags().Changed(f) {
			return false
		}
	}
	return isTTY
}

func runListTUI(cmd *cobra.Command, positionalOrg string) error {
	ctx := cmd.Context()
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return renderListTable(cmd, positionalOrg)
	}
	pathOutput, _ := cmd.Flags().GetString("path-output")
	reg, report, err := loadVerifiedListRegistry(ctx)
	if err != nil {
		return camperrors.Wrap(err, "failed to load registry")
	}
	orgFilter, _ := cmd.Flags().GetString("org")
	if err := requireListOrg(reg, orgFilter); err != nil {
		return err
	}
	model := newListTUIModel(ctx, reg, orgFilter)
	model.gotoEnabled = pathOutput != ""
	if report.HasChanges() {
		model.setStatus("registry cleaned: "+verificationSummaryText(report), false)
	}
	prog := tea.NewProgram(model, tea.WithContext(ctx), tea.WithAltScreen())
	final, err := prog.Run()
	if err != nil {
		return camperrors.Wrap(err, "running campaign browser")
	}
	return writeGotoSelection(final, pathOutput)
}

// writeGotoSelection persists the campaign the user chose to go to, for the
// shell function to cd into (local absolute path) or hop (ssh-hop: marker).
// No-op unless a path-output file was supplied and a selection was made.
func writeGotoSelection(final tea.Model, pathOutput string) error {
	m, ok := final.(listTUIModel)
	if !ok || pathOutput == "" || m.gotoPath == "" {
		return nil
	}
	return os.WriteFile(pathOutput, []byte(m.gotoPath), 0o600)
}

// gotoSelectionFor returns the path-output payload for a list row: absolute path
// for local campaigns, ssh-hop:<machine>:<name> for remote rows.
func gotoSelectionFor(e campaignEntry) string {
	if e.Machine != "" && e.Machine != machines.LocalMachineID {
		return "ssh-hop:" + e.Machine + ":" + e.Name
	}
	return e.Path
}

// isRemoteListEntry reports whether a row came from a remote machine fan-out.
func isRemoteListEntry(e campaignEntry) bool {
	return e.Machine != "" && e.Machine != machines.LocalMachineID
}

func newListTUIModel(ctx context.Context, reg *config.Registry, orgFilter string) listTUIModel {
	ti := textinput.New()
	ti.Prompt = "> "
	m := listTUIModel{ctx: ctx, input: ti, orgFilter: orgFilter}
	if mf, err := machines.Load(); err == nil && len(mf.Machines) > 0 {
		m.machinesConfigured = true
	}
	m.loadFromRegistry(reg)
	return m
}

func (m *listTUIModel) loadFromRegistry(reg *config.Registry) {
	m.fallback = reg.FallbackOrg()
	// Preserve remote rows across registry reload after local mutate.
	var remotes []campaignEntry
	if m.remoteOn {
		for _, e := range m.all {
			if isRemoteListEntry(e) {
				remotes = append(remotes, e)
			}
		}
	}
	m.all = sortCampaigns(reg.Campaigns, "org", m.fallback)
	m.localCount = len(m.all)
	if m.remoteOn {
		m.all = append(m.all, remotes...)
	}
	m.rebuildVisible()
}

func (m *listTUIModel) rebuildVisible() {
	out := make([]campaignEntry, 0, len(m.all))
	for _, e := range m.all {
		if m.orgFilter != "" && e.Org != m.orgFilter {
			continue
		}
		if m.activeOnly && e.Status != config.StatusActive {
			continue
		}
		out = append(out, e)
	}
	m.visible = out
	m.cursor = ui.ClampIdx(m.cursor, len(m.visible))
}

func (m *listTUIModel) reload() error {
	selID := ""
	if m.cursor < len(m.visible) {
		selID = m.visible[m.cursor].ID
	}
	reg, err := config.LoadRegistry(m.ctx)
	if err != nil {
		return camperrors.Wrap(err, "failed to reload registry")
	}
	m.loadFromRegistry(reg)
	if selID != "" {
		for i, e := range m.visible {
			if e.ID == selID {
				m.cursor = i
				break
			}
		}
	}
	return nil
}

func (m listTUIModel) Init() tea.Cmd { return textinput.Blink }

func (m *listTUIModel) cycleStatus() error {
	e := m.visible[m.cursor]
	next := nextLifecycleStatus(e.Status)
	if err := config.ValidateStatus(next); err != nil {
		return err
	}
	if err := m.mutate(e.ID, func(c *config.RegisteredCampaign) { c.Status = next }); err != nil {
		return err
	}
	m.setStatus(fmt.Sprintf("set %q %s", e.Name, next), false)
	return nil
}

func (m *listTUIModel) assignOrg(id, name, org string) error {
	if err := config.ValidateName("org", org); err != nil {
		return err
	}
	if err := m.mutate(id, func(c *config.RegisteredCampaign) { c.Org = org }); err != nil {
		return err
	}
	m.setStatus(fmt.Sprintf("moved %q to org %q", name, org), false)
	return nil
}

func (m *listTUIModel) mutate(id string, apply func(*config.RegisteredCampaign)) error {
	err := config.UpdateRegistry(m.ctx, func(reg *config.Registry) error {
		c, ok := reg.Campaigns[id]
		if !ok {
			return camperrors.NewNotFound("campaign", id, nil)
		}
		apply(&c)
		reg.Campaigns[id] = c
		return nil
	})
	if err != nil {
		return err
	}
	return m.reload()
}

func (m *listTUIModel) copyPath() error {
	return ui.WriteClipboard(pathutil.AbbreviateHome(m.visible[m.cursor].Path))
}

func (m *listTUIModel) setStatus(s string, isErr bool) {
	m.status = s
	m.statusErr = isErr
}

func (m *listTUIModel) setError(err error) {
	m.status = err.Error()
	m.statusErr = true
}

func nextLifecycleStatus(s string) string {
	switch s {
	case config.StatusActive:
		return config.StatusInactive
	case config.StatusInactive:
		return config.StatusReference
	default:
		return config.StatusActive
	}
}

func (m listTUIModel) orgNamesCSV() string {
	seen := map[string]bool{}
	var names []string
	for _, e := range m.all {
		if !seen[e.Org] {
			seen[e.Org] = true
			names = append(names, e.Org)
		}
	}
	return strings.Join(names, ", ")
}
