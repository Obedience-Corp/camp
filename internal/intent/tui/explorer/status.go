package explorer

import (
	"github.com/Obedience-Corp/camp/internal/intent/tui"
	"github.com/charmbracelet/lipgloss"
)

// statusKind is the severity of the explorer's footer message. The zero value
// is statusInfo, so a message set without an explicit severity reads as
// neutral rather than as a failure.
type statusKind int

const (
	// statusInfo is neutral: progress, view changes, and usage hints.
	statusInfo statusKind = iota
	// statusSuccess reports a completed mutation.
	statusSuccess
	// statusError reports a failure or a refusal.
	statusError
)

// setStatus shows a neutral message in the footer.
func (m *Model) setStatus(msg string) {
	m.statusMessage = msg
	m.statusKind = statusInfo
}

// setStatusSuccess shows a message reporting that an action completed.
func (m *Model) setStatusSuccess(msg string) {
	m.statusMessage = msg
	m.statusKind = statusSuccess
}

// setStatusError shows a message reporting a failure or a refusal.
func (m *Model) setStatusError(msg string) {
	m.statusMessage = msg
	m.statusKind = statusError
}

// clearStatus removes the footer message.
func (m *Model) clearStatus() {
	m.statusMessage = ""
	m.statusKind = statusInfo
}

// statusStyle returns the footer style for the current message severity.
func (m Model) statusStyle() lipgloss.Style {
	switch m.statusKind {
	case statusError:
		return tui.ErrorStyle
	case statusSuccess:
		return tui.SuccessStyle
	default:
		return tui.HelpStyle
	}
}
