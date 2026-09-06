package explorer

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/intent/tui"
	campui "github.com/Obedience-Corp/camp/internal/ui"
)

var errClipboardUnavailable = errors.New("no clipboard available")

// stubClipboard swaps the shared clipboard writer so tests never touch the
// operator's real clipboard, and records what a copy would have written.
func stubClipboard(t *testing.T, err error) *string {
	t.Helper()
	orig := campui.WriteClipboard
	t.Cleanup(func() { campui.WriteClipboard = orig })

	var copied string
	campui.WriteClipboard = func(s string) error {
		copied = s
		return err
	}
	return &copied
}

func pressY(m Model) Model {
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	return next.(Model)
}

func TestCopyIDCopiesSelectedIntent(t *testing.T) {
	copied := stubClipboard(t, nil)

	m := makeTestModel(2, 0)
	m.cursorGroup = 0
	m.cursorItem = 1

	m = pressY(m)

	if *copied != "inbox-1" {
		t.Fatalf("clipboard received %q, want %q", *copied, "inbox-1")
	}
	if m.statusMessage != "Copied inbox-1" {
		t.Errorf("status = %q, want %q", m.statusMessage, "Copied inbox-1")
	}
	if m.statusKind != statusSuccess {
		t.Errorf("status kind = %v, want statusSuccess", m.statusKind)
	}
}

func TestCopyIDCopiesSelectedNote(t *testing.T) {
	copied := stubClipboard(t, nil)

	note := &intent.Intent{
		ID:     "possible-framing-20260709-134513",
		Title:  "Possible framing",
		Status: intent.StatusNote,
	}
	m := makeTestModel(0, 0)
	m.intents = []*intent.Intent{note}
	m.filteredIntents = m.intents
	m.groups = []IntentGroup{{Name: "Notes", Status: intent.StatusNote, Intents: m.intents, Expanded: true}}
	m.cursorGroup = 0
	m.cursorItem = 0

	m = pressY(m)

	if *copied != note.ID {
		t.Fatalf("clipboard received %q, want %q", *copied, note.ID)
	}
	if m.statusKind != statusSuccess {
		t.Errorf("status kind = %v, want statusSuccess", m.statusKind)
	}
}

func TestCopyIDOnGroupHeaderCopiesNothing(t *testing.T) {
	copied := stubClipboard(t, nil)

	m := makeTestModel(2, 0)
	m.cursorGroup = 0
	m.cursorItem = -1

	m = pressY(m)

	if *copied != "" {
		t.Fatalf("group header copied %q, want no copy", *copied)
	}
	if !strings.Contains(m.statusMessage, "No intent selected") {
		t.Errorf("status = %q, want a no-selection message", m.statusMessage)
	}
	if m.statusKind != statusInfo {
		t.Errorf("status kind = %v, want statusInfo", m.statusKind)
	}
}

func TestCopyIDWithoutAnIDReportsAnError(t *testing.T) {
	copied := stubClipboard(t, nil)

	m := makeTestModel(1, 0)
	m.groups[0].Intents[0].ID = ""
	m.cursorGroup = 0
	m.cursorItem = 0

	m = pressY(m)

	if *copied != "" {
		t.Fatalf("copied %q for an intent with no id", *copied)
	}
	if m.statusKind != statusError {
		t.Errorf("status kind = %v, want statusError", m.statusKind)
	}
}

func TestCopyIDReportsClipboardFailure(t *testing.T) {
	stubClipboard(t, errClipboardUnavailable)

	m := makeTestModel(1, 0)
	m.cursorGroup = 0
	m.cursorItem = 0

	m = pressY(m)

	if m.statusKind != statusError {
		t.Fatalf("status kind = %v, want statusError", m.statusKind)
	}
	if !strings.Contains(m.statusMessage, "Copy failed") {
		t.Errorf("status = %q, want a copy-failure message", m.statusMessage)
	}
	if strings.Contains(m.statusMessage, "Copied") {
		t.Errorf("status = %q claims success after a clipboard failure", m.statusMessage)
	}
}

func TestCopyIDFromActionMenu(t *testing.T) {
	copied := stubClipboard(t, nil)

	m := makeTestModel(1, 0)
	m.cursorGroup = 0
	m.cursorItem = 0

	next, _ := m.Update(tui.ActionMenuSelectedMsg{Action: "copy-id"})
	m = next.(Model)

	if *copied != "inbox-0" {
		t.Fatalf("action menu copied %q, want %q", *copied, "inbox-0")
	}
	if m.focus != focusList {
		t.Errorf("focus = %v, want focusList after the action menu closes", m.focus)
	}
}
