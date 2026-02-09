package explorer

import (
	"fmt"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/obediencecorp/camp/internal/intent"
)

// modelFrom extracts a Model from tea.Model regardless of whether it's a value or pointer.
func modelFrom(tm tea.Model) Model {
	switch v := tm.(type) {
	case *Model:
		return *v
	case Model:
		return v
	default:
		panic(fmt.Sprintf("unexpected tea.Model type: %T", tm))
	}
}

// --- handleGatherStart tests ---

func TestHandleGatherStart_NoSelections(t *testing.T) {
	m := makeTestModel(5, 3)
	m.cursorGroup = 0
	m.cursorItem = 0

	result, cmd := m.handleGatherStart()
	rm := modelFrom(result)

	if rm.focus != focusList {
		t.Errorf("focus = %v, want focusList", rm.focus)
	}
	if rm.statusMessage == "" {
		t.Error("expected status message about selecting intents")
	}
	if cmd != nil {
		t.Error("expected nil cmd")
	}
}

func TestHandleGatherStart_OneSelection(t *testing.T) {
	m := makeTestModel(5, 3)
	m.selectedIntents["inbox-0"] = true

	result, cmd := m.handleGatherStart()
	rm := modelFrom(result)

	if rm.focus == focusGatherDialog {
		t.Error("should NOT open gather dialog with only 1 selection")
	}
	if rm.statusMessage == "" {
		t.Error("expected status message about needing 2+ intents")
	}
	if cmd != nil {
		t.Error("expected nil cmd")
	}
}

func TestHandleGatherStart_TwoSelections(t *testing.T) {
	m := makeTestModel(5, 3)
	m.selectedIntents["inbox-0"] = true
	m.selectedIntents["inbox-1"] = true

	result, _ := m.handleGatherStart()
	rm := modelFrom(result)

	if rm.focus != focusGatherDialog {
		t.Errorf("focus = %v, want focusGatherDialog", rm.focus)
	}
	intents := rm.gatherDialog.Intents()
	if len(intents) != 2 {
		t.Errorf("gather dialog intents = %d, want 2", len(intents))
	}
}

func TestHandleGatherStart_ManySelections(t *testing.T) {
	m := makeTestModel(5, 3)
	for i := range 5 {
		m.selectedIntents[fmt.Sprintf("inbox-%d", i)] = true
	}

	result, _ := m.handleGatherStart()
	rm := modelFrom(result)

	if rm.focus != focusGatherDialog {
		t.Errorf("focus = %v, want focusGatherDialog", rm.focus)
	}
	intents := rm.gatherDialog.Intents()
	if len(intents) != 5 {
		t.Errorf("gather dialog intents = %d, want 5", len(intents))
	}
}

// --- handleGatherGroup tests ---

func TestHandleGatherGroup_OnGroupHeader(t *testing.T) {
	m := makeTestModel(3, 2)
	m.cursorGroup = 0
	m.cursorItem = -1

	result, _ := m.handleGatherGroup()
	rm := modelFrom(result)

	if rm.focus != focusGatherDialog {
		t.Errorf("focus = %v, want focusGatherDialog", rm.focus)
	}
	if !rm.multiSelectMode {
		t.Error("expected multiSelectMode to be true")
	}
	// All 3 inbox intents should be selected
	for i := range 3 {
		id := fmt.Sprintf("inbox-%d", i)
		if !rm.selectedIntents[id] {
			t.Errorf("intent %s should be selected", id)
		}
	}
	intents := rm.gatherDialog.Intents()
	if len(intents) != 3 {
		t.Errorf("gather dialog intents = %d, want 3", len(intents))
	}
}

func TestHandleGatherGroup_GroupTooSmall(t *testing.T) {
	m := makeTestModel(1, 0)
	m.cursorGroup = 0
	m.cursorItem = -1

	result, _ := m.handleGatherGroup()
	rm := modelFrom(result)

	if rm.focus == focusGatherDialog {
		t.Error("should NOT open gather dialog for group with < 2 intents")
	}
	if rm.statusMessage == "" {
		t.Error("expected status message about group needing 2+ intents")
	}
}

func TestHandleGatherGroup_EmptyGroup(t *testing.T) {
	m := makeTestModel(3, 0)
	// Ready group (index 2) has 0 intents
	m.cursorGroup = 2
	m.cursorItem = -1

	result, _ := m.handleGatherGroup()
	rm := modelFrom(result)

	if rm.focus == focusGatherDialog {
		t.Error("should NOT open gather dialog for empty group")
	}
	if rm.statusMessage == "" {
		t.Error("expected status message")
	}
}

