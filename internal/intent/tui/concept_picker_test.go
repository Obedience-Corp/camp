package tui

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/Obedience-Corp/camp/internal/concept"
)

// mockConceptService is a test implementation of concept.Service.
type mockConceptService struct {
	concepts []concept.Concept
	items    map[string][]concept.Item
}

// Helper for creating depth pointers in tests
func intPtr(i int) *int { return &i }

func (m mockConceptService) List(ctx context.Context) ([]concept.Concept, error) {
	return m.concepts, nil
}

func (m mockConceptService) ListItems(ctx context.Context, conceptName, subpath string) ([]concept.Item, error) {
	key := conceptName + ":" + subpath
	if items, ok := m.items[key]; ok {
		return items, nil
	}
	return nil, nil
}

func (m mockConceptService) Resolve(ctx context.Context, conceptName, item string) (string, error) {
	return "", nil
}

func (m mockConceptService) ResolvePath(ctx context.Context, path string) (*concept.Item, error) {
	return nil, nil
}

func (m mockConceptService) ConceptForPath(ctx context.Context, path string) (*concept.Concept, error) {
	return nil, nil
}

func (m mockConceptService) Get(ctx context.Context, name string) (*concept.Concept, error) {
	for _, c := range m.concepts {
		if c.Name == name {
			return &c, nil
		}
	}
	return nil, nil
}

func TestConceptPicker_InitialState(t *testing.T) {
	svc := mockConceptService{
		concepts: []concept.Concept{
			{Name: "p", Path: "projects", Description: "Projects", HasItems: true},
			{Name: "f", Path: "festivals", Description: "Festivals", HasItems: true},
		},
	}

	picker := NewConceptPickerModel(context.Background(), svc)

	if picker.step != stepSelectingType {
		t.Errorf("Expected initial step to be stepSelectingType, got %v", picker.step)
	}

	if len(picker.concepts) != 2 {
		t.Errorf("Expected 2 concepts, got %d", len(picker.concepts))
	}

	// Initial selection should be 0 (NONE option)
	if picker.typeWheel.Selected() != 0 {
		t.Errorf("Expected initial selection to be 0 (NONE), got %d", picker.typeWheel.Selected())
	}

	if picker.Done() {
		t.Error("Picker should not be done initially")
	}

	if picker.Cancelled() {
		t.Error("Picker should not be cancelled initially")
	}
}

func TestConceptPicker_SelectNone(t *testing.T) {
	svc := mockConceptService{
		concepts: []concept.Concept{
			{Name: "p", Path: "projects", Description: "Projects", HasItems: true},
		},
	}

	picker := NewConceptPickerModel(context.Background(), svc)

	// Initial selection is 0 (NONE), press enter
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if picker.step != stepDone {
		t.Errorf("Expected step to be stepDone after selecting NONE, got %v", picker.step)
	}

	if picker.selectedPath != "" {
		t.Errorf("Expected empty selectedPath for NONE, got %q", picker.selectedPath)
	}

	if picker.Cancelled() {
		t.Error("Picker should not be cancelled when NONE is selected")
	}

	if picker.selectedConcept != nil {
		t.Error("Expected selectedConcept to be nil when NONE is selected")
	}
}

func TestConceptPicker_NewProjectOption(t *testing.T) {
	svc := mockConceptService{
		concepts: []concept.Concept{
			{Name: "p", Path: "projects", Description: "Projects", HasItems: true},
		},
		items: map[string][]concept.Item{
			"p:": {
				{Name: "camp", Path: "projects/camp", IsDir: true, Children: 1},
			},
		},
	}

	picker := NewConceptPickerModel(context.Background(), svc)

	// Navigate to projects concept (skip NONE)
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Should have 2 items: "New" option + "camp"
	if len(picker.items) != 2 {
		t.Errorf("Expected 2 items (New + camp), got %d", len(picker.items))
	}

	// First item should be the "New" option
	if picker.items[0].Name != "+ New Project" {
		t.Errorf("Expected first item to be '+ New Project', got %q", picker.items[0].Name)
	}

	if picker.items[0].Path != "projects/new" {
		t.Errorf("Expected first item path to be 'projects/new', got %q", picker.items[0].Path)
	}

	// Select the "New" option (it's already selected as first item)
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if picker.step != stepDone {
		t.Errorf("Expected step to be stepDone after selecting New, got %v", picker.step)
	}

	if picker.selectedPath != "projects/new" {
		t.Errorf("Expected selectedPath to be 'projects/new', got %q", picker.selectedPath)
	}
}

