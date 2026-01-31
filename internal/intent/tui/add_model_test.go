package tui

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/obediencecorp/camp/internal/concept"
	"github.com/obediencecorp/camp/internal/intent/tui/vim"
)

func TestIntentAddModel_InitialState(t *testing.T) {
	ctx := context.Background()
	svc := mockConceptService{
		concepts: []concept.Concept{
			{Name: "Projects", Path: "projects", HasItems: true},
		},
	}

	m := NewIntentAddModel(ctx, svc, AddOptions{
		DefaultType: "idea",
		FullMode:    false,
	})

	if m.step != addStepTitle {
		t.Errorf("Expected initial step to be addStepTitle, got %v", m.step)
	}
	if m.cancelled {
		t.Error("Model should not be cancelled initially")
	}
	if m.Done() {
		t.Error("Model should not be done initially")
	}
	if m.typeIdx != 0 {
		t.Errorf("Expected default type index 0 (idea), got %d", m.typeIdx)
	}
}

func TestIntentAddModel_DefaultTypeSelection(t *testing.T) {
	ctx := context.Background()
	svc := mockConceptService{}

	// Test with "feature" as default type
	m := NewIntentAddModel(ctx, svc, AddOptions{
		DefaultType: "feature",
		FullMode:    false,
	})

	// feature is index 1 in intentTypes
	if m.typeIdx != 1 {
		t.Errorf("Expected type index 1 (feature), got %d", m.typeIdx)
	}

	// Test with "bug" as default type
	m = NewIntentAddModel(ctx, svc, AddOptions{
		DefaultType: "bug",
		FullMode:    false,
	})

	// bug is index 2 in intentTypes
	if m.typeIdx != 2 {
		t.Errorf("Expected type index 2 (bug), got %d", m.typeIdx)
	}
}

func TestIntentAddModel_TitleStep(t *testing.T) {
	ctx := context.Background()
	svc := mockConceptService{}

	m := NewIntentAddModel(ctx, svc, AddOptions{})

	// Type a title
	for _, char := range "Test Intent" {
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{char}}
		var model tea.Model
		model, _ = m.Update(msg)
		m = model.(IntentAddModel)
	}

	// Press Enter to advance
	msg := tea.KeyMsg{Type: tea.KeyEnter}
	model, _ := m.Update(msg)
	m = model.(IntentAddModel)

	if m.step != addStepType {
		t.Errorf("Expected step to be addStepType after entering title, got %v", m.step)
	}
}

func TestIntentAddModel_EmptyTitleNoAdvance(t *testing.T) {
	ctx := context.Background()
	svc := mockConceptService{}

	m := NewIntentAddModel(ctx, svc, AddOptions{})

	// Try to advance without a title
	msg := tea.KeyMsg{Type: tea.KeyEnter}
	model, _ := m.Update(msg)
	m = model.(IntentAddModel)

	// Should stay on title step
	if m.step != addStepTitle {
		t.Errorf("Expected to stay on addStepTitle with empty title, got %v", m.step)
	}
}

func TestIntentAddModel_TypeSelection(t *testing.T) {
	ctx := context.Background()
	svc := mockConceptService{
		concepts: []concept.Concept{
			{Name: "Projects", Path: "projects", HasItems: true},
		},
	}

	m := NewIntentAddModel(ctx, svc, AddOptions{})

	// Set title first
	for _, char := range "Test" {
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{char}}
		model, _ := m.Update(msg)
		m = model.(IntentAddModel)
	}
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(IntentAddModel)

	// Now on type step, navigate down
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = model.(IntentAddModel)

	if m.typeIdx != 1 {
		t.Errorf("Expected typeIdx 1 after down, got %d", m.typeIdx)
	}

	// Navigate with j
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = model.(IntentAddModel)

	if m.typeIdx != 2 {
		t.Errorf("Expected typeIdx 2 after j, got %d", m.typeIdx)
	}

	// Navigate up with k
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = model.(IntentAddModel)

	if m.typeIdx != 1 {
		t.Errorf("Expected typeIdx 1 after k, got %d", m.typeIdx)
	}
}

func TestIntentAddModel_CancelOnEsc(t *testing.T) {
	ctx := context.Background()
	svc := mockConceptService{}

	m := NewIntentAddModel(ctx, svc, AddOptions{})

	// Press Esc to cancel
	msg := tea.KeyMsg{Type: tea.KeyEsc}
	model, _ := m.Update(msg)
	m = model.(IntentAddModel)

	if !m.Cancelled() {
		t.Error("Model should be cancelled after Esc")
	}
	if !m.Done() {
		t.Error("Model should be done after cancel")
	}
}

func TestIntentAddModel_FullModeIncludesBody(t *testing.T) {
	ctx := context.Background()
	svc := mockConceptService{}

	m := NewIntentAddModel(ctx, svc, AddOptions{
		FullMode: true,
	})

	if !m.fullMode {
		t.Error("fullMode should be true when FullMode option is set")
	}
}

func TestIntentAddModel_ViewRendersCorrectly(t *testing.T) {
	ctx := context.Background()
	svc := mockConceptService{}

	m := NewIntentAddModel(ctx, svc, AddOptions{})

	// Should render without panic
	view := m.View()
	if view == "" {
		t.Error("View should not be empty")
	}
	if !containsText(view, "Create Intent") {
		t.Error("View should contain 'Create Intent' title")
	}
	if !containsText(view, "Title") {
		t.Error("View should contain 'Title' prompt")
	}
}

