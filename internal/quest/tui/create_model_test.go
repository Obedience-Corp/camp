//go:build dev

package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func sendKey(m QuestCreateModel, key tea.KeyMsg) QuestCreateModel {
	model, _ := m.Update(key)
	return model.(QuestCreateModel)
}

func sendRune(m QuestCreateModel, r rune) QuestCreateModel {
	return sendKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
}

func typeText(m QuestCreateModel, text string) QuestCreateModel {
	for _, r := range text {
		m = sendRune(m, r)
	}
	return m
}

func TestQuestCreateModel_InitialState(t *testing.T) {
	m := NewQuestCreateModel(context.Background(), CreateOptions{})

	if m.step != createStepName {
		t.Errorf("Expected initial step createStepName, got %v", m.step)
	}
	if m.Cancelled() {
		t.Error("Should not be cancelled initially")
	}
	if m.Done() {
		t.Error("Should not be done initially")
	}
	if m.Result() != nil {
		t.Error("Result should be nil initially")
	}
}

func TestQuestCreateModel_DefaultValues(t *testing.T) {
	m := NewQuestCreateModel(context.Background(), CreateOptions{
		DefaultName:    "test-quest",
		DefaultPurpose: "test purpose",
		DefaultTags:    "tag1,tag2",
	})

	if m.nameInput.Value() != "test-quest" {
		t.Errorf("Expected default name %q, got %q", "test-quest", m.nameInput.Value())
	}
	if m.purposeInput.Value() != "test purpose" {
		t.Errorf("Expected default purpose %q, got %q", "test purpose", m.purposeInput.Value())
	}
	if m.tagsInput.Value() != "tag1,tag2" {
		t.Errorf("Expected default tags %q, got %q", "tag1,tag2", m.tagsInput.Value())
	}
}

func TestQuestCreateModel_NameStep_EnterAdvances(t *testing.T) {
	m := NewQuestCreateModel(context.Background(), CreateOptions{})

	m = typeText(m, "my-quest")
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.step != createStepPurpose {
		t.Errorf("Expected step createStepPurpose, got %v", m.step)
	}
}

func TestQuestCreateModel_NameStep_EmptyNoAdvance(t *testing.T) {
	m := NewQuestCreateModel(context.Background(), CreateOptions{})

	m = sendKey(m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.step != createStepName {
		t.Errorf("Expected to stay on createStepName, got %v", m.step)
	}
	if m.nameErr == "" {
		t.Error("Expected name error message")
	}
}

func TestQuestCreateModel_NameStep_EscCancels(t *testing.T) {
	m := NewQuestCreateModel(context.Background(), CreateOptions{})

	m = sendKey(m, tea.KeyMsg{Type: tea.KeyEsc})

	if !m.Cancelled() {
		t.Error("Should be cancelled after Esc")
	}
	if !m.Done() {
		t.Error("Should be done after cancel")
	}
}

func TestQuestCreateModel_PurposeStep_EnterAdvances(t *testing.T) {
	m := NewQuestCreateModel(context.Background(), CreateOptions{})

	// Name step
	m = typeText(m, "quest")
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyEnter})

	// Purpose step (empty is OK)
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.step != createStepDescription {
		t.Errorf("Expected step createStepDescription, got %v", m.step)
	}
}

func TestQuestCreateModel_PurposeStep_EscCancels(t *testing.T) {
	m := NewQuestCreateModel(context.Background(), CreateOptions{})

	m = typeText(m, "quest")
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyEsc})

	if !m.Cancelled() {
		t.Error("Should be cancelled after Esc at purpose step")
	}
}

func TestQuestCreateModel_DescriptionStep_CtrlS_AdvancesToTags(t *testing.T) {
	m := NewQuestCreateModel(context.Background(), CreateOptions{})

	// Name -> Purpose -> Description
	m = typeText(m, "quest")
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.step != createStepDescription {
		t.Fatalf("Expected description step, got %v", m.step)
	}

	// Type some description text (starts in insert mode)
	m = typeText(m, "Quest description")

	// Ctrl+S to advance
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyCtrlS})

	if m.step != createStepTags {
		t.Errorf("Expected tags step after Ctrl+S, got %v", m.step)
	}
}

func TestQuestCreateModel_DescriptionStep_Tab_SkipsToTags(t *testing.T) {
	m := NewQuestCreateModel(context.Background(), CreateOptions{})

	m = typeText(m, "quest")
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyEnter})

	// Exit insert mode first
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyEsc})

	// Tab in normal mode skips to tags
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyTab})

	if m.step != createStepTags {
		t.Errorf("Expected tags step after Tab, got %v", m.step)
	}
}

func TestQuestCreateModel_DescriptionStep_VimWQ(t *testing.T) {
	m := NewQuestCreateModel(context.Background(), CreateOptions{})

	m = typeText(m, "quest")
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyEnter})

	// Exit insert mode
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyEsc})

	// :wq
	m = sendRune(m, ':')
	m = typeText(m, "wq")
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.step != createStepTags {
		t.Errorf("Expected tags step after :wq, got %v", m.step)
	}
}

