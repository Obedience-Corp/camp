package org

import (
	"context"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/spf13/cobra"
)

// orgTUIMinWide is the terminal width below which the two panes stack into a
// single focused pane instead of rendering side by side.
const orgTUIMinWide = 72

type orgPane int

const (
	paneOrgs orgPane = iota
	paneMembers
)

type orgOverlay int

const (
	overlayNone orgOverlay = iota
	overlayMove
	overlayRename
	overlayCreate
)

type orgTUIModel struct {
	ctx context.Context

	reg       *config.Registry
	fallback  string
	currentID string

	orgs       []orgCount
	members    []orgMember
	focusedOrg string

	pane      orgPane
	orgCursor int
	memCursor int

	overlay orgOverlay
	input   textinput.Model

	status    string
	statusErr bool

	width    int
	height   int
	quitting bool
}

func newOrgTUIModel(ctx context.Context, reg *config.Registry) orgTUIModel {
	ti := textinput.New()
	ti.Prompt = "> "

	m := orgTUIModel{ctx: ctx, input: ti}
	if id, err := currentCampaignID(ctx); err == nil {
		m.currentID = id
	}
	m.loadFromRegistry(reg)
	return m
}

func (m *orgTUIModel) loadFromRegistry(reg *config.Registry) {
	m.reg = reg
	m.fallback = reg.FallbackOrg()
	m.orgs = computeOrgCounts(reg)
	m.orgCursor = clamp(m.orgCursor, 0, lastIndex(len(m.orgs)))
	m.syncFocusedOrg()
}

func (m *orgTUIModel) syncFocusedOrg() {
	if len(m.orgs) == 0 {
		m.focusedOrg = ""
		m.members = nil
		m.memCursor = 0
		return
	}
	m.focusedOrg = m.orgs[m.orgCursor].Org
	m.members = buildOrgShow(m.reg, m.focusedOrg).Members
	m.memCursor = clamp(m.memCursor, 0, lastIndex(len(m.members)))
}

func (m *orgTUIModel) reload() error {
	reg, err := config.LoadRegistry(m.ctx)
	if err != nil {
		return camperrors.Wrap(err, "failed to reload registry")
	}
	m.loadFromRegistry(reg)
	return nil
}

func (m orgTUIModel) Init() tea.Cmd { return textinput.Blink }

// runOrgTUI launches the interactive org browser. When stdout is not a terminal
// it degrades to the flat `camp org list` output rather than erroring.
func runOrgTUI(cmd *cobra.Command) error {
	ctx := cmd.Context()
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return runOrgList(cmd, nil)
	}
	reg, err := config.LoadRegistry(ctx)
	if err != nil {
		return camperrors.Wrap(err, "failed to load registry")
	}
	prog := tea.NewProgram(newOrgTUIModel(ctx, reg), tea.WithContext(ctx), tea.WithAltScreen())
	if _, err := prog.Run(); err != nil {
		return camperrors.Wrap(err, "running org browser")
	}
	return nil
}

func (m *orgTUIModel) setStatus(s string) {
	m.status = s
	m.statusErr = false
}

func (m *orgTUIModel) setError(err error) {
	m.status = err.Error()
	m.statusErr = true
}

// assignOrg moves a single campaign into targetOrg through the shared atomic
// registry write path, then reloads the model from disk.
func (m *orgTUIModel) assignOrg(campaignID, targetOrg string) error {
	if err := validateOrgName(targetOrg); err != nil {
		return err
	}
	err := config.UpdateRegistry(m.ctx, func(reg *config.Registry) error {
		entry, ok := reg.Campaigns[campaignID]
		if !ok {
			return camperrors.NewNotFound("campaign", campaignID, nil)
		}
		entry.Org = targetOrg
		reg.Campaigns[campaignID] = entry
		return nil
	})
	if err != nil {
		return err
	}
	return m.reload()
}

func (m *orgTUIModel) renameOrg(oldOrg, newOrg string) error {
	if err := validateOrgName(newOrg); err != nil {
		return err
	}
	err := config.UpdateRegistry(m.ctx, func(reg *config.Registry) error {
		_, rerr := renameOrgInRegistry(reg, oldOrg, newOrg)
		return rerr
	})
	if err != nil {
		return err
	}
	return m.reload()
}

func (m orgTUIModel) orgExists(name string) bool {
	for _, o := range m.orgs {
		if o.Org == name {
			return true
		}
	}
	return false
}

func (m orgTUIModel) orgNamesCSV() string {
	names := make([]string, 0, len(m.orgs))
	for _, o := range m.orgs {
		names = append(names, o.Org)
	}
	return strings.Join(names, ", ")
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func lastIndex(n int) int {
	if n <= 0 {
		return 0
	}
	return n - 1
}
