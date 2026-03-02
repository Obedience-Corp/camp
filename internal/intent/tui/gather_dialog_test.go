package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/intent"
	tea "github.com/charmbracelet/bubbletea"
)

func makeTestIntents(count int) []*intent.Intent {
	intents := make([]*intent.Intent, count)
	for i := range count {
		intents[i] = &intent.Intent{
			ID:     fmt.Sprintf("intent-%d", i),
			Title:  fmt.Sprintf("Test Intent %d", i),
			Status: intent.StatusInbox,
		}
	}
	return intents
}

// --- Initialization tests ---

func TestNewGatherDialog_InitializesCorrectly(t *testing.T) {
	intents := makeTestIntents(3)
	d := NewGatherDialog(intents)

	if len(d.intents) != 3 {
		t.Errorf("intents count = %d, want 3", len(d.intents))
	}
	if !d.archiveSources {
		t.Error("archiveSources should default to true")
	}
	if d.scrollOffset != 0 {
		t.Errorf("scrollOffset = %d, want 0", d.scrollOffset)
	}
	if d.maxVisible != 15 {
		t.Errorf("maxVisible = %d, want 15", d.maxVisible)
	}
	if d.focusedField != 0 {
		t.Errorf("focusedField = %d, want 0 (title)", d.focusedField)
	}
	if d.done {
		t.Error("done should be false initially")
	}
	if d.cancelled {
		t.Error("cancelled should be false initially")
	}
}

func TestNewGatherDialog_SuggestsTitle_SingleIntent(t *testing.T) {
	intents := []*intent.Intent{
		{ID: "a", Title: "My Feature Request"},
	}
	d := NewGatherDialog(intents)

	if d.Title() != "My Feature Request" {
		t.Errorf("title = %q, want %q", d.Title(), "My Feature Request")
	}
}

func TestNewGatherDialog_SuggestsTitle_CommonPrefix(t *testing.T) {
	intents := []*intent.Intent{
		{ID: "a", Title: "Authentication: login flow"},
		{ID: "b", Title: "Authentication: signup flow"},
		{ID: "c", Title: "Authentication: reset password"},
	}
	d := NewGatherDialog(intents)

	title := d.Title()
	if !strings.HasPrefix(title, "Authentication:") {
		t.Errorf("title = %q, expected prefix 'Authentication:'", title)
	}
}

func TestNewGatherDialog_SuggestsTitle_NoCommonPrefix(t *testing.T) {
	intents := []*intent.Intent{
		{ID: "a", Title: "Feature A"},
		{ID: "b", Title: "Bug B"},
	}
	d := NewGatherDialog(intents)

	title := d.Title()
	// Falls back to first title + "(and more)"
	if !strings.Contains(title, "Feature A") {
		t.Errorf("title = %q, expected to contain first intent title", title)
	}
}

func TestNewGatherDialog_EmptyIntents(t *testing.T) {
	d := NewGatherDialog(nil)

	if len(d.intents) != 0 {
		t.Errorf("intents count = %d, want 0", len(d.intents))
	}
	if d.Title() != "" {
		t.Errorf("title = %q, want empty for nil intents", d.Title())
	}
}

// --- View rendering tests ---

func TestGatherDialog_View_ShowsCountHeader(t *testing.T) {
	intents := makeTestIntents(5)
	d := NewGatherDialog(intents)

	view := d.View()
	if !strings.Contains(view, "Gather 5 Intents") {
		t.Error("view should contain 'Gather 5 Intents' header")
	}
}

func TestGatherDialog_View_ShowsAllIntents(t *testing.T) {
	intents := makeTestIntents(3)
	d := NewGatherDialog(intents)

	view := d.View()
	for _, i := range intents {
		if !strings.Contains(view, i.Title) {
			t.Errorf("view should contain intent title %q", i.Title)
		}
	}
}

func TestGatherDialog_View_ShowsStatusBadges(t *testing.T) {
	intents := []*intent.Intent{
		{ID: "a", Title: "Intent A", Status: intent.StatusInbox},
		{ID: "b", Title: "Intent B", Status: intent.StatusActive},
	}
	d := NewGatherDialog(intents)

	view := d.View()
	if !strings.Contains(view, "[inbox]") {
		t.Error("view should contain [inbox] badge")
	}
	if !strings.Contains(view, "[active]") {
		t.Error("view should contain [active] badge")
	}
}

