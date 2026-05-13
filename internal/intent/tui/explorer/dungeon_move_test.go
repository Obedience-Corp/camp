package explorer

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Obedience-Corp/camp/internal/intent"
)

// TestDungeonMove_SomedayToDone_TransitionsAndSurfacesProgress is a
// regression test for #278: moving an intent from one dungeon status to
// another (e.g. Someday → Done) used to appear frozen because the
// auto-commit retry path stalled briefly without surfacing any in-progress
// feedback. The user could not tell whether the TUI was alive or hung.
//
// After the fix, pressing enter in the dungeon reason input:
//   - immediately sets focus back to focusList so the user can interact
//   - sets statusMessage to a "Moving to ..." progress indicator
//   - returns a non-nil tea.Cmd that performs the actual move
//
// The underlying move continues asynchronously and the moveFinishedMsg
// handler replaces the progress message with the final outcome.
func TestDungeonMove_SomedayToDone_TransitionsAndSurfacesProgress(t *testing.T) {
	ctx := context.Background()
	m := NewModel(ctx, nil, nil, "/tmp/intents", "/tmp/campaign", "test-id", "", nil)
	m.ready = true

	somedayIntent := &intent.Intent{
		ID:        "someday-1",
		Title:     "Someday item",
		Status:    intent.StatusSomeday,
		Type:      intent.TypeFeature,
		CreatedAt: time.Now(),
	}
	m.intents = []*intent.Intent{somedayIntent}
	m.filteredIntents = m.intents

	// Open the dungeon-reason input directly (the same call updateMove makes
	// when the user picks a dungeon destination from the move overlay).
	m.startDungeonReasonInput(somedayIntent, intent.StatusDone, "move")

	if m.focus != focusDungeonReason {
		t.Fatalf("expected focusDungeonReason after startDungeonReasonInput, got %v", m.focus)
	}
	if !m.dungeonReasonInput.Focused() {
		t.Fatal("expected reason textinput to be focused so the user can type")
	}
	if m.dungeonReasonFor != intent.StatusDone {
		t.Fatalf("dungeonReasonFor = %v, want StatusDone", m.dungeonReasonFor)
	}

	// Verify viewDungeonReason actually renders (regression for the original
	// "freeze" report — if no render branch existed, the View would silently
	// fall through to the previous frame).
	rendered := m.View()
	if rendered == "" {
		t.Fatal("View() returned empty string while focusDungeonReason was active")
	}
	if !strings.Contains(rendered, "Reason") {
		t.Fatalf("expected dungeon-reason dialog to render, got: %q", rendered)
	}

	// Type a reason character-by-character so we go through the same key
	// routing the user does. Empty-reason rejection is also exercised.
	model, _ := m.updateDungeonReason(tea.KeyMsg{Type: tea.KeyEnter})
	if got := model.(*Model); got.statusMessage != "Reason is required for dungeon moves" {
		t.Fatalf("empty-reason enter should reject with helpful statusMessage, got %q",
			got.statusMessage)
	}
	if got := model.(*Model); got.focus != focusDungeonReason {
		t.Fatal("empty-reason enter should keep focus on the reason input")
	}

	for _, r := range "reclassifying" {
		model, _ = m.updateDungeonReason(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		_ = model // mutations happen through the *Model receiver; m already reflects them
	}

	if strings.TrimSpace(m.dungeonReasonInput.Value()) == "" {
		t.Fatal("textinput did not accept typed characters")
	}

	model, cmd := m.updateDungeonReason(tea.KeyMsg{Type: tea.KeyEnter})
	got := model.(*Model)

	if got.focus != focusList {
		t.Fatalf("expected focusList after committing reason, got %v", got.focus)
	}
	if !strings.Contains(got.statusMessage, "Moving to") {
		t.Fatalf("expected 'Moving to ...' progress message, got %q", got.statusMessage)
	}
	if got.dungeonReasonIntent != nil {
		t.Fatal("dungeonReasonIntent should be cleared after committing")
	}
	if got.dungeonReasonAction != "" {
		t.Fatal("dungeonReasonAction should be cleared after committing")
	}
	if cmd == nil {
		t.Fatal("expected a non-nil tea.Cmd to dispatch the move; got nil")
	}
}

// TestDungeonMove_EscCancelsAndRestoresList confirms the cancel path leaves
// the model in a clean state, independent of the freeze fix.
func TestDungeonMove_EscCancelsAndRestoresList(t *testing.T) {
	ctx := context.Background()
	m := NewModel(ctx, nil, nil, "/tmp/intents", "/tmp/campaign", "test-id", "", nil)
	m.ready = true

	somedayIntent := &intent.Intent{
		ID:        "someday-1",
		Title:     "Someday item",
		Status:    intent.StatusSomeday,
		Type:      intent.TypeFeature,
		CreatedAt: time.Now(),
	}
	m.intents = []*intent.Intent{somedayIntent}
	m.filteredIntents = m.intents

	m.startDungeonReasonInput(somedayIntent, intent.StatusDone, "move")

	model, cmd := m.updateDungeonReason(tea.KeyMsg{Type: tea.KeyEsc})
	got := model.(*Model)

	if got.focus != focusList {
		t.Fatalf("esc should return to focusList, got %v", got.focus)
	}
	if got.dungeonReasonIntent != nil {
		t.Fatal("dungeonReasonIntent should be cleared on cancel")
	}
	if got.dungeonReasonAction != "" {
		t.Fatal("dungeonReasonAction should be cleared on cancel")
	}
	if cmd != nil {
		t.Fatal("cancel should not dispatch any tea.Cmd")
	}
}
