// Package tui provides terminal UI components for intent management.
package tui

import (
	"github.com/charmbracelet/lipgloss"
)

// ConfirmationResult represents the outcome of a confirmation dialog.
type ConfirmationResult int

const (
	// ConfirmationPending indicates the user hasn't responded yet.
	ConfirmationPending ConfirmationResult = iota
	// ConfirmationYes indicates the user confirmed the action.
	ConfirmationYes
	// ConfirmationNo indicates the user cancelled the action.
	ConfirmationNo
)

// ConfirmationDialog is a modal dialog for confirming destructive actions.
type ConfirmationDialog struct {
	Title   string
	Message string
	Result  ConfirmationResult
}

// NewConfirmationDialog creates a new confirmation dialog.
func NewConfirmationDialog(title, message string) ConfirmationDialog {
	return ConfirmationDialog{
		Title:   title,
		Message: message,
		Result:  ConfirmationPending,
	}
}

// HandleKey processes a key press and updates the result.
func (d *ConfirmationDialog) HandleKey(key string) {
	switch key {
	case "y", "Y", "enter":
		d.Result = ConfirmationYes
	case "n", "N", "esc", "escape":
		d.Result = ConfirmationNo
	}
}

// IsDone returns true if the user has made a choice.
func (d ConfirmationDialog) IsDone() bool {
	return d.Result != ConfirmationPending
}

// Confirmed returns true if the user confirmed the action.
func (d ConfirmationDialog) Confirmed() bool {
	return d.Result == ConfirmationYes
}

// Styles for the confirmation dialog.
var (
	dialogBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(pal.BorderFocus).
			Padding(1, 2).
			Align(lipgloss.Center)

	dialogTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(pal.Error)

	dialogMessageStyle = lipgloss.NewStyle().
				Foreground(pal.TextPrimary)

	dialogButtonStyle = lipgloss.NewStyle().
				Padding(0, 2)

	dialogActiveButtonStyle = dialogButtonStyle.
				Background(pal.Accent).
				Foreground(lipgloss.Color("0")) // Black text on accent background
)

// View renders the confirmation dialog.
func (d ConfirmationDialog) View() string {
	title := dialogTitleStyle.Render(d.Title)
	message := dialogMessageStyle.Render(d.Message)

	// "No" is the default/safe option (styled differently)
	yesBtn := dialogButtonStyle.Render("[y] Yes")
	noBtn := dialogActiveButtonStyle.Render("[n] No")

	buttons := lipgloss.JoinHorizontal(lipgloss.Center, yesBtn, "  ", noBtn)
	hint := HelpStyle.Render("\ny: confirm • n/Esc: cancel")

	content := lipgloss.JoinVertical(
		lipgloss.Center,
		title,
		"",
		message,
		"",
		buttons,
		hint,
	)

	return dialogBoxStyle.Render(content)
}