func TestIntentAddModel_ResultIsNilWhenCancelled(t *testing.T) {
	ctx := context.Background()
	svc := mockConceptService{}

	m := NewIntentAddModel(ctx, svc, AddOptions{})

	// Cancel
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = model.(IntentAddModel)

	if m.Result() != nil {
		t.Error("Result should be nil when cancelled")
	}
}

func TestIntentAddModel_WindowSizeMsg(t *testing.T) {
	ctx := context.Background()
	svc := mockConceptService{}

	m := NewIntentAddModel(ctx, svc, AddOptions{})

	// Send window size
	model, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = model.(IntentAddModel)

	if m.width != 80 {
		t.Errorf("Expected width 80, got %d", m.width)
	}
	if m.height != 24 {
		t.Errorf("Expected height 24, got %d", m.height)
	}
}

func TestIntentAddModel_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	svc := mockConceptService{}

	m := NewIntentAddModel(ctx, svc, AddOptions{})

	// Cancel context
	cancel()

	// Next update should detect cancellation
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = model.(IntentAddModel)

	if !m.Cancelled() {
		t.Error("Model should be cancelled when context is cancelled")
	}
	if !m.Done() {
		t.Error("Model should be done when context is cancelled")
	}
}

func TestIntentAddModel_BodyCtrlS(t *testing.T) {
	ctx := context.Background()
	svc := mockConceptService{
		concepts: []concept.Concept{
			{Name: "p", Path: "projects", HasItems: false},
		},
	}

	m := NewIntentAddModel(ctx, svc, AddOptions{})

	// Navigate to body step: Title -> Type -> Concept -> Body
	// Title
	for _, char := range "Test" {
		model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{char}})
		m = model.(IntentAddModel)
	}
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(IntentAddModel)
	// Type
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(IntentAddModel)
	// Concept (select p which has no items, goes to body)
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(IntentAddModel)

	if m.step != addStepBody {
		t.Fatalf("Expected to be at body step, got %v", m.step)
	}

	// Type some body text
	for _, char := range "Body content" {
		model, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{char}})
		m = model.(IntentAddModel)
	}

	// Press Ctrl+S to save
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = model.(IntentAddModel)

	if !m.Done() {
		t.Error("Model should be done after Ctrl+S")
	}
	if m.Cancelled() {
		t.Error("Model should not be cancelled")
	}
	if m.Result() == nil {
		t.Error("Result should not be nil")
	}
}

func TestIntentAddModel_VimWQ(t *testing.T) {
	ctx := context.Background()
	svc := mockConceptService{
		concepts: []concept.Concept{
			{Name: "p", Path: "projects", HasItems: false},
		},
	}

	m := NewIntentAddModel(ctx, svc, AddOptions{})

	// Navigate to body step
	for _, char := range "Test" {
		model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{char}})
		m = model.(IntentAddModel)
	}
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(IntentAddModel)
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(IntentAddModel)
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(IntentAddModel)

	// Exit insert mode first (body step starts in insert mode)
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = model.(IntentAddModel)

	if m.vimEditor.Mode() == vim.ModeInsert {
		t.Error("Should be in normal mode after Esc")
	}

	// Type :wq
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	m = model.(IntentAddModel)

	if !m.vimEditor.IsCommandMode() {
		t.Error("Should be in vim command mode after :")
	}

	for _, char := range "wq" {
		model, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{char}})
		m = model.(IntentAddModel)
	}

	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(IntentAddModel)

	if !m.Done() {
		t.Error("Model should be done after :wq")
	}
	if m.Cancelled() {
		t.Error("Model should not be cancelled after :wq")
	}
}

func TestIntentAddModel_VimQBang(t *testing.T) {
	ctx := context.Background()
	svc := mockConceptService{
		concepts: []concept.Concept{
			{Name: "p", Path: "projects", HasItems: false},
		},
	}

	m := NewIntentAddModel(ctx, svc, AddOptions{})

	// Navigate to body step
	for _, char := range "Test" {
		model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{char}})
		m = model.(IntentAddModel)
	}
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(IntentAddModel)
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(IntentAddModel)
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(IntentAddModel)

	// Exit insert mode first (body step starts in insert mode)
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = model.(IntentAddModel)

	// Type :q!
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	m = model.(IntentAddModel)
	for _, char := range "q!" {
		model, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{char}})
		m = model.(IntentAddModel)
	}
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(IntentAddModel)

	if !m.Done() {
		t.Error("Model should be done after :q!")
	}
	if !m.Cancelled() {
		t.Error("Model should be cancelled after :q!")
	}
}

func TestIntentAddModel_VimEscCancelsCommand(t *testing.T) {
	ctx := context.Background()
	svc := mockConceptService{
		concepts: []concept.Concept{
			{Name: "p", Path: "projects", HasItems: false},
		},
	}

	m := NewIntentAddModel(ctx, svc, AddOptions{})

	// Navigate to body step
	for _, char := range "Test" {
		model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{char}})
		m = model.(IntentAddModel)
	}
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(IntentAddModel)
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(IntentAddModel)
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(IntentAddModel)

	// Exit insert mode first (body step starts in insert mode)
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = model.(IntentAddModel)

	// Enter vim command mode
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	m = model.(IntentAddModel)

	if !m.vimEditor.IsCommandMode() {
		t.Error("Should be in vim command mode")
	}

	// Press Esc to cancel command mode
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = model.(IntentAddModel)

	if m.vimEditor.IsCommandMode() {
		t.Error("Should exit vim command mode after Esc")
	}
	if m.Done() {
		t.Error("Should not be done - Esc in command mode just exits command mode")
	}
}