func TestHandleGatherGroup_BoundsCheck(t *testing.T) {
	m := makeTestModel(3, 2)

	// Negative cursor group
	m.cursorGroup = -1
	result, _ := m.handleGatherGroup()
	rm := modelFrom(result)
	if rm.focus == focusGatherDialog {
		t.Error("negative cursorGroup should not open dialog")
	}

	// Out of range cursor group
	m.cursorGroup = 100
	result, _ = m.handleGatherGroup()
	rm = modelFrom(result)
	if rm.focus == focusGatherDialog {
		t.Error("out-of-range cursorGroup should not open dialog")
	}
}

// --- toggleSelection tests ---

func TestToggleSelection_SelectAndDeselect(t *testing.T) {
	m := makeTestModel(3, 0)
	i := m.intents[0]

	// Select
	m.toggleSelection(i)
	if !m.selectedIntents[i.ID] {
		t.Error("intent should be selected after toggle")
	}

	// Deselect
	m.toggleSelection(i)
	if m.selectedIntents[i.ID] {
		t.Error("intent should be deselected after second toggle")
	}
}

func TestToggleSelection_NilIntent(t *testing.T) {
	m := makeTestModel(3, 0)
	// Should not panic
	m.toggleSelection(nil)
}

func TestToggleSelection_MultiSelectModeActivates(t *testing.T) {
	m := makeTestModel(3, 0)
	if m.multiSelectMode {
		t.Error("multiSelectMode should start false")
	}

	// Select first intent -> enters multi-select mode
	m.toggleSelection(m.intents[0])
	if !m.multiSelectMode {
		t.Error("multiSelectMode should be true after selecting")
	}

	// Select second
	m.toggleSelection(m.intents[1])
	if !m.multiSelectMode {
		t.Error("multiSelectMode should still be true with 2 selections")
	}

	// Deselect second -> still has one selection
	m.toggleSelection(m.intents[1])
	if !m.multiSelectMode {
		t.Error("multiSelectMode should still be true with 1 selection")
	}

	// Deselect first -> no selections -> exits multi-select
	m.toggleSelection(m.intents[0])
	if m.multiSelectMode {
		t.Error("multiSelectMode should be false when all deselected")
	}
}

func TestExitMultiSelectMode(t *testing.T) {
	m := makeTestModel(3, 0)
	m.selectedIntents["inbox-0"] = true
	m.selectedIntents["inbox-1"] = true
	m.multiSelectMode = true

	m.exitMultiSelectMode()

	if m.multiSelectMode {
		t.Error("multiSelectMode should be false after exit")
	}
	if len(m.selectedIntents) != 0 {
		t.Errorf("selectedIntents should be empty, got %d", len(m.selectedIntents))
	}
	if m.statusMessage == "" {
		t.Error("expected status message about cleared selection")
	}
}

// --- Ctrl+G routing tests ---

func TestCtrlG_OnGroupHeader_NoSelections(t *testing.T) {
	m := makeTestModel(3, 2)
	m.cursorGroup = 0
	m.cursorItem = -1

	msg := tea.KeyMsg{Type: tea.KeyCtrlG}
	result, _ := m.updateNormal(msg)
	rm := modelFrom(result)

	// Should call handleGatherGroup -> opens dialog for inbox group (3 intents)
	if rm.focus != focusGatherDialog {
		t.Errorf("focus = %v, want focusGatherDialog", rm.focus)
	}
}

func TestCtrlG_OnItem_WithSelections(t *testing.T) {
	m := makeTestModel(3, 2)
	m.cursorGroup = 0
	m.cursorItem = 0
	m.selectedIntents["inbox-0"] = true
	m.selectedIntents["inbox-1"] = true

	msg := tea.KeyMsg{Type: tea.KeyCtrlG}
	result, _ := m.updateNormal(msg)
	rm := modelFrom(result)

	// Should call handleGatherStart -> opens dialog with 2 selected
	if rm.focus != focusGatherDialog {
		t.Errorf("focus = %v, want focusGatherDialog", rm.focus)
	}
}