func TestQuestCreateModel_DescriptionStep_VimQBang_Cancels(t *testing.T) {
	m := NewQuestCreateModel(context.Background(), CreateOptions{})

	m = typeText(m, "quest")
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyEnter})

	// Exit insert mode
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyEsc})

	// :q!
	m = sendRune(m, ':')
	m = typeText(m, "q!")
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyEnter})

	if !m.Cancelled() {
		t.Error("Should be cancelled after :q!")
	}
}

func TestQuestCreateModel_TagsStep_EnterFinishes(t *testing.T) {
	m := NewQuestCreateModel(context.Background(), CreateOptions{})

	// Name -> Purpose -> Description (skip) -> Tags
	m = typeText(m, "my-quest")
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = typeText(m, "test purpose")
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyCtrlS}) // skip description
	m = typeText(m, "infra,reliability")
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyEnter})

	if !m.Done() {
		t.Error("Should be done after tags Enter")
	}
	if m.Cancelled() {
		t.Error("Should not be cancelled")
	}

	result := m.Result()
	if result == nil {
		t.Fatal("Result should not be nil")
	}
	if result.Name != "my-quest" {
		t.Errorf("Name = %q, want %q", result.Name, "my-quest")
	}
	if result.Purpose != "test purpose" {
		t.Errorf("Purpose = %q, want %q", result.Purpose, "test purpose")
	}
	if result.Tags != "infra,reliability" {
		t.Errorf("Tags = %q, want %q", result.Tags, "infra,reliability")
	}
}

func TestQuestCreateModel_TagsStep_EscCancels(t *testing.T) {
	m := NewQuestCreateModel(context.Background(), CreateOptions{})

	m = typeText(m, "quest")
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyEsc})

	if !m.Cancelled() {
		t.Error("Should be cancelled after Esc at tags step")
	}
}

func TestQuestCreateModel_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	m := NewQuestCreateModel(ctx, CreateOptions{})

	cancel()

	m = sendRune(m, 'a')

	if !m.Cancelled() {
		t.Error("Should be cancelled when context is cancelled")
	}
	if !m.Done() {
		t.Error("Should be done when context is cancelled")
	}
}

func TestQuestCreateModel_WindowSizeMsg(t *testing.T) {
	m := NewQuestCreateModel(context.Background(), CreateOptions{})

	model, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = model.(QuestCreateModel)

	if m.width != 80 {
		t.Errorf("Expected width 80, got %d", m.width)
	}
	if m.height != 24 {
		t.Errorf("Expected height 24, got %d", m.height)
	}
}

func TestQuestCreateModel_ViewRendersCorrectly(t *testing.T) {
	m := NewQuestCreateModel(context.Background(), CreateOptions{})

	view := m.View()
	if view == "" {
		t.Error("View should not be empty")
	}
	if !strings.Contains(view, "Create Quest") {
		t.Error("View should contain 'Create Quest' title")
	}
	if !strings.Contains(view, "Name") {
		t.Error("View should contain 'Name' prompt")
	}
}

func TestQuestCreateModel_ViewProgress(t *testing.T) {
	m := NewQuestCreateModel(context.Background(), CreateOptions{})

	// Advance past name
	m = typeText(m, "my-quest")
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyEnter})

	view := m.View()
	if !strings.Contains(view, "my-quest") {
		t.Error("View should show completed name in progress")
	}
}

func TestQuestCreateModel_ResultIsNilWhenCancelled(t *testing.T) {
	m := NewQuestCreateModel(context.Background(), CreateOptions{})

	m = sendKey(m, tea.KeyMsg{Type: tea.KeyEsc})

	if m.Result() != nil {
		t.Error("Result should be nil when cancelled")
	}
}

func TestQuestCreateModel_FullFlow(t *testing.T) {
	m := NewQuestCreateModel(context.Background(), CreateOptions{})

	// Name
	m = typeText(m, "q2-reliability")
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyEnter})

	// Purpose
	m = typeText(m, "harden platform")
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyEnter})

	// Description (type text, then Ctrl+S)
	m = typeText(m, "Full description here")
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyCtrlS})

	// Tags
	m = typeText(m, "platform,reliability")
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyEnter})

	if !m.Done() {
		t.Error("Should be done after full flow")
	}

	result := m.Result()
	if result == nil {
		t.Fatal("Result should not be nil")
	}
	if result.Name != "q2-reliability" {
		t.Errorf("Name = %q, want %q", result.Name, "q2-reliability")
	}
	if result.Purpose != "harden platform" {
		t.Errorf("Purpose = %q, want %q", result.Purpose, "harden platform")
	}
	if result.Description != "Full description here" {
		t.Errorf("Description = %q, want %q", result.Description, "Full description here")
	}
	if result.Tags != "platform,reliability" {
		t.Errorf("Tags = %q, want %q", result.Tags, "platform,reliability")
	}
}
