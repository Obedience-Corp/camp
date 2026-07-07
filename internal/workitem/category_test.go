package workitem

import "testing"

func TestApplyWorkflowCategories(t *testing.T) {
	categoryForType := func(wt string) string {
		switch wt {
		case "design", "festival":
			return "plan"
		case "explore":
			return "research"
		default:
			return "uncategorized"
		}
	}

	items := []WorkItem{
		{WorkflowType: WorkflowTypeDesign},
		{WorkflowType: WorkflowTypeExplore},
		{WorkflowType: WorkflowTypeFestival},
		{WorkflowType: WorkflowType("customthing")},
	}

	got := ApplyWorkflowCategories(items, categoryForType)

	want := []string{"plan", "research", "plan", "uncategorized"}
	for i, w := range want {
		if got[i].WorkflowCategory != w {
			t.Fatalf("item %d category = %q, want %q", i, got[i].WorkflowCategory, w)
		}
	}
}

func TestApplyWorkflowCategoriesNilFunc(t *testing.T) {
	items := []WorkItem{{WorkflowType: WorkflowTypeDesign}}
	got := ApplyWorkflowCategories(items, nil)
	if got[0].WorkflowCategory != "" {
		t.Fatalf("expected empty category with nil func, got %q", got[0].WorkflowCategory)
	}
}
