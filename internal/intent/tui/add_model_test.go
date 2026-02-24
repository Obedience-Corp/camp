package tui

import (
	"context"
	"os"
	"path/filepath"
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

func TestIntentAddModel_CtrlN_SaveAndNew_AtTitle(t *testing.T) {
	ctx := context.Background()
	svc := mockConceptService{}

	m := NewIntentAddModel(ctx, svc, AddOptions{DefaultType: "idea"})

	// Type a title
	for _, char := range "Quick idea" {
		model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{char}})
		m = model.(IntentAddModel)
	}

	// Press Ctrl+N to save and start new
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	m = model.(IntentAddModel)

	// Should have one saved result
	if len(m.SavedResults()) != 1 {
		t.Fatalf("Expected 1 saved result, got %d", len(m.SavedResults()))
	}

	saved := m.SavedResults()[0]
	if saved.Title != "Quick idea" {
		t.Errorf("Saved title = %q, want %q", saved.Title, "Quick idea")
	}
	if saved.Type != "idea" {
		t.Errorf("Saved type = %q, want %q", saved.Type, "idea")
	}

	// Should be back on title step with empty input
	if m.step != addStepTitle {
		t.Errorf("Expected to be on title step, got %v", m.step)
	}
	if m.titleInput.Value() != "" {
		t.Errorf("Title input should be empty after reset, got %q", m.titleInput.Value())
	}
	if m.savedCount != 1 {
		t.Errorf("savedCount = %d, want 1", m.savedCount)
	}
}

func TestIntentAddModel_CtrlN_EmptyTitle_NoSave(t *testing.T) {
	ctx := context.Background()
	svc := mockConceptService{}

	m := NewIntentAddModel(ctx, svc, AddOptions{})

	// Press Ctrl+N without typing anything
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	m = model.(IntentAddModel)

	// Should have no saved results
	if len(m.SavedResults()) != 0 {
		t.Errorf("Expected 0 saved results, got %d", len(m.SavedResults()))
	}
}

func TestIntentAddModel_CtrlN_AtTypeStep(t *testing.T) {
	ctx := context.Background()
	svc := mockConceptService{}

	m := NewIntentAddModel(ctx, svc, AddOptions{})

	// Enter title, advance to type step
	for _, char := range "My feature" {
		model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{char}})
		m = model.(IntentAddModel)
	}
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(IntentAddModel)

	// Navigate to "feature" (index 1)
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = model.(IntentAddModel)

	// Press Ctrl+N
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	m = model.(IntentAddModel)

	if len(m.SavedResults()) != 1 {
		t.Fatalf("Expected 1 saved result, got %d", len(m.SavedResults()))
	}

	saved := m.SavedResults()[0]
	if saved.Title != "My feature" {
		t.Errorf("Saved title = %q, want %q", saved.Title, "My feature")
	}
	if saved.Type != "feature" {
		t.Errorf("Saved type = %q, want %q", saved.Type, "feature")
	}

	// Should be back on title step
	if m.step != addStepTitle {
		t.Errorf("Expected to be on title step, got %v", m.step)
	}
}

func TestIntentAddModel_CtrlN_AtBodyStep(t *testing.T) {
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

	if m.step != addStepBody {
		t.Fatalf("Expected body step, got %v", m.step)
	}

	// Type some body content (in insert mode)
	for _, char := range "Body text" {
		model, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{char}})
		m = model.(IntentAddModel)
	}

	// Press Ctrl+N
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	m = model.(IntentAddModel)

	if len(m.SavedResults()) != 1 {
		t.Fatalf("Expected 1 saved result, got %d", len(m.SavedResults()))
	}

	saved := m.SavedResults()[0]
	if saved.Title != "Test" {
		t.Errorf("Saved title = %q, want %q", saved.Title, "Test")
	}
	if saved.Body != "Body text" {
		t.Errorf("Saved body = %q, want %q", saved.Body, "Body text")
	}

	// Should be back on title step
	if m.step != addStepTitle {
		t.Errorf("Expected to be on title step, got %v", m.step)
	}
}

func TestIntentAddModel_CtrlN_MultipleRapidFire(t *testing.T) {
	ctx := context.Background()
	svc := mockConceptService{}

	m := NewIntentAddModel(ctx, svc, AddOptions{DefaultType: "idea"})

	// Rapid-fire: type title, Ctrl-N, type another, Ctrl-N, type third, Esc
	titles := []string{"First idea", "Second idea", "Third idea"}

	for i, title := range titles[:2] {
		for _, char := range title {
			model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{char}})
			m = model.(IntentAddModel)
		}
		model, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
		m = model.(IntentAddModel)

		if len(m.SavedResults()) != i+1 {
			t.Fatalf("After save %d: expected %d saved results, got %d", i+1, i+1, len(m.SavedResults()))
		}
	}

	// Type third title then cancel
	for _, char := range titles[2] {
		model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{char}})
		m = model.(IntentAddModel)
	}
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = model.(IntentAddModel)

	// Should have 2 saved results (third was cancelled, not saved)
	if len(m.SavedResults()) != 2 {
		t.Errorf("Expected 2 saved results, got %d", len(m.SavedResults()))
	}

	// Verify saved titles
	for i, expected := range titles[:2] {
		if m.SavedResults()[i].Title != expected {
			t.Errorf("SavedResults[%d].Title = %q, want %q", i, m.SavedResults()[i].Title, expected)
		}
	}

	// savedCount should be 2
	if m.savedCount != 2 {
		t.Errorf("savedCount = %d, want 2", m.savedCount)
	}
}

func TestIntentAddModel_CtrlN_ViewShowsSavedCount(t *testing.T) {
	ctx := context.Background()
	svc := mockConceptService{}

	m := NewIntentAddModel(ctx, svc, AddOptions{})

	// Initially no saved count shown
	view := m.View()
	if containsText(view, "saved") {
		t.Error("View should not show saved count initially")
	}

	// Save one intent
	for _, char := range "Test" {
		model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{char}})
		m = model.(IntentAddModel)
	}
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	m = model.(IntentAddModel)

	// Now view should show saved count
	view = m.View()
	if !containsText(view, "1 saved") {
		t.Error("View should show '1 saved' after first Ctrl-N save")
	}
}

func TestIntentAddModel_TitleCompletion_AtTrigger(t *testing.T) {
	// Create a campaign directory with projects
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "projects", "fest"), 0755)
	os.MkdirAll(filepath.Join(root, "projects", "camp"), 0755)

	ctx := context.Background()
	svc := mockConceptService{}

	shortcuts := map[string]string{"p": "projects/"}
	m := NewIntentAddModel(ctx, svc, AddOptions{CampaignRoot: root, Shortcuts: shortcuts})

	// Type "@p/" — auto-expand replaces @p/ with @projects/
	for _, char := range "@p/" {
		model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{char}})
		m = model.(IntentAddModel)
	}

	// Completion should be active (expanded to @projects/, listing directory)
	if !m.completion.active {
		t.Fatal("Expected completion to be active after typing @p/")
	}
	if len(m.completion.candidates) < 2 {
		t.Errorf("Expected at least 2 candidates, got %d", len(m.completion.candidates))
	}
}

func TestIntentAddModel_TitleCompletion_NoCampaignRoot(t *testing.T) {
	ctx := context.Background()
	svc := mockConceptService{}

	// No campaign root = no completion
	m := NewIntentAddModel(ctx, svc, AddOptions{})

	for _, char := range "@p/" {
		model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{char}})
		m = model.(IntentAddModel)
	}

	if m.completion.active {
		t.Error("Completion should not be active without CampaignRoot")
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
