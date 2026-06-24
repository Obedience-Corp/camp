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
	"github.com/spf13/cobra"
)

// stdoutIsTTY reports whether stdout is an interactive terminal. A package var so
// the dispatch is deterministic under test.
var stdoutIsTTY = func() bool { return term.IsTerminal(int(os.Stdout.Fd())) }

type listOverlay int

const (
	listOverlayNone listOverlay = iota
	listOverlayMove
)

type listTUIModel struct {
	ctx context.Context

	fallback   string
	all        []campaignEntry // every campaign, org-major sorted
	visible    []campaignEntry // after the activeOnly filter
	cursor     int
	activeOnly bool

	overlay listOverlay
	input   textinput.Model

	status    string
	statusErr bool

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

// runListTUI launches the browser, degrading to the table when stdout is not a
// terminal (bubbletea needs a TTY).
func runListTUI(cmd *cobra.Command) error {
	ctx := cmd.Context()
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return renderListTable(cmd)
	}
	reg, err := config.LoadRegistry(ctx)
	if err != nil {
		return camperrors.Wrap(err, "failed to load registry")
	}
	prog := tea.NewProgram(newListTUIModel(ctx, reg), tea.WithContext(ctx), tea.WithAltScreen())
	if _, err := prog.Run(); err != nil {
		return camperrors.Wrap(err, "running campaign browser")
	}
	return nil
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
	m.cursor = clampIdx(m.cursor, len(m.visible))
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

// cycleStatus advances the selected campaign active -> inactive -> reference ->
// active through the shared atomic registry write path.
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

// writeClipboard copies s to the system clipboard. A package var so tests can
// intercept it instead of touching the real clipboard.
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

func clampIdx(v, n int) int {
	if n <= 0 {
		return 0
	}
	if v < 0 {
		return 0
	}
	if v > n-1 {
		return n - 1
	}
	return v
}
