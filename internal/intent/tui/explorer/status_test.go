package explorer

import (
	"testing"

	"github.com/Obedience-Corp/camp/internal/intent/tui"
)

func TestStatusStyleBySeverity(t *testing.T) {
	tests := []struct {
		name string
		set  func(*Model)
		want string
	}{
		{"info", func(m *Model) { m.setStatus("Archiving...") }, tui.HelpStyle.Render("x")},
		{"success", func(m *Model) { m.setStatusSuccess("Archived") }, tui.SuccessStyle.Render("x")},
		{"error", func(m *Model) { m.setStatusError("Archive failed") }, tui.ErrorStyle.Render("x")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := makeTestModel(1, 0)
			tt.set(&m)
			if got := m.statusStyle().Render("x"); got != tt.want {
				t.Errorf("style = %q, want %q", got, tt.want)
			}
		})
	}
}

// A success message must not inherit the previous message's severity. Nothing
// clears the footer on keypress, so this is the regression the setStatus*
// helpers exist to prevent.
func TestStatusSeverityDoesNotGoStale(t *testing.T) {
	m := makeTestModel(1, 0)

	m.setStatusError("Archive failed: disk full")
	m.setStatusSuccess("Archived")
	if m.statusKind != statusSuccess {
		t.Fatalf("kind = %v after success following an error, want statusSuccess", m.statusKind)
	}

	m.setStatusError("Delete failed: permission denied")
	if m.statusKind != statusError {
		t.Fatalf("kind = %v after error following a success, want statusError", m.statusKind)
	}

	m.clearStatus()
	if m.statusMessage != "" || m.statusKind != statusInfo {
		t.Fatalf("clearStatus left %q / %v", m.statusMessage, m.statusKind)
	}
}