func TestConceptPicker_TypeSelectionNavigation(t *testing.T) {
	svc := mockConceptService{
		concepts: []concept.Concept{
			{Name: "p", Path: "projects", Description: "Projects", HasItems: true},
			{Name: "f", Path: "festivals", Description: "Festivals", HasItems: true},
		},
	}

	picker := NewConceptPickerModel(context.Background(), svc)

	// Initial selection should be 0 (NONE)
	if picker.typeWheel.Selected() != 0 {
		t.Errorf("Expected initial selection 0, got %d", picker.typeWheel.Selected())
	}

	// Navigate down to first concept (index 1)
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if picker.typeWheel.Selected() != 1 {
		t.Errorf("Expected selection 1 after down, got %d", picker.typeWheel.Selected())
	}

	// Navigate down to second concept (index 2)
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if picker.typeWheel.Selected() != 2 {
		t.Errorf("Expected selection 2 after second down, got %d", picker.typeWheel.Selected())
	}

	// Navigate up back to first concept
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	if picker.typeWheel.Selected() != 1 {
		t.Errorf("Expected selection 1 after up, got %d", picker.typeWheel.Selected())
	}
}

func TestConceptPicker_SelectConceptType(t *testing.T) {
	// Use a non-"p" concept to avoid the "New" item injection
	svc := mockConceptService{
		concepts: []concept.Concept{
			{Name: "docs", Path: "docs", Description: "Documentation", HasItems: true},
		},
		items: map[string][]concept.Item{
			"docs:": {
				{Name: "api", Path: "docs/api", IsDir: true, Children: 1},
			},
		},
	}

	picker := NewConceptPickerModel(context.Background(), svc)

	// Navigate past NONE to the first concept
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})

	// Select the concept
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if picker.step != stepSelectingItem {
		t.Errorf("Expected step to be stepSelectingItem after Enter, got %v", picker.step)
	}

	if picker.selectedConcept == nil {
		t.Error("Expected selectedConcept to be set")
	} else if picker.selectedConcept.Name != "docs" {
		t.Errorf("Expected selectedConcept.Name to be 'docs', got %q", picker.selectedConcept.Name)
	}

	if len(picker.items) != 1 {
		t.Errorf("Expected 1 item to be loaded, got %d", len(picker.items))
	}
}

func TestConceptPicker_SelectConceptWithoutItems(t *testing.T) {
	svc := mockConceptService{
		concepts: []concept.Concept{
			{Name: "p", Path: "projects", Description: "Projects", HasItems: false},
		},
	}

	picker := NewConceptPickerModel(context.Background(), svc)

	// Navigate past NONE to the concept
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})

	// Select the concept
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Should complete immediately since concept has no items
	if picker.step != stepDone {
		t.Errorf("Expected step to be stepDone for concept without items, got %v", picker.step)
	}

	if picker.selectedPath != "projects" {
		t.Errorf("Expected selectedPath to be 'projects', got %q", picker.selectedPath)
	}
}

func TestConceptPicker_DrillIntoDirectory(t *testing.T) {
	// Use MaxDepth so Enter auto-drills (infinite depth would require right/l to drill)
	// Use non-"p" concept to avoid "New" item injection
	svc := mockConceptService{
		concepts: []concept.Concept{
			{Name: "docs", Path: "docs", Description: "Documentation", HasItems: true, MaxDepth: intPtr(5)},
		},
		items: map[string][]concept.Item{
			"docs:": {
				{Name: "api", Path: "docs/api", IsDir: true, Children: 2},
			},
			"docs:api": {
				{Name: "internal", Path: "docs/api/internal", IsDir: true, Children: 1},
				{Name: "main.go", Path: "docs/api/main.go", IsDir: false},
			},
		},
	}

	picker := NewConceptPickerModel(context.Background(), svc)

	// Navigate past NONE to select concept type
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Drill into api directory
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if picker.currentSubpath != "api" {
		t.Errorf("Expected currentSubpath to be 'api', got %q", picker.currentSubpath)
	}

	if len(picker.pathHistory) != 1 {
		t.Errorf("Expected pathHistory length 1, got %d", len(picker.pathHistory))
	}

	if len(picker.items) != 2 {
		t.Errorf("Expected 2 items at api level, got %d", len(picker.items))
	}
}

func TestConceptPicker_SelectFile(t *testing.T) {
	// Use non-"p" concept to avoid "New" item injection
	svc := mockConceptService{
		concepts: []concept.Concept{
			{Name: "docs", Path: "docs", Description: "Documentation", HasItems: true},
		},
		items: map[string][]concept.Item{
			"docs:": {
				{Name: "README.md", Path: "docs/README.md", IsDir: false},
			},
		},
	}

	picker := NewConceptPickerModel(context.Background(), svc)

	// Navigate past NONE to select concept type
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Select file
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if picker.step != stepDone {
		t.Errorf("Expected step to be stepDone after selecting file, got %v", picker.step)
	}

	if picker.selectedPath != "docs/README.md" {
		t.Errorf("Expected selectedPath to be 'docs/README.md', got %q", picker.selectedPath)
	}

	if picker.Cancelled() {
		t.Error("Picker should not be cancelled when file is selected")
	}
}

