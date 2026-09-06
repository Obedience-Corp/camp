package tui

import (
	"strings"
	"testing"
)

func TestHelpContentDocumentsMeetingActions(t *testing.T) {
	for _, want := range []string{
		"t           Filter by type (non-meeting)",
		"MEETING NOTES (selected meeting)",
		"t           Open transcript sidecar",
		"A           Open meeting audio (machine-local)",
		"x           Extract checkbox action items to inbox",
	} {
		if !strings.Contains(helpContent, want) {
			t.Errorf("help content missing %q", want)
		}
	}
}

func TestHelpContentDocumentsCopyBinding(t *testing.T) {
	if !strings.Contains(helpContent, "y           Copy intent id to clipboard") {
		t.Error("help content missing the copy-id binding")
	}
}
