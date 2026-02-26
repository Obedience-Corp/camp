// Package tui provides terminal UI components for intent management.
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/Obedience-Corp/camp/internal/intent"
)

// GatherDialog is a modal dialog for configuring a gather operation.
// It allows the user to specify a title, review selected intents,
// and choose whether to archive source intents.
type GatherDialog struct {
	titleInput     textinput.Model
	intents        []*intent.Intent
	archiveSources bool
	focusedField   int // 0 = title, 1 = archive checkbox, 2 = buttons
	done           bool
	cancelled      bool
	width          int
	scrollOffset   int // Scroll offset for intent list
	maxVisible     int // Max intents visible at once
}

// gatherFinishedMsg is sent when a gather operation completes.
type gatherFinishedMsg struct {
	gatheredID    string
	gatheredTitle string
	sourceCount   int
	err           error
}

// NewGatherDialog creates a new gather dialog for the given intents.
// It auto-suggests a title based on the intent titles.
func NewGatherDialog(intents []*intent.Intent) GatherDialog {
	ti := textinput.New()
	ti.Placeholder = "Enter title for gathered intent..."
	ti.CharLimit = 100
	ti.Width = 50
	ti.Focus()

	// Auto-suggest title from first intent or common prefix
	if len(intents) > 0 {
		suggested := suggestGatherTitle(intents)
		ti.SetValue(suggested)
	}

	return GatherDialog{
		titleInput:     ti,
		intents:        intents,
		archiveSources: true, // Default to archiving sources
		focusedField:   0,
		width:          60,
		maxVisible:     15,
	}
}

// suggestGatherTitle suggests a title based on the selected intents.
func suggestGatherTitle(intents []*intent.Intent) string {
	if len(intents) == 0 {
		return ""
	}

	if len(intents) == 1 {
		return intents[0].Title
	}

	// Try to find common prefix in titles
	titles := make([]string, len(intents))
	for i, intent := range intents {
		titles[i] = intent.Title
	}

	prefix := commonPrefix(titles)
	if len(prefix) > 5 {
		return strings.TrimSpace(prefix) + "..."
	}

	// Fall back to first intent title with indication of multiple
	return intents[0].Title + " (and more)"
}

// commonPrefix finds the common prefix of a slice of strings.
func commonPrefix(strs []string) string {
	if len(strs) == 0 {
		return ""
	}
	if len(strs) == 1 {
		return strs[0]
	}

	prefix := strs[0]
	for _, s := range strs[1:] {
		for len(prefix) > 0 && !strings.HasPrefix(s, prefix) {
			prefix = prefix[:len(prefix)-1]
		}
	}
	return prefix
}

// Init implements tea.Model.
func (d GatherDialog) Init() tea.Cmd {
	return textinput.Blink
}

// Update handles keyboard input for the gather dialog.
func (d GatherDialog) Update(msg tea.Msg) (GatherDialog, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			d.done = true
			d.cancelled = true
			return d, nil

		case "tab", "shift+tab":
			// Cycle through fields: title -> archive -> buttons
			if msg.String() == "shift+tab" {
				d.focusedField = (d.focusedField + 2) % 3
			} else {
				d.focusedField = (d.focusedField + 1) % 3
			}
			if d.focusedField == 0 {
				d.titleInput.Focus()
			} else {
				d.titleInput.Blur()
			}
			return d, nil

		case "enter":
			if d.focusedField == 0 {
				// In title field, move to next field
				d.focusedField = 1
				d.titleInput.Blur()
				return d, nil
			}
			// Confirm gather
			if d.titleInput.Value() != "" {
				d.done = true
				d.cancelled = false
			}
			return d, nil

		case " ":
			// Toggle archive checkbox when focused
			if d.focusedField == 1 {
				d.archiveSources = !d.archiveSources
				return d, nil
			}

		case "ctrl+d", "ctrl+f":
			// Scroll intent list down
			if d.scrollOffset+d.maxVisible < len(d.intents) {
				d.scrollOffset += 5
				maxOffset := len(d.intents) - d.maxVisible
				if maxOffset < 0 {
					maxOffset = 0
				}
				if d.scrollOffset > maxOffset {
					d.scrollOffset = maxOffset
				}
			}
			return d, nil

		case "ctrl+u", "ctrl+b":
			// Scroll intent list up
			d.scrollOffset -= 5
			if d.scrollOffset < 0 {
				d.scrollOffset = 0
			}
			return d, nil
		}
	}

	// Update text input if focused
	if d.focusedField == 0 {
		d.titleInput, cmd = d.titleInput.Update(msg)
	}

	return d, cmd
}

// Done returns true if the dialog is finished.
func (d GatherDialog) Done() bool {
	return d.done
}

