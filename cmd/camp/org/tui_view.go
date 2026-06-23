package org

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	tuistyles "github.com/Obedience-Corp/camp/internal/intent/tui"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
)

var orgPal = theme.TUI()

var (
	orgTitleStyle  = tuistyles.TitleStyle
	orgHelpStyle   = tuistyles.HelpStyle
	orgErrStyle    = tuistyles.ErrorStyle
	orgOkStyle     = tuistyles.SuccessStyle
	orgPaneFocused = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(orgPal.BorderFocus).Padding(0, 1)
	orgPaneBlurred = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(orgPal.Border).Padding(0, 1)
	orgSelStyle    = lipgloss.NewStyle().Foreground(orgPal.Accent).Bold(true)
	orgRowStyle    = lipgloss.NewStyle().Foreground(orgPal.TextPrimary)
	orgMutedStyle  = lipgloss.NewStyle().Foreground(orgPal.TextMuted)
	orgHereStyle   = lipgloss.NewStyle().Foreground(orgPal.Success)
)

func (m orgTUIModel) View() string {
	if m.quitting {
		return ""
	}
	if len(m.orgs) == 0 {
		return orgTitleStyle.Render("Orgs") + "\n\n" +
			orgMutedStyle.Render("No campaigns registered yet. Run camp init or camp register.") + "\n\n" +
			orgHelpStyle.Render("q: quit") + "\n"
	}
	if m.overlay != overlayNone {
		return m.overlayView()
	}

	var body string
	if m.width > 0 && m.width < orgTUIMinWide {
		if m.pane == paneMembers {
			body = m.renderMemberPane()
		} else {
			body = m.renderOrgPane()
		}
	} else {
		body = lipgloss.JoinHorizontal(lipgloss.Top, m.renderOrgPane(), "  ", m.renderMemberPane())
	}

	return body + "\n" + m.statusLine() + m.footer() + "\n"
}

func (m orgTUIModel) renderOrgPane() string {
	var b strings.Builder
	b.WriteString(orgTitleStyle.Render("Orgs") + "\n")
	for i, o := range m.orgs {
		cursor := "  "
		label := fmt.Sprintf("%-16s %d (%d active)", o.Org, o.Campaigns, o.Active)
		switch {
		case i == m.orgCursor && m.pane == paneOrgs:
			cursor = "> "
			label = orgSelStyle.Render(label)
		case i == m.orgCursor:
			cursor = "> "
			label = orgRowStyle.Render(label)
		default:
			label = orgMutedStyle.Render(label)
		}
		b.WriteString(cursor + label + "\n")
	}
	return m.paneStyle(paneOrgs).Render(strings.TrimRight(b.String(), "\n"))
}

func (m orgTUIModel) renderMemberPane() string {
	var b strings.Builder
	title := "Members"
	if m.focusedOrg != "" {
		title = fmt.Sprintf("Members of %q", m.focusedOrg)
	}
	b.WriteString(orgTitleStyle.Render(title) + "\n")
	if len(m.members) == 0 {
		b.WriteString(orgMutedStyle.Render("no campaigns in this org") + "\n")
	}
	for i, mem := range m.members {
		cursor := "  "
		here := "  "
		if mem.ID == m.currentID && m.currentID != "" {
			here = orgHereStyle.Render("* ")
		}
		label := fmt.Sprintf("%-24s %s", mem.Name, mem.Status)
		switch {
		case i == m.memCursor && m.pane == paneMembers:
			cursor = "> "
			label = orgSelStyle.Render(label)
		case i == m.memCursor:
			cursor = "> "
			label = orgRowStyle.Render(label)
		default:
			label = orgMutedStyle.Render(label)
		}
		b.WriteString(cursor + here + label + "\n")
	}
	return m.paneStyle(paneMembers).Render(strings.TrimRight(b.String(), "\n"))
}

func (m orgTUIModel) paneStyle(p orgPane) lipgloss.Style {
	if m.pane == p {
		return orgPaneFocused
	}
	return orgPaneBlurred
}

func (m orgTUIModel) footer() string {
	if m.pane == paneOrgs {
		return orgHelpStyle.Render("j/k: orgs . l: members . r: rename . q: quit")
	}
	return orgHelpStyle.Render("j/k: members . h: orgs . m: move . d: default . esc: back . q: quit")
}

func (m orgTUIModel) statusLine() string {
	if m.status == "" {
		return ""
	}
	if m.statusErr {
		return orgErrStyle.Render(m.status) + "\n"
	}
	return orgOkStyle.Render(m.status) + "\n"
}

func (m orgTUIModel) overlayView() string {
	var prompt string
	switch m.overlay {
	case overlayRename:
		prompt = fmt.Sprintf("Rename org %q to:", m.orgs[m.orgCursor].Org)
	case overlayMove:
		prompt = fmt.Sprintf("Move %q to org:", m.members[m.memCursor].Name)
	}
	box := orgTitleStyle.Render(prompt) + "\n\n" +
		m.input.View() + "\n\n" +
		orgMutedStyle.Render("existing orgs: "+m.orgNamesCSV()) + "\n\n" +
		orgHelpStyle.Render("enter: confirm . esc: cancel")
	return orgPaneFocused.Render(box) + "\n"
}
