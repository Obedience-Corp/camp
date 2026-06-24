package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Obedience-Corp/camp/internal/config"
	tuistyles "github.com/Obedience-Corp/camp/internal/intent/tui"
	"github.com/Obedience-Corp/camp/internal/pathutil"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
)

var listPal = theme.TUI()

var (
	listTitleStyle = tuistyles.TitleStyle
	listHelpStyle  = tuistyles.HelpStyle
	listErrStyle   = tuistyles.ErrorStyle
	listOkStyle    = tuistyles.SuccessStyle
	listOrgHeader  = lipgloss.NewStyle().Foreground(listPal.AccentAlt).Bold(true)
	listSelStyle   = lipgloss.NewStyle().Foreground(listPal.Accent).Bold(true)
	listNameStyle  = lipgloss.NewStyle().Foreground(listPal.TextPrimary)
	listMutedStyle = lipgloss.NewStyle().Foreground(listPal.TextMuted)
	listBadgeOn    = lipgloss.NewStyle().Foreground(listPal.Success)
	listBadgeOff   = lipgloss.NewStyle().Foreground(listPal.Warning)
	listBadgeRef   = lipgloss.NewStyle().Foreground(listPal.AccentAlt)
	listBox        = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(listPal.BorderFocus).Padding(0, 1)
)

func listStatusCell(status string) string {
	padded := fmt.Sprintf("%-10s", status)
	switch status {
	case config.StatusActive:
		return listBadgeOn.Render(padded)
	case config.StatusInactive:
		return listBadgeOff.Render(padded)
	case config.StatusReference:
		return listBadgeRef.Render(padded)
	default:
		return listMutedStyle.Render(padded)
	}
}

func (m listTUIModel) View() string {
	if m.quitting {
		return ""
	}
	if m.overlay != listOverlayNone {
		return m.overlayView()
	}

	var b strings.Builder
	b.WriteString(m.topBar() + "\n\n")

	if len(m.visible) == 0 {
		b.WriteString(listMutedStyle.Render("no campaigns to show") + "\n")
	}

	start, end := m.windowBounds()
	prevOrg := ""
	for i := start; i < end; i++ {
		e := m.visible[i]
		if e.Org != prevOrg {
			b.WriteString(listOrgHeader.Render(e.Org) + "\n")
			prevOrg = e.Org
		}
		cursor := "  "
		name := fmt.Sprintf("%-22s", e.Name)
		if i == m.cursor {
			cursor = "> "
			name = listSelStyle.Render(name)
		} else {
			name = listNameStyle.Render(name)
		}
		path := listMutedStyle.Render(pathutil.AbbreviateHome(e.Path))
		b.WriteString("  " + cursor + name + "  " + listStatusCell(e.Status) + "  " + path + "\n")
	}
	if end < len(m.visible) || start > 0 {
		b.WriteString(listMutedStyle.Render(fmt.Sprintf("  [%d-%d of %d]", start+1, end, len(m.visible))) + "\n")
	}

	b.WriteString("\n" + m.statusLine() + m.footer())
	return listBox.Render(strings.TrimRight(b.String(), "\n")) + "\n"
}

// windowBounds returns the slice of m.visible to render, keeping the cursor in
// view. When the terminal height is unknown (tests) it shows everything.
func (m listTUIModel) windowBounds() (int, int) {
	total := len(m.visible)
	capacity := m.height - 7
	if m.height <= 0 || capacity >= total {
		return 0, total
	}
	if capacity < 3 {
		capacity = 3
	}
	start := m.cursor - capacity/2
	if start < 0 {
		start = 0
	}
	end := start + capacity
	if end > total {
		end = total
		start = end - capacity
	}
	if start < 0 {
		start = 0
	}
	return start, end
}

func (m listTUIModel) topBar() string {
	mode := "all"
	if m.activeOnly {
		mode = "active only"
	}
	return listTitleStyle.Render("Campaigns") + "  " +
		listMutedStyle.Render(fmt.Sprintf("%s  .  showing: %s", ui.CountLabel(len(m.all), "campaign", "campaigns"), mode))
}

func (m listTUIModel) footer() string {
	return listHelpStyle.Render("g: go . j/k: move . s: status . m: org . y: copy . f: filter . q: quit")
}

func (m listTUIModel) statusLine() string {
	if m.status == "" {
		return ""
	}
	if m.statusErr {
		return listErrStyle.Render(m.status) + "\n"
	}
	return listOkStyle.Render(m.status) + "\n"
}

func (m listTUIModel) overlayView() string {
	e := m.visible[m.cursor]
	box := listTitleStyle.Render(fmt.Sprintf("Move %q to org:", e.Name)) + "\n\n" +
		m.input.View() + "\n\n" +
		listMutedStyle.Render("existing orgs: "+m.orgNamesCSV()) + "\n\n" +
		listHelpStyle.Render("enter: confirm . esc: cancel")
	return listBox.Render(box) + "\n"
}
