package tui

import (
	"context"
	"testing"

	"github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/priority"
)

func TestCycleCategory(t *testing.T) {
	items := []workitem.WorkItem{
		{Key: "a", WorkflowType: workitem.WorkflowTypeDesign, WorkflowCategory: "plan"},
		{Key: "b", WorkflowType: workitem.WorkflowTypeExplore, WorkflowCategory: "research"},
		{Key: "c", WorkflowType: workitem.WorkflowType("code_reviews"), WorkflowCategory: "review"},
	}
	m := New(context.Background(), items, "", nil, priority.NewStore(), "")
	m.refilter()
	if m.categoryFilter != "" || len(m.filteredItems) != 3 {
		t.Fatalf("initial: category=%q items=%d", m.categoryFilter, len(m.filteredItems))
	}

	steps := []struct {
		wantCategory string
		wantKey      string
	}{
		{"plan", "a"},
		{"research", "b"},
		{"review", "c"},
	}
	for i, step := range steps {
		m.cycleCategory()
		if m.categoryFilter != step.wantCategory {
			t.Fatalf("cycle %d: category=%q want %q", i+1, m.categoryFilter, step.wantCategory)
		}
		if len(m.filteredItems) != 1 || m.filteredItems[0].Key != step.wantKey {
			t.Fatalf("cycle %d: filtered=%v want single %q", i+1, m.filteredItems, step.wantKey)
		}
	}

	m.cycleCategory()
	if m.categoryFilter != "" || len(m.filteredItems) != 3 {
		t.Fatalf("wrap: category=%q items=%d want all", m.categoryFilter, len(m.filteredItems))
	}
}

func TestCategoryFilterComposesWithType(t *testing.T) {
	items := []workitem.WorkItem{
		{Key: "a", WorkflowType: workitem.WorkflowTypeDesign, WorkflowCategory: "plan"},
		{Key: "b", WorkflowType: workitem.WorkflowTypeFestival, WorkflowCategory: "plan"},
		{Key: "c", WorkflowType: workitem.WorkflowTypeExplore, WorkflowCategory: "research"},
	}
	m := New(context.Background(), items, "", nil, priority.NewStore(), "")
	m.typeFilter = "festival"
	m.categoryFilter = "plan"
	m.refilter()
	if len(m.filteredItems) != 1 || m.filteredItems[0].Key != "b" {
		t.Fatalf("type+category compose = %v, want single festival/plan item", m.filteredItems)
	}
}