// Cancelled returns true if the user cancelled the dialog.
func (d GatherDialog) Cancelled() bool {
	return d.cancelled
}

// Title returns the entered title.
func (d GatherDialog) Title() string {
	return d.titleInput.Value()
}

// ArchiveSources returns whether to archive source intents.
func (d GatherDialog) ArchiveSources() bool {
	return d.archiveSources
}

// Intents returns the list of intents to gather.
func (d GatherDialog) Intents() []*intent.Intent {
	return d.intents
}

// IntentIDs returns the IDs of the intents to gather.
func (d GatherDialog) IntentIDs() []string {
	ids := make([]string, len(d.intents))
	for i, intent := range d.intents {
		ids[i] = intent.ID
	}
	return ids
}

// View renders the gather dialog.
func (d GatherDialog) View() string {
	var b strings.Builder

	// Title with count
	b.WriteString(gatherDialogTitleStyle.Render(fmt.Sprintf("Gather %d Intents", len(d.intents))))
	b.WriteString("\n\n")

	// Selected intents list — show all with scroll window
	b.WriteString(gatherDialogLabelStyle.Render(fmt.Sprintf("Selected intents (%d):", len(d.intents))))
	b.WriteString("\n")

	end := d.scrollOffset + d.maxVisible
	if end > len(d.intents) {
		end = len(d.intents)
	}

	// Show scroll-up indicator
	if d.scrollOffset > 0 {
		b.WriteString(gatherDialogMutedStyle.Render(fmt.Sprintf("  ▲ %d more above", d.scrollOffset)))
		b.WriteString("\n")
	}

	for i := d.scrollOffset; i < end; i++ {
		bullet := "  • "
		title := d.intents[i].Title
		if len(title) > 50 {
			title = title[:47] + "..."
		}
		status := d.intents[i].Status.String()
		line := fmt.Sprintf("%s%s [%s]", bullet, title, status)
		b.WriteString(gatherDialogItemStyle.Render(line))
		b.WriteString("\n")
	}

	// Show scroll-down indicator
	if end < len(d.intents) {
		b.WriteString(gatherDialogMutedStyle.Render(fmt.Sprintf("  ▼ %d more below", len(d.intents)-end)))
		b.WriteString("\n")
	}

	b.WriteString("\n")

	// Title input
	b.WriteString(gatherDialogLabelStyle.Render("Title for gathered intent:"))
	b.WriteString("\n")
	b.WriteString(d.titleInput.View())
	b.WriteString("\n\n")

	// Archive checkbox
	checkboxLabel := "Archive source intents after gather"
	checkbox := "[ ]"
	if d.archiveSources {
		checkbox = "[×]"
	}
	checkboxStyle := gatherDialogItemStyle
	if d.focusedField == 1 {
		checkboxStyle = gatherDialogSelectedStyle
	}
	b.WriteString(checkboxStyle.Render(checkbox + " " + checkboxLabel))
	b.WriteString("\n\n")

	// Buttons
	confirmStyle := gatherDialogButtonStyle
	if d.focusedField == 2 {
		confirmStyle = gatherDialogButtonActiveStyle
	}
	cancelStyle := gatherDialogButtonStyle

	b.WriteString(confirmStyle.Render(" Gather "))
	b.WriteString("  ")
	b.WriteString(cancelStyle.Render(" Cancel (Esc) "))
	b.WriteString("\n\n")

	// Help text
	b.WriteString(gatherDialogMutedStyle.Render("Tab: next field • Space: toggle checkbox • Enter: confirm • Esc: cancel"))

	return gatherDialogBoxStyle.
		Width(d.width).
		Render(b.String())
}

// Styles for the gather dialog.
var (
	gatherDialogBoxStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(pal.BorderFocus).
				Padding(1, 2)

	gatherDialogTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(pal.Accent)

	gatherDialogLabelStyle = lipgloss.NewStyle().
				Foreground(pal.TextPrimary)

	gatherDialogItemStyle = lipgloss.NewStyle().
				Foreground(pal.TextSecondary)

	gatherDialogSelectedStyle = lipgloss.NewStyle().
					Foreground(pal.Accent).
					Bold(true)

	gatherDialogMutedStyle = lipgloss.NewStyle().
				Foreground(pal.TextMuted)

	gatherDialogButtonStyle = lipgloss.NewStyle().
				Background(pal.BgSelected).
				Foreground(pal.TextPrimary).
				Padding(0, 1)

	gatherDialogButtonActiveStyle = lipgloss.NewStyle().
					Background(pal.Accent).
					Foreground(pal.TextPrimary).
					Padding(0, 1).
					Bold(true)
)