func TestCtrlG_OnItem_NoSelections(t *testing.T) {
	m := makeTestModel(3, 2)
	m.cursorGroup = 0
	m.cursorItem = 0

	msg := tea.KeyMsg{Type: tea.KeyCtrlG}
	result, _ := m.updateNormal(msg)
	rm := modelFrom(result)

	// Should call handleGatherStart -> shows "select 2+" message
	if rm.focus == focusGatherDialog {
		t.Error("should NOT open gather dialog with no selections")
	}
	if rm.statusMessage == "" {
		t.Error("expected status message")
	}
}

func TestCtrlG_OnGroupHeader_WithSelections(t *testing.T) {
	m := makeTestModel(3, 2)
	m.cursorGroup = 0
	m.cursorItem = -1
	m.selectedIntents["active-0"] = true
	m.selectedIntents["active-1"] = true

	msg := tea.KeyMsg{Type: tea.KeyCtrlG}
	result, _ := m.updateNormal(msg)
	rm := modelFrom(result)

	// Has selections -> handleGatherStart, not handleGatherGroup
	if rm.focus != focusGatherDialog {
		t.Errorf("focus = %v, want focusGatherDialog", rm.focus)
	}
}

// --- Confirmation flow tests ---

func TestGatherConfirm_ArchiveTrueShowsConfirmation(t *testing.T) {
	m := makeTestModel(3, 0)
	m.selectedIntents["inbox-0"] = true
	m.selectedIntents["inbox-1"] = true
	m.selectedIntents["inbox-2"] = true

	// Open gather dialog
	m.handleGatherStart()
	if m.focus != focusGatherDialog {
		t.Fatal("expected focusGatherDialog after handleGatherStart")
	}

	// Archive is true by default; tab to buttons and confirm
	tabMsg := tea.KeyMsg{Type: tea.KeyTab}
	m.gatherDialog, _ = m.gatherDialog.Update(tabMsg) // -> archive
	m.gatherDialog, _ = m.gatherDialog.Update(tabMsg) // -> buttons

	// Press enter to confirm
	enterMsg := tea.KeyMsg{Type: tea.KeyEnter}
	m.gatherDialog, _ = m.gatherDialog.Update(enterMsg)

	if !m.gatherDialog.Done() {
		t.Fatal("gather dialog should be done after enter on buttons")
	}
	if m.gatherDialog.Cancelled() {
		t.Fatal("gather dialog should not be cancelled")
	}

	// Now simulate updateGatherDialog processing
	result, _ := m.updateGatherDialog(enterMsg)
	rm := modelFrom(result)

	// Since archive=true, should show confirmation
	if rm.focus != focusConfirm {
		t.Errorf("focus = %v, want focusConfirm (archive=true should require confirmation)", rm.focus)
	}
	if rm.pendingAction != "gather" {
		t.Errorf("pendingAction = %q, want 'gather'", rm.pendingAction)
	}
}

func TestGatherConfirm_ArchiveFalseSkipsConfirmation(t *testing.T) {
	m := makeTestModel(3, 0)
	m.selectedIntents["inbox-0"] = true
	m.selectedIntents["inbox-1"] = true
	m.selectedIntents["inbox-2"] = true

	// Open gather dialog
	m.handleGatherStart()

	// Tab to archive checkbox and toggle it off
	tabMsg := tea.KeyMsg{Type: tea.KeyTab}
	m.gatherDialog, _ = m.gatherDialog.Update(tabMsg) // -> archive

	spaceMsg := tea.KeyMsg{Type: tea.KeySpace}
	m.gatherDialog, _ = m.gatherDialog.Update(spaceMsg) // toggle archive off

	if m.gatherDialog.ArchiveSources() {
		t.Fatal("archive should be false after toggle")
	}

	// Tab to buttons and confirm
	m.gatherDialog, _ = m.gatherDialog.Update(tabMsg) // -> buttons

	enterMsg := tea.KeyMsg{Type: tea.KeyEnter}
	m.gatherDialog, _ = m.gatherDialog.Update(enterMsg) // confirm

	if !m.gatherDialog.Done() {
		t.Fatal("gather dialog should be done")
	}

	// Process in updateGatherDialog - should NOT show confirmation
	result, cmd := m.updateGatherDialog(enterMsg)
	rm := modelFrom(result)

	// Non-destructive -> focus returns to list and cmd is non-nil (executeGather)
	if rm.focus != focusList {
		t.Errorf("focus = %v, want focusList (archive=false skips confirmation)", rm.focus)
	}
	if cmd == nil {
		t.Error("expected non-nil cmd (executeGather) for non-destructive gather")
	}
}