func TestConceptPicker_BackspaceFromNested(t *testing.T) {
	// Use MaxDepth so Enter auto-drills (infinite depth would require right/l to drill)
	// Use non-"p" concept to avoid "New" item injection
	svc := mockConceptService{
		concepts: []concept.Concept{
			{Name: "docs", Path: "docs", Description: "Documentation", HasItems: true, MaxDepth: intPtr(5)},
		},
		items: map[string][]concept.Item{
			"docs:": {
				{Name: "api", Path: "docs/api", IsDir: true, Children: 1},
			},
			"docs:api": {
				{Name: "internal", Path: "docs/api/internal", IsDir: true, Children: 1},
			},
			"docs:api/internal": {
				{Name: "file.go", Path: "docs/api/internal/file.go", IsDir: false},
			},
		},
	}

	picker := NewConceptPickerModel(context.Background(), svc)

	// Navigate to docs/api/internal
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}) // Skip NONE
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEnter})                     // Select concept
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEnter})                     // Drill into api
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEnter})                     // Drill into internal

	if picker.currentSubpath != "api/internal" {
		t.Errorf("Expected currentSubpath to be 'api/internal', got %q", picker.currentSubpath)
	}

	// Backspace to go back to api
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyBackspace})

	if picker.currentSubpath != "api" {
		t.Errorf("Expected currentSubpath to be 'api' after backspace, got %q", picker.currentSubpath)
	}

	// Backspace to go back to root
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyBackspace})

	if picker.currentSubpath != "" {
		t.Errorf("Expected currentSubpath to be empty after second backspace, got %q", picker.currentSubpath)
	}

	// Backspace to go back to type selection
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyBackspace})

	if picker.step != stepSelectingType {
		t.Errorf("Expected step to be stepSelectingType after third backspace, got %v", picker.step)
	}
}

func TestConceptPicker_CancelFromTypeSelection(t *testing.T) {
	svc := mockConceptService{
		concepts: []concept.Concept{
			{Name: "p", Path: "projects", Description: "Projects", HasItems: true},
		},
	}

	picker := NewConceptPickerModel(context.Background(), svc)

	// Press Escape
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if picker.step != stepDone {
		t.Errorf("Expected step to be stepDone after Esc, got %v", picker.step)
	}

	if !picker.Cancelled() {
		t.Error("Picker should be cancelled after Esc")
	}
}

func TestConceptPicker_CancelFromItemSelection(t *testing.T) {
	svc := mockConceptService{
		concepts: []concept.Concept{
			{Name: "p", Path: "projects", Description: "Projects", HasItems: true},
		},
		items: map[string][]concept.Item{
			"p:": {
				{Name: "camp", Path: "projects/camp", IsDir: true, Children: 1},
			},
		},
	}

	picker := NewConceptPickerModel(context.Background(), svc)

	// Navigate to item selection (skip NONE first)
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Press Escape
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if picker.step != stepDone {
		t.Errorf("Expected step to be stepDone after Esc, got %v", picker.step)
	}

	if !picker.Cancelled() {
		t.Error("Picker should be cancelled after Esc")
	}
}

func TestConceptPicker_LeftArrowNavigation(t *testing.T) {
	svc := mockConceptService{
		concepts: []concept.Concept{
			{Name: "p", Path: "projects", Description: "Projects", HasItems: true},
		},
		items: map[string][]concept.Item{
			"p:": {
				{Name: "camp", Path: "projects/camp", IsDir: true, Children: 1},
			},
		},
	}

	picker := NewConceptPickerModel(context.Background(), svc)

	// Navigate to item selection (skip NONE first)
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Use left arrow to go back
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyLeft})

	if picker.step != stepSelectingType {
		t.Errorf("Expected step to be stepSelectingType after left arrow, got %v", picker.step)
	}
}

func TestConceptPicker_HKeyNavigation(t *testing.T) {
	svc := mockConceptService{
		concepts: []concept.Concept{
			{Name: "p", Path: "projects", Description: "Projects", HasItems: true},
		},
		items: map[string][]concept.Item{
			"p:": {
				{Name: "camp", Path: "projects/camp", IsDir: true, Children: 1},
			},
		},
	}

	picker := NewConceptPickerModel(context.Background(), svc)

	// Navigate to item selection (skip NONE first)
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Use 'h' to go back (vim style)
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})

	if picker.step != stepSelectingType {
		t.Errorf("Expected step to be stepSelectingType after 'h', got %v", picker.step)
	}
}

