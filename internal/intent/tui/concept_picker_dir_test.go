package tui

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/Obedience-Corp/camp/internal/concept"
	"github.com/Obedience-Corp/camp/internal/config"
	tea "github.com/charmbracelet/bubbletea"
)

// workflowTypeService mirrors a campaign whose workflow parent has depth 1,
// a configured design child, and an ad-hoc custom workflow (blog) on disk.
func workflowTypeService() concept.Service {
	depth1 := 1
	concepts := []config.ConceptEntry{{
		Name:  "workflow",
		Path:  "workflow",
		Depth: &depth1,
		Children: []config.ConceptEntry{
			{Name: "design", Path: "workflow/design", Depth: &depth1},
		},
	}}
	fsys := fstest.MapFS{
		"workflow/design/auth-doc/README.md":  &fstest.MapFile{Data: []byte("")},
		"workflow/design/empty-doc/.keep":     &fstest.MapFile{Data: []byte("")},
		"workflow/blog/posts/2026-post.md":    &fstest.MapFile{Data: []byte("")},
		"workflow/blog/README.md":             &fstest.MapFile{Data: []byte("")},
		"workflow/blog/assets/hero/image.png": &fstest.MapFile{Data: []byte("")},
	}
	return concept.NewFSService("", concepts, fsys)
}

// openWorkflowSubmenu selects the workflow concept (type list: none, workflow).
func openWorkflowSubmenu(t *testing.T, svc concept.Service) ConceptPickerModel {
	t.Helper()
	picker := NewConceptPickerModel(context.Background(), svc, "")
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if picker.step != stepSelectingItem {
		t.Fatalf("selecting workflow should open the submenu, step = %v", picker.step)
	}
	return picker
}

func moveToID(t *testing.T, picker ConceptPickerModel, id string) ConceptPickerModel {
	t.Helper()
	for range len(picker.itemSelectorItems()) {
		if it, ok := picker.sel.Selected(); ok && it.ID == id {
			return picker
		}
		picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyUp})
	}
	t.Fatalf("item %q not reachable in picker", id)
	return picker
}

func TestConceptPicker_AdHocWorkflowSelectsDirectory(t *testing.T) {
	picker := openWorkflowSubmenu(t, workflowTypeService())
	picker = moveToID(t, picker, "workflow/blog")

	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if picker.step != stepDone {
		t.Fatalf("enter on blog should select it, not drill into an empty listing; step = %v, items = %+v", picker.step, picker.items)
	}
	if got := picker.SelectedPath(); got != "workflow/blog" {
		t.Errorf("SelectedPath = %q, want workflow/blog", got)
	}
}

func TestConceptPicker_UseThisDirectorySelectsWorkflowType(t *testing.T) {
	picker := openWorkflowSubmenu(t, workflowTypeService())
	picker = moveToID(t, picker, "workflow/design")
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if picker.step != stepSelectingItem {
		t.Fatalf("enter on design should drill into its work items, step = %v", picker.step)
	}

	// The cursor starts on a work item, not the pinned directory option.
	if it, _ := picker.sel.Selected(); it.ID == thisDirOptionID {
		t.Error("initial cursor should rest on a work item, not the directory option")
	}

	picker = moveToID(t, picker, thisDirOptionID)
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if picker.step != stepDone {
		t.Fatalf("choosing the directory option should finish, step = %v", picker.step)
	}
	if got := picker.SelectedPath(); got != "workflow/design" {
		t.Errorf("SelectedPath = %q, want workflow/design", got)
	}
}

func TestConceptPicker_UseThisDirectoryHiddenAtConceptRoot(t *testing.T) {
	picker := openWorkflowSubmenu(t, workflowTypeService())
	for _, it := range picker.itemSelectorItems() {
		if it.ID == thisDirOptionID {
			t.Fatal("concept root submenu should not offer the directory option")
		}
	}

	// Drilling in then backing out removes it again.
	picker = moveToID(t, picker, "workflow/design")
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEnter})
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if picker.currentDir != "" {
		t.Errorf("currentDir after backing out = %q, want empty", picker.currentDir)
	}
	for _, it := range picker.itemSelectorItems() {
		if it.ID == thisDirOptionID {
			t.Fatal("directory option should disappear after returning to the concept root")
		}
	}
}

func TestConceptPicker_EmptyDrilledDirectoryStillSelectable(t *testing.T) {
	svc := mockConceptService{
		concepts: []concept.Concept{
			{Name: "docs", Path: "docs", Description: "Documentation", HasItems: true},
		},
		items: map[string][]concept.Item{
			"docs:": {{Name: "guides", Path: "docs/guides", IsDir: true, Children: 1}},
		},
	}
	picker := NewConceptPickerModel(context.Background(), svc, "")
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEnter})
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})

	if len(picker.items) != 0 {
		t.Fatalf("drilled listing items = %+v, want none", picker.items)
	}
	picker, _ = picker.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if picker.step != stepDone {
		t.Fatalf("an empty drilled directory should still be selectable, step = %v", picker.step)
	}
	if got := picker.SelectedPath(); got != "docs/guides" {
		t.Errorf("SelectedPath = %q, want docs/guides", got)
	}
}