func TestGatherDialog_View_ShowsSelectedCount(t *testing.T) {
	intents := makeTestIntents(7)
	d := NewGatherDialog(intents)

	view := d.View()
	if !strings.Contains(view, "(7)") {
		t.Error("view should contain selected count '(7):'")
	}
}

func TestGatherDialog_View_ScrollIndicators(t *testing.T) {
	// Create more intents than maxVisible
	intents := makeTestIntents(20)
	d := NewGatherDialog(intents)

	view := d.View()
	// At scroll offset 0, should not show up indicator but should show down
	if strings.Contains(view, "more above") {
		t.Error("should not show 'more above' at scroll offset 0")
	}
	if !strings.Contains(view, "more below") {
		t.Error("should show 'more below' when intents exceed maxVisible")
	}

	// Scroll down
	d.scrollOffset = 5
	view = d.View()
	if !strings.Contains(view, "more above") {
		t.Error("should show 'more above' when scrolled down")
	}
}

// --- Scroll behavior tests ---

func TestGatherDialog_ScrollDown(t *testing.T) {
	intents := makeTestIntents(25)
	d := NewGatherDialog(intents)

	ctrlD := tea.KeyMsg{Type: tea.KeyCtrlD}
	d, _ = d.Update(ctrlD)

	if d.scrollOffset <= 0 {
		t.Errorf("scrollOffset = %d, want > 0 after scroll down", d.scrollOffset)
	}
}

func TestGatherDialog_ScrollUp(t *testing.T) {
	intents := makeTestIntents(25)
	d := NewGatherDialog(intents)
	d.scrollOffset = 10

	ctrlU := tea.KeyMsg{Type: tea.KeyCtrlU}
	d, _ = d.Update(ctrlU)

	if d.scrollOffset >= 10 {
		t.Errorf("scrollOffset = %d, want < 10 after scroll up", d.scrollOffset)
	}
}

func TestGatherDialog_ScrollBounds_CannotExceedMax(t *testing.T) {
	intents := makeTestIntents(20)
	d := NewGatherDialog(intents)

	// Scroll down many times
	ctrlD := tea.KeyMsg{Type: tea.KeyCtrlD}
	for range 20 {
		d, _ = d.Update(ctrlD)
	}

	maxOffset := len(intents) - d.maxVisible
	if d.scrollOffset > maxOffset {
		t.Errorf("scrollOffset = %d, should not exceed %d", d.scrollOffset, maxOffset)
	}
}

func TestGatherDialog_ScrollBounds_CannotGoNegative(t *testing.T) {
	intents := makeTestIntents(20)
	d := NewGatherDialog(intents)
	d.scrollOffset = 2

	// Scroll up many times
	ctrlU := tea.KeyMsg{Type: tea.KeyCtrlU}
	for range 10 {
		d, _ = d.Update(ctrlU)
	}

	if d.scrollOffset < 0 {
		t.Errorf("scrollOffset = %d, should not go negative", d.scrollOffset)
	}
}

func TestGatherDialog_ScrollDown_NoOpWhenFewIntents(t *testing.T) {
	intents := makeTestIntents(5) // fewer than maxVisible
	d := NewGatherDialog(intents)

	ctrlD := tea.KeyMsg{Type: tea.KeyCtrlD}
	d, _ = d.Update(ctrlD)

	if d.scrollOffset != 0 {
		t.Errorf("scrollOffset = %d, want 0 when intents fit in view", d.scrollOffset)
	}
}

// --- Archive checkbox tests ---

func TestGatherDialog_ArchiveDefaultTrue(t *testing.T) {
	intents := makeTestIntents(3)
	d := NewGatherDialog(intents)

	if !d.ArchiveSources() {
		t.Error("archive should default to true")
	}
}

func TestGatherDialog_ToggleArchive(t *testing.T) {
	intents := makeTestIntents(3)
	d := NewGatherDialog(intents)

	// Tab to archive field
	tab := tea.KeyMsg{Type: tea.KeyTab}
	d, _ = d.Update(tab) // → archive (field 1)

	if d.focusedField != 1 {
		t.Fatalf("focusedField = %d, want 1 (archive)", d.focusedField)
	}

	// Space toggles archive
	space := tea.KeyMsg{Type: tea.KeySpace}
	d, _ = d.Update(space)

	if d.ArchiveSources() {
		t.Error("archive should be false after toggle")
	}

	// Toggle again
	d, _ = d.Update(space)
	if !d.ArchiveSources() {
		t.Error("archive should be true after second toggle")
	}
}

