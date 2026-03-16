package workitem

import "testing"

func TestFilter_ByType(t *testing.T) {
	items := []WorkItem{
		{WorkflowType: WorkflowTypeIntent, Title: "intent1"},
		{WorkflowType: WorkflowTypeDesign, Title: "design1"},
		{WorkflowType: WorkflowTypeIntent, Title: "intent2"},
		{WorkflowType: WorkflowTypeFestival, Title: "fest1"},
	}

	filtered := Filter(items, []string{"intent"}, nil, "")
	if len(filtered) != 2 {
		t.Errorf("expected 2 intents, got %d", len(filtered))
	}
	for _, item := range filtered {
		if item.WorkflowType != WorkflowTypeIntent {
			t.Errorf("unexpected type %q in filtered results", item.WorkflowType)
		}
	}
}

func TestFilter_ByStage(t *testing.T) {
	items := []WorkItem{
		{LifecycleStage: "inbox", Title: "a"},
		{LifecycleStage: "active", Title: "b"},
		{LifecycleStage: "ready", Title: "c"},
	}

	filtered := Filter(items, nil, []string{"active"}, "")
	if len(filtered) != 1 || filtered[0].Title != "b" {
		t.Errorf("expected 1 active item, got %v", filtered)
	}
}

func TestFilter_ByQuery(t *testing.T) {
	items := []WorkItem{
		{Title: "Auth Feature", RelativePath: "intents/auth.md"},
		{Title: "Dashboard", RelativePath: "design/dashboard"},
	}

	filtered := Filter(items, nil, nil, "auth")
	if len(filtered) != 1 || filtered[0].Title != "Auth Feature" {
		t.Errorf("query filter failed: got %v", filtered)
	}
}

func TestFilter_QueryIsCaseInsensitive(t *testing.T) {
	items := []WorkItem{{Title: "AUTH Feature"}}
	filtered := Filter(items, nil, nil, "auth")
	if len(filtered) != 1 {
		t.Error("query should be case insensitive")
	}
}

func TestFilter_NoFilters(t *testing.T) {
	items := []WorkItem{{Title: "a"}, {Title: "b"}}
	filtered := Filter(items, nil, nil, "")
	if len(filtered) != 2 {
		t.Error("no filters should return all items")
	}
}

func TestFilter_PreservesOrder(t *testing.T) {
	items := []WorkItem{
		{WorkflowType: WorkflowTypeIntent, Title: "first"},
		{WorkflowType: WorkflowTypeDesign, Title: "skip"},
		{WorkflowType: WorkflowTypeIntent, Title: "second"},
	}
	filtered := Filter(items, []string{"intent"}, nil, "")
	if len(filtered) != 2 || filtered[0].Title != "first" || filtered[1].Title != "second" {
		t.Error("filter should preserve original order")
	}
}
