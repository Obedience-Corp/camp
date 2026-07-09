package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
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
	all        []campaignEntry
	visible    []campaignEntry
	cursor     int
	activeOnly bool

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
	for _, f := range []string{"sort", "org", "tag", "status", "all", "group", "no-group"} {
		if cmd.Flags().Changed(f) {
			return false
		}
	}
	return isTTY
}

func runListTUI(cmd *cobra.Command) error {
	ctx := cmd.Context()
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return renderListTable(cmd)
	}
	pathOutput, _ := cmd.Flags().GetString("path-output")
	reg, report, err := loadVerifiedListRegistry(ctx)
	if err != nil {
		return camperrors.Wrap(err, "failed to load registry")
	}
	model := newListTUIModel(ctx, reg)
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
// shell function to cd into. No-op unless a path-output file was supplied and a
// selection was made.
func writeGotoSelection(final tea.Model, pathOutput string) error {
	m, ok := final.(listTUIModel)
	if !ok || pathOutput == "" || m.gotoPath == "" {
		return nil
	}
	return os.WriteFile(pathOutput, []byte(m.gotoPath), 0o600)
}

func newListTUIModel(ctx context.Context, reg *config.Registry) listTUIModel {
	ti := textinput.New()
	ti.Prompt = "> "
	m := listTUIModel{ctx: ctx, input: ti}
	m.loadFromRegistry(reg)
	return m
}

func (m *listTUIModel) loadFromRegistry(reg *config.Registry) {
	m.fallback = reg.FallbackOrg()
	m.all = sortCampaigns(reg.Campaigns, "org", m.fallback)
	m.rebuildVisible()
}

func (m *listTUIModel) rebuildVisible() {
	if !m.activeOnly {
		m.visible = m.all
	} else {
		out := make([]campaignEntry, 0, len(m.all))
		for _, e := range m.all {
			if e.Status == config.StatusActive {
				out = append(out, e)
			}
		}
		m.visible = out
	}
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

// Package var so tests do not touch the real clipboard.
var writeClipboard = func(s string) error {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.Command("pbcopy")
	default:
		c = exec.Command("xclip", "-selection", "clipboard")
	}
	c.Stdin = strings.NewReader(s)
	return c.Run()
}

func (m *listTUIModel) copyPath() error {
	return writeClipboard(pathutil.AbbreviateHome(m.visible[m.cursor].Path))
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