// --- Done / Cancel tests ---

func TestGatherDialog_EscCancels(t *testing.T) {
	intents := makeTestIntents(3)
	d := NewGatherDialog(intents)

	esc := tea.KeyMsg{Type: tea.KeyEsc}
	d, _ = d.Update(esc)

	if !d.Done() {
		t.Error("dialog should be done after esc")
	}
	if !d.Cancelled() {
		t.Error("dialog should be cancelled after esc")
	}
}

func TestGatherDialog_EnterConfirms(t *testing.T) {
	intents := makeTestIntents(3)
	d := NewGatherDialog(intents)
	// Title is auto-set, so it's non-empty

	// Tab to buttons
	tab := tea.KeyMsg{Type: tea.KeyTab}
	d, _ = d.Update(tab) // → archive
	d, _ = d.Update(tab) // → buttons

	enter := tea.KeyMsg{Type: tea.KeyEnter}
	d, _ = d.Update(enter)

	if !d.Done() {
		t.Error("dialog should be done after enter on buttons")
	}
	if d.Cancelled() {
		t.Error("dialog should not be cancelled")
	}
}

func TestGatherDialog_EnterOnTitleMovesToNextField(t *testing.T) {
	intents := makeTestIntents(3)
	d := NewGatherDialog(intents)

	if d.focusedField != 0 {
		t.Fatal("should start on title field")
	}

	enter := tea.KeyMsg{Type: tea.KeyEnter}
	d, _ = d.Update(enter)

	if d.focusedField != 1 {
		t.Errorf("focusedField = %d, want 1 after enter on title", d.focusedField)
	}
	if d.Done() {
		t.Error("enter on title should NOT complete the dialog")
	}
}

// --- IntentIDs ---

func TestGatherDialog_IntentIDs(t *testing.T) {
	intents := makeTestIntents(3)
	d := NewGatherDialog(intents)

	ids := d.IntentIDs()
	if len(ids) != 3 {
		t.Errorf("IntentIDs() = %d, want 3", len(ids))
	}
	for i, id := range ids {
		expected := fmt.Sprintf("intent-%d", i)
		if id != expected {
			t.Errorf("IntentIDs()[%d] = %q, want %q", i, id, expected)
		}
	}
}

// --- Tab cycling ---

func TestGatherDialog_TabCyclesFields(t *testing.T) {
	intents := makeTestIntents(3)
	d := NewGatherDialog(intents)

	tab := tea.KeyMsg{Type: tea.KeyTab}

	d, _ = d.Update(tab) // 0 → 1
	if d.focusedField != 1 {
		t.Errorf("after tab: focusedField = %d, want 1", d.focusedField)
	}

	d, _ = d.Update(tab) // 1 → 2
	if d.focusedField != 2 {
		t.Errorf("after tab: focusedField = %d, want 2", d.focusedField)
	}

	d, _ = d.Update(tab) // 2 → 0 (wraps)
	if d.focusedField != 0 {
		t.Errorf("after tab: focusedField = %d, want 0 (wrap)", d.focusedField)
	}
}

func TestGatherDialog_ShiftTabCyclesReverse(t *testing.T) {
	intents := makeTestIntents(3)
	d := NewGatherDialog(intents)

	shiftTab := tea.KeyMsg{Type: tea.KeyShiftTab}

	d, _ = d.Update(shiftTab) // 0 → 2 (reverse wrap)
	if d.focusedField != 2 {
		t.Errorf("after shift-tab: focusedField = %d, want 2", d.focusedField)
	}

	d, _ = d.Update(shiftTab) // 2 → 1
	if d.focusedField != 1 {
		t.Errorf("after shift-tab: focusedField = %d, want 1", d.focusedField)
	}
}

// --- commonPrefix helper ---

func TestCommonPrefix(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  string
	}{
		{"empty", nil, ""},
		{"single", []string{"hello"}, "hello"},
		{"common", []string{"abc-1", "abc-2", "abc-3"}, "abc-"},
		{"no common", []string{"foo", "bar"}, ""},
		{"partial", []string{"test-alpha", "test-beta"}, "test-"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := commonPrefix(tt.input)
			if got != tt.want {
				t.Errorf("commonPrefix(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