func TestConceptPicker_EmptyDirectory(t *testing.T) {
	// Use non-"p" concept to avoid "New" item injection
	svc := mockConceptService{
		concepts: []concept.Concept{
			{Name: "docs", Path: "docs", Description: "Documentation", HasItems: true},
		},
		items: map[string][]concept.Item{
			"docs:": {
				{Name: "empty", Path: "docs/empty", IsDir: true, Children: 0},
			},
		},
	}

	picker := NewConceptPickerModel(context.Background(), svc)

	// Navigate to item selection (skip NONE first)
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// The empty directory should show but not be drillable
	if len(picker.items) != 1 {
		t.Errorf("Expected 1 item, got %d", len(picker.items))
	}

	// Pressing enter on empty directory should select it, not drill
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if picker.step != stepDone {
		t.Errorf("Expected step to be stepDone after selecting empty dir, got %v", picker.step)
	}
}

func TestConceptPicker_ViewRendersCorrectly(t *testing.T) {
	svc := mockConceptService{
		concepts: []concept.Concept{
			{Name: "p", Path: "projects", Description: "Projects", HasItems: true},
		},
	}

	picker := NewConceptPickerModel(context.Background(), svc)

	view := picker.View()
	if view == "" {
		t.Error("View should not be empty")
	}

	// View should contain "Select concept type"
	if !containsText(view, "Select concept type") {
		t.Error("Type selection view should contain 'Select concept type'")
	}
}

func TestConceptPicker_InfiniteDepthDrillWithRightKey(t *testing.T) {
	// Infinite depth (MaxDepth nil): Enter selects, right/l drills
	svc := mockConceptService{
		concepts: []concept.Concept{
			{Name: "f", Path: "festivals", Description: "Festivals", HasItems: true}, // No MaxDepth = infinite
		},
		items: map[string][]concept.Item{
			"f:": {
				{Name: "active", Path: "festivals/active", IsDir: true, Children: 1},
			},
			"f:active": {
				{Name: "my-festival", Path: "festivals/active/my-festival", IsDir: true, Children: 0},
			},
		},
	}

	picker := NewConceptPickerModel(context.Background(), svc)

	// Select concept type (skip NONE first)
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// With infinite depth, Enter should SELECT, not drill
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if picker.step != stepDone {
		t.Errorf("Expected Enter to select (stepDone) with infinite depth, got step %v", picker.step)
	}

	if picker.selectedPath != "festivals/active" {
		t.Errorf("Expected selectedPath 'festivals/active', got %q", picker.selectedPath)
	}
}

func TestConceptPicker_InfiniteDepthDrillWithLKey(t *testing.T) {
	// Infinite depth (MaxDepth nil): right/l key drills
	svc := mockConceptService{
		concepts: []concept.Concept{
			{Name: "f", Path: "festivals", Description: "Festivals", HasItems: true}, // No MaxDepth = infinite
		},
		items: map[string][]concept.Item{
			"f:": {
				{Name: "active", Path: "festivals/active", IsDir: true, Children: 1},
			},
			"f:active": {
				{Name: "my-festival", Path: "festivals/active/my-festival", IsDir: true, Children: 0},
			},
		},
	}

	picker := NewConceptPickerModel(context.Background(), svc)

	// Select concept type (skip NONE first)
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Use 'l' key to drill (vim style)
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})

	if picker.step != stepSelectingItem {
		t.Errorf("Expected 'l' to drill (stay in stepSelectingItem), got step %v", picker.step)
	}

	if picker.currentSubpath != "active" {
		t.Errorf("Expected currentSubpath 'active' after drill, got %q", picker.currentSubpath)
	}
}

func TestConceptPicker_Breadcrumb(t *testing.T) {
	// Use MaxDepth so Enter auto-drills (infinite depth would require right/l to drill)
	// Use non-"p" concept to avoid "New" item injection
	svc := mockConceptService{
		concepts: []concept.Concept{
			{Name: "docs", Path: "docs", Description: "Documentation", HasItems: true, MaxDepth: intPtr(5)},
		},
		items: map[string][]concept.Item{
			"docs:": {
				{Name: "api", Path: "docs/api", IsDir: true, Children: 1},
			},
			"docs:api": {
				{Name: "internal", Path: "docs/api/internal", IsDir: true, Children: 1},
			},
		},
	}

	picker := NewConceptPickerModel(context.Background(), svc)

	// Navigate to docs/api (skip NONE first)
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}) // Skip NONE
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEnter})                     // Select concept
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEnter})                     // Drill into api

	breadcrumb := picker.buildBreadcrumb()
	expected := "docs > api"
	if breadcrumb != expected {
		t.Errorf("Expected breadcrumb %q, got %q", expected, breadcrumb)
	}
}

func containsText(view, text string) bool {
	return len(view) >= len(text) && (view == text ||
		len(view) > len(text) &&
			(view[:len(text)] == text ||
				view[len(view)-len(text):] == text ||
				findSubstring(view, text)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