// --- getSelectedIntentObjects / getSelectedIDs ---

func TestGetSelectedIntentObjects(t *testing.T) {
	m := makeTestModel(5, 3)
	m.selectedIntents["inbox-0"] = true
	m.selectedIntents["inbox-2"] = true
	m.selectedIntents["active-1"] = true

	objects := m.getSelectedIntentObjects()
	if len(objects) != 3 {
		t.Errorf("getSelectedIntentObjects returned %d, want 3", len(objects))
	}

	ids := make(map[string]bool)
	for _, o := range objects {
		ids[o.ID] = true
	}
	for _, expected := range []string{"inbox-0", "inbox-2", "active-1"} {
		if !ids[expected] {
			t.Errorf("missing expected intent %s", expected)
		}
	}
}

func TestGetSelectedIDs(t *testing.T) {
	m := makeTestModel(3, 0)
	m.selectedIntents["inbox-0"] = true
	m.selectedIntents["inbox-2"] = true

	ids := m.getSelectedIDs()
	if len(ids) != 2 {
		t.Errorf("getSelectedIDs returned %d, want 2", len(ids))
	}
}

// --- Space key toggles selection ---

func TestSpaceKey_TogglesIntentSelection(t *testing.T) {
	m := makeTestModel(3, 0)
	m.cursorGroup = 0
	m.cursorItem = 0

	spaceMsg := tea.KeyMsg{Type: tea.KeySpace}
	result, _ := m.updateNormal(spaceMsg)
	rm := modelFrom(result)

	if !rm.selectedIntents["inbox-0"] {
		t.Error("space should select the intent under cursor")
	}
	if !rm.multiSelectMode {
		t.Error("should enter multiSelectMode")
	}
}

func TestSpaceKey_OnGroupHeader_TogglesExpansion(t *testing.T) {
	m := makeTestModel(3, 0)
	m.cursorGroup = 0
	m.cursorItem = -1

	wasExpanded := m.groups[0].Expanded
	spaceMsg := tea.KeyMsg{Type: tea.KeySpace}
	result, _ := m.updateNormal(spaceMsg)
	rm := modelFrom(result)

	if rm.groups[0].Expanded == wasExpanded {
		t.Error("space on group header should toggle expansion")
	}
}

// --- Esc clears multi-select ---

func TestEsc_ClearsMultiSelect(t *testing.T) {
	m := makeTestModel(3, 0)
	m.selectedIntents["inbox-0"] = true
	m.selectedIntents["inbox-1"] = true
	m.multiSelectMode = true

	escMsg := tea.KeyMsg{Type: tea.KeyEsc}
	result, _ := m.updateNormal(escMsg)
	rm := modelFrom(result)

	if rm.multiSelectMode {
		t.Error("esc should exit multiSelectMode")
	}
	if len(rm.selectedIntents) != 0 {
		t.Error("esc should clear selectedIntents")
	}
}

// --- makeTestModelWithStatuses helper for diverse group testing ---

func makeTestModelWithStatuses(counts map[intent.Status]int) Model {
	m := makeTestModel(0, 0)
	var intents []*intent.Intent
	for status, count := range counts {
		for i := range count {
			intents = append(intents, &intent.Intent{
				ID:        fmt.Sprintf("%s-%d", status.String(), i),
				Title:     fmt.Sprintf("%s Intent %d", status.String(), i),
				Status:    status,
				Type:      intent.TypeFeature,
				CreatedAt: time.Now(),
			})
		}
	}
	m.intents = intents
	m.filteredIntents = intents
	m.groups = groupIntentsByStatus(intents)
	return m
}

func TestHandleGatherGroup_ActiveGroup(t *testing.T) {
	m := makeTestModelWithStatuses(map[intent.Status]int{
		intent.StatusInbox:  1,
		intent.StatusActive: 4,
	})
	m.cursorGroup = 1 // Active group
	m.cursorItem = -1

	result, _ := m.handleGatherGroup()
	rm := modelFrom(result)

	if rm.focus != focusGatherDialog {
		t.Errorf("focus = %v, want focusGatherDialog for active group with 4 intents", rm.focus)
	}
	intents := rm.gatherDialog.Intents()
	if len(intents) != 4 {
		t.Errorf("gather dialog intents = %d, want 4", len(intents))
	}
}
