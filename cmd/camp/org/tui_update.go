package org

import (
	"context"
	"fmt"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type createCampaignDoneMsg struct {
	name string
	org  string
	err  error
}

func (m orgTUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case createCampaignDoneMsg:
		return m.handleCreateCampaignDone(msg)
	case tea.KeyMsg:
		if m.overlay != overlayNone {
			return m.updateOverlay(msg)
		}
		return m.updateBrowse(msg)
	}
	return m, nil
}

func (m orgTUIModel) updateBrowse(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.status = ""
	switch key.String() {
	case "ctrl+c", "q":
		m.quitting = true
		return m, tea.Quit
	case "esc":
		if m.pane == paneMembers {
			m.pane = paneOrgs
			return m, nil
		}
		m.quitting = true
		return m, tea.Quit
	case "down", "j":
		m.moveDown()
		return m, nil
	case "up", "k":
		m.moveUp()
		return m, nil
	case "right", "l", "enter":
		if m.pane == paneOrgs && len(m.members) > 0 {
			m.pane = paneMembers
			m.memCursor = 0
		}
		return m, nil
	case "left", "h", "backspace":
		if m.pane == paneMembers {
			m.pane = paneOrgs
		}
		return m, nil
	case "r":
		if m.pane == paneOrgs && len(m.orgs) > 0 {
			m.openOverlay(overlayRename, "new org name")
		}
		return m, nil
	case "n":
		if m.pane == paneOrgs {
			m.openOverlay(overlayCreateEmpty, "new org name")
			return m, textinput.Blink
		}
		return m, nil
	case "N":
		if m.pane == paneOrgs && len(m.orgs) > 0 {
			m.pendingOrg = m.orgs[m.orgCursor].Org
			m.openOverlay(overlayNewCampaign, "new campaign name")
			return m, textinput.Blink
		}
		return m, nil
	case "x", "D":
		if m.pane == paneOrgs && len(m.orgs) > 0 {
			return m.beginDeleteFocused()
		}
		return m, nil
	case "m":
		if m.pane == paneMembers && len(m.members) > 0 {
			m.openOverlay(overlayMove, "target org")
		}
		return m, nil
	case "c":
		if m.pane == paneMembers && len(m.members) > 0 {
			m.openOverlay(overlayCreate, "new org name")
		}
		return m, nil
	case "d":
		if m.pane == paneMembers && len(m.members) > 0 {
			member := m.members[m.memCursor]
			if err := m.assignOrg(member.ID, m.fallback); err != nil {
				m.setError(err)
			} else {
				m.setStatus(fmt.Sprintf("returned %q to org %q", member.Name, m.fallback))
			}
		}
		return m, nil
	}
	return m, nil
}

func (m orgTUIModel) beginDeleteFocused() (tea.Model, tea.Cmd) {
	row := m.orgs[m.orgCursor]
	if row.Org == m.fallback {
		m.setError(camperrors.NewValidation("org", "cannot delete the fallback org", nil))
		return m, nil
	}
	if row.Campaigns > 0 {
		m.setError(camperrors.NewValidation("org",
			fmt.Sprintf("cannot delete: %q has %d member(s)", row.Org, row.Campaigns), nil))
		return m, nil
	}
	m.pendingDelete = row.Org
	m.overlay = overlayConfirmDelete
	return m, nil
}

func (m *orgTUIModel) moveDown() {
	if m.pane == paneOrgs {
		if len(m.orgs) == 0 {
			return
		}
		m.orgCursor = (m.orgCursor + 1) % len(m.orgs)
		m.memCursor = 0
		m.syncFocusedOrg()
		return
	}
	if len(m.members) == 0 {
		return
	}
	m.memCursor = (m.memCursor + 1) % len(m.members)
}

func (m *orgTUIModel) moveUp() {
	if m.pane == paneOrgs {
		if len(m.orgs) == 0 {
			return
		}
		m.orgCursor = (m.orgCursor - 1 + len(m.orgs)) % len(m.orgs)
		m.memCursor = 0
		m.syncFocusedOrg()
		return
	}
	if len(m.members) == 0 {
		return
	}
	m.memCursor = (m.memCursor - 1 + len(m.members)) % len(m.members)
}

