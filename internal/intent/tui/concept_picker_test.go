package tui

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/obediencecorp/camp/internal/concept"
)

// mockConceptService is a test implementation of concept.Service.
type mockConceptService struct {
	concepts []concept.Concept
	items    map[string][]concept.Item
}

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

	if picker.Done() {
		t.Error("Picker should not be done initially")
	}

	if picker.Cancelled() {
		t.Error("Picker should not be cancelled initially")
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

	// Initial selection should be 0
	if picker.typeWheel.Selected() != 0 {
		t.Errorf("Expected initial selection 0, got %d", picker.typeWheel.Selected())
	}

	// Navigate down
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if picker.typeWheel.Selected() != 1 {
		t.Errorf("Expected selection 1 after down, got %d", picker.typeWheel.Selected())
	}

	// Navigate up
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	if picker.typeWheel.Selected() != 0 {
		t.Errorf("Expected selection 0 after up, got %d", picker.typeWheel.Selected())
	}
}

func TestConceptPicker_SelectConceptType(t *testing.T) {
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

	// Select the concept
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if picker.step != stepSelectingItem {
		t.Errorf("Expected step to be stepSelectingItem after Enter, got %v", picker.step)
	}

	if picker.selectedConcept == nil {
		t.Error("Expected selectedConcept to be set")
	} else if picker.selectedConcept.Name != "p" {
		t.Errorf("Expected selectedConcept.Name to be 'p', got %q", picker.selectedConcept.Name)
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
	svc := mockConceptService{
		concepts: []concept.Concept{
			{Name: "p", Path: "projects", Description: "Projects", HasItems: true},
		},
		items: map[string][]concept.Item{
			"p:": {
				{Name: "camp", Path: "projects/camp", IsDir: true, Children: 2},
			},
			"p:camp": {
				{Name: "internal", Path: "projects/camp/internal", IsDir: true, Children: 1},
				{Name: "main.go", Path: "projects/camp/main.go", IsDir: false},
			},
		},
	}

	picker := NewConceptPickerModel(context.Background(), svc)

	// Select concept type
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Drill into camp directory
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if picker.currentSubpath != "camp" {
		t.Errorf("Expected currentSubpath to be 'camp', got %q", picker.currentSubpath)
	}

	if len(picker.pathHistory) != 1 {
		t.Errorf("Expected pathHistory length 1, got %d", len(picker.pathHistory))
	}

	if len(picker.items) != 2 {
		t.Errorf("Expected 2 items at camp level, got %d", len(picker.items))
	}
}

func TestConceptPicker_SelectFile(t *testing.T) {
	svc := mockConceptService{
		concepts: []concept.Concept{
			{Name: "p", Path: "projects", Description: "Projects", HasItems: true},
		},
		items: map[string][]concept.Item{
			"p:": {
				{Name: "README.md", Path: "projects/README.md", IsDir: false},
			},
		},
	}

	picker := NewConceptPickerModel(context.Background(), svc)

	// Select concept type
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Select file
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if picker.step != stepDone {
		t.Errorf("Expected step to be stepDone after selecting file, got %v", picker.step)
	}

	if picker.selectedPath != "projects/README.md" {
		t.Errorf("Expected selectedPath to be 'projects/README.md', got %q", picker.selectedPath)
	}

	if picker.Cancelled() {
		t.Error("Picker should not be cancelled when file is selected")
	}
}

func TestConceptPicker_BackspaceFromNested(t *testing.T) {
	svc := mockConceptService{
		concepts: []concept.Concept{
			{Name: "p", Path: "projects", Description: "Projects", HasItems: true},
		},
		items: map[string][]concept.Item{
			"p:": {
				{Name: "camp", Path: "projects/camp", IsDir: true, Children: 1},
			},
			"p:camp": {
				{Name: "internal", Path: "projects/camp/internal", IsDir: true, Children: 1},
			},
			"p:camp/internal": {
				{Name: "file.go", Path: "projects/camp/internal/file.go", IsDir: false},
			},
		},
	}

	picker := NewConceptPickerModel(context.Background(), svc)

	// Navigate to projects/camp/internal
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEnter}) // Select concept
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEnter}) // Drill into camp
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEnter}) // Drill into internal

	if picker.currentSubpath != "camp/internal" {
		t.Errorf("Expected currentSubpath to be 'camp/internal', got %q", picker.currentSubpath)
	}

	// Backspace to go back to camp
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyBackspace})

	if picker.currentSubpath != "camp" {
		t.Errorf("Expected currentSubpath to be 'camp' after backspace, got %q", picker.currentSubpath)
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

	// Navigate to item selection
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

	// Navigate to item selection
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

	// Navigate to item selection
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Use 'h' to go back (vim style)
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})

	if picker.step != stepSelectingType {
		t.Errorf("Expected step to be stepSelectingType after 'h', got %v", picker.step)
	}
}

func TestConceptPicker_EmptyDirectory(t *testing.T) {
	svc := mockConceptService{
		concepts: []concept.Concept{
			{Name: "p", Path: "projects", Description: "Projects", HasItems: true},
		},
		items: map[string][]concept.Item{
			"p:": {
				{Name: "empty", Path: "projects/empty", IsDir: true, Children: 0},
			},
		},
	}

	picker := NewConceptPickerModel(context.Background(), svc)

	// Navigate to item selection
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

func TestConceptPicker_Breadcrumb(t *testing.T) {
	svc := mockConceptService{
		concepts: []concept.Concept{
			{Name: "p", Path: "projects", Description: "Projects", HasItems: true},
		},
		items: map[string][]concept.Item{
			"p:": {
				{Name: "camp", Path: "projects/camp", IsDir: true, Children: 1},
			},
			"p:camp": {
				{Name: "internal", Path: "projects/camp/internal", IsDir: true, Children: 1},
			},
		},
	}

	picker := NewConceptPickerModel(context.Background(), svc)

	// Navigate to projects/camp/internal
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEnter}) // Select concept
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEnter}) // Drill into camp

	breadcrumb := picker.buildBreadcrumb()
	expected := "p > camp"
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