func (m *orgTUIModel) openOverlay(kind orgOverlay, placeholder string) {
	m.overlay = kind
	m.input.SetValue("")
	m.input.Placeholder = placeholder
	m.input.Focus()
}

func (m orgTUIModel) updateOverlay(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.overlay == overlayConfirmDelete {
		return m.updateConfirmDelete(key)
	}
	switch key.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "esc":
		m.closeOverlay()
		return m, nil
	case "enter":
		return m.commitOverlay()
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(key)
	return m, cmd
}

func (m orgTUIModel) updateConfirmDelete(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "esc", "n":
		m.pendingDelete = ""
		m.closeOverlay()
		return m, nil
	case "y", "enter":
		name := m.pendingDelete
		m.pendingDelete = ""
		m.closeOverlay()
		if err := m.deleteEmptyOrg(name); err != nil {
			m.setError(err)
		} else {
			m.setStatus(fmt.Sprintf("deleted org %q", name))
		}
		return m, nil
	}
	return m, nil
}

func (m *orgTUIModel) closeOverlay() {
	m.overlay = overlayNone
	m.input.Blur()
}

func (m orgTUIModel) commitOverlay() (tea.Model, tea.Cmd) {
	value := strings.TrimSpace(m.input.Value())
	switch m.overlay {
	case overlayRename:
		old := m.orgs[m.orgCursor].Org
		if err := m.renameOrg(old, value); err != nil {
			m.setError(err)
		} else {
			m.setStatus(fmt.Sprintf("renamed org %q to %q", old, value))
		}
	case overlayMove:
		member := m.members[m.memCursor]
		if err := m.assignOrg(member.ID, value); err != nil {
			m.setError(err)
		} else {
			m.setStatus(fmt.Sprintf("moved %q to org %q", member.Name, value))
		}
	case overlayCreate:
		member := m.members[m.memCursor]
		existed := m.orgExists(value)
		if err := m.assignOrg(member.ID, value); err != nil {
			m.setError(err)
		} else if existed {
			m.setStatus(fmt.Sprintf("added %q to existing org %q", member.Name, value))
		} else {
			m.setStatus(fmt.Sprintf("created org %q with %q", value, member.Name))
		}
	case overlayCreateEmpty:
		if value == "" {
			m.setError(camperrors.NewValidation("org", "org name is required", nil))
			return m, nil // keep overlay open
		}
		if err := validateOrgName(value); err != nil {
			m.setError(err)
			return m, nil // keep overlay open
		}
		existed := m.orgExists(value)
		if err := m.createEmptyOrg(value); err != nil {
			m.setError(err)
			return m, nil
		}
		if existed {
			m.setStatus(fmt.Sprintf("org %q already exists", value))
		} else {
			m.setStatus(fmt.Sprintf("created org %q", value))
		}
		// focus the new org if present
		for i, o := range m.orgs {
			if o.Org == value {
				m.orgCursor = i
				m.syncFocusedOrg()
				break
			}
		}
	case overlayNewCampaign:
		if value == "" {
			m.setError(camperrors.NewValidation("campaign", "campaign name is required", nil))
			return m, nil
		}
		org := m.pendingOrg
		m.closeOverlay()
		m.setStatus(fmt.Sprintf("creating %q in %q...", value, org))
		return m, createCampaignCmd(m.ctx, value, org)
	}
	m.closeOverlay()
	return m, nil
}

func (m orgTUIModel) handleCreateCampaignDone(msg createCampaignDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.setError(msg.err)
		return m, nil
	}
	if err := m.reload(); err != nil {
		m.setError(err)
		return m, nil
	}
	m.setStatus(fmt.Sprintf("created %q in %q", msg.name, msg.org))
	// Focus the org the campaign was created into.
	for i, o := range m.orgs {
		if o.Org == msg.org {
			m.orgCursor = i
			m.syncFocusedOrg()
			break
		}
	}
	return m, nil
}

func createCampaignCmd(ctx context.Context, name, org string) tea.Cmd {
	return func() tea.Msg {
		return createCampaignDoneMsg{name: name, org: org, err: createCampaignInOrg(ctx, name, org)}
	}
}
