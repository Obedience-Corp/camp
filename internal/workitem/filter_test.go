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
		{LifecycleStage: LifecycleStageInbox, Title: "a"},
		{LifecycleStage: LifecycleStageActive, Title: "b"},
		{LifecycleStage: LifecycleStageReady, Title: "c"},
	}

	filtered := Filter(items, nil, []string{"active"}, "")
	if len(filtered) != 1 || filtered[0].Title != "b" {
		t.Errorf("expected 1 active item, got %v", filtered)
	}
}

func TestFilter_ByStageNone(t *testing.T) {
	items := []WorkItem{
		{WorkflowType: WorkflowTypeDesign, LifecycleStage: LifecycleStageNone, Title: "design"},
		{WorkflowType: WorkflowTypeExplore, LifecycleStage: LifecycleStageNone, Title: "explore"},
		{WorkflowType: WorkflowTypeIntent, LifecycleStage: LifecycleStageInbox, Title: "intent"},
		{WorkflowType: WorkflowTypeFestival, LifecycleStage: LifecycleStageActive, Title: "festival"},
	}

	filtered := Filter(items, nil, []string{"none"}, "")
	if len(filtered) != 2 {
		t.Fatalf("expected 2 no-stage items, got %d: %v", len(filtered), filtered)
	}
	for _, item := range filtered {
		if item.LifecycleStage != LifecycleStageNone {
			t.Fatalf("unexpected stage %q in no-stage filter", item.LifecycleStage)
		}
	}
}

func TestFilter_StageActiveExcludesNone(t *testing.T) {
	items := []WorkItem{
		{WorkflowType: WorkflowTypeDesign, LifecycleStage: LifecycleStageNone, Title: "design"},
		{WorkflowType: WorkflowTypeExplore, LifecycleStage: LifecycleStageNone, Title: "explore"},
		{WorkflowType: WorkflowTypeFestival, LifecycleStage: LifecycleStageActive, Title: "festival"},
	}

	filtered := Filter(items, nil, []string{"active"}, "")
	if len(filtered) != 1 || filtered[0].Title != "festival" {
		t.Fatalf("expected only active festival, got %v", filtered)
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

func TestFilter_ByCategory(t *testing.T) {
	items := []WorkItem{
		{WorkflowType: WorkflowTypeDesign, WorkflowCategory: "plan", Title: "d"},
		{WorkflowType: WorkflowTypeExplore, WorkflowCategory: "research", Title: "e"},
		{WorkflowType: WorkflowType("code_reviews"), WorkflowCategory: "review", Title: "r"},
	}

	got := FilterAdvanced(items, FilterOptions{Categories: []string{"research"}, ShowParked: true})
	if len(got) != 1 || got[0].Title != "e" {
		t.Fatalf("category filter = %v, want single research item", got)
	}

	got = FilterAdvanced(items, FilterOptions{Categories: []string{"plan", "review"}, ShowParked: true})
	if len(got) != 2 {
		t.Fatalf("multi-category filter len = %d, want 2", len(got))
	}
}

func TestFilter_UncategorizedMatchesEmptyCategory(t *testing.T) {
	items := []WorkItem{{Title: "uncategorized"}, {Title: "plan", WorkflowCategory: "plan"}}
	got := FilterAdvanced(items, FilterOptions{Categories: []string{"uncategorized"}, ShowParked: true})
	if len(got) != 1 || got[0].Title != "uncategorized" {
		t.Fatalf("uncategorized filter = %v", got)
	}
}

func TestFilter_CategoryCombinesWithType(t *testing.T) {
	items := []WorkItem{
		{WorkflowType: WorkflowTypeDesign, WorkflowCategory: "plan", Title: "d"},
		{WorkflowType: WorkflowTypeFestival, WorkflowCategory: "plan", Title: "f"},
		{WorkflowType: WorkflowTypeExplore, WorkflowCategory: "research", Title: "e"},
	}
	got := FilterAdvanced(items, FilterOptions{Types: []string{"festival"}, Categories: []string{"plan"}, ShowParked: true})
	if len(got) != 1 || got[0].Title != "f" {
		t.Fatalf("type+category filter = %v, want festival plan item", got)
	}
}

func TestFilter_ByDisplayedStatus(t *testing.T) {
	items := []WorkItem{
		{Title: "intent active", LifecycleStage: LifecycleStageActive},
		{Title: "design active", LifecycleStage: LifecycleStageNone, AttentionStage: "active"},
		{Title: "current", LifecycleStage: LifecycleStageNone, AttentionStage: "current"},
		{Title: "planning", LifecycleStage: LifecycleStagePlanning},
	}

	got := FilterAdvanced(items, FilterOptions{Statuses: []string{"active"}})
	if len(got) != 2 || got[0].Title != "intent active" || got[1].Title != "design active" {
		t.Fatalf("display status active = %v", got)
	}
	got = FilterAdvanced(items, FilterOptions{Statuses: []string{"planning"}})
	if len(got) != 1 || got[0].Title != "planning" {
		t.Fatalf("planning alias = %v", got)
	}
}

func TestFilter_StatusParkedOverridesDefaultHiding(t *testing.T) {
	items := []WorkItem{{Title: "parked", AttentionStage: "parked"}, {Title: "active", AttentionStage: "active"}}
	got := FilterAdvanced(items, FilterOptions{Statuses: []string{"parked"}, ShowParked: false})
	if len(got) != 1 || got[0].Title != "parked" {
		t.Fatalf("status parked = %v", got)
	}
}

func TestFilter_QueryMatchesCategory(t *testing.T) {
	items := []WorkItem{
		{WorkflowType: WorkflowTypeExplore, WorkflowCategory: "research", Title: "alpha"},
		{WorkflowType: WorkflowTypeDesign, WorkflowCategory: "plan", Title: "beta"},
	}
	got := FilterAdvanced(items, FilterOptions{Query: "research", ShowParked: true})
	if len(got) != 1 || got[0].Title != "alpha" {
		t.Fatalf("query on category = %v, want the research item", got)
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

func TestHasAllTags(t *testing.T) {
	cases := []struct {
		name     string
		itemTags []string
		wanted   []string
		want     bool
	}{
		{"empty wanted is always true", []string{"a"}, nil, true},
		{"empty wanted with empty item is true", nil, nil, true},
		{"single matching tag", []string{"a", "b"}, []string{"a"}, true},
		{"single non-matching tag", []string{"a", "b"}, []string{"c"}, false},
		{"all wanted present", []string{"a", "b", "c"}, []string{"a", "b"}, true},
		{"one wanted missing is false, distinguishing AND from OR", []string{"a", "c"}, []string{"a", "b"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasAllTags(tc.itemTags, tc.wanted); got != tc.want {
				t.Errorf("hasAllTags(%v, %v) = %v, want %v", tc.itemTags, tc.wanted, got, tc.want)
			}
		})
	}
}

func TestAnyMatches(t *testing.T) {
	cases := []struct {
		name   string
		items  []string
		wanted []string
		want   bool
	}{
		{"empty wanted set matches nothing; match-all lives in the FilterAdvanced guard", []string{"projects/camp"}, nil, false},
		{"single matching entry", []string{"projects/camp"}, []string{"projects/camp"}, true},
		{"one of several entries matches", []string{"projects/fest", "projects/camp"}, []string{"projects/camp"}, true},
		{"no matching entries", []string{"projects/obey"}, []string{"projects/camp", "projects/fest"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := anyMatches(tc.items, toSet(tc.wanted)); got != tc.want {
				t.Errorf("anyMatches(%v, toSet(%v)) = %v, want %v", tc.items, tc.wanted, got, tc.want)
			}
		})
	}
}

func TestFilterAdvanced_TagsAreANDedProjectsAreORed(t *testing.T) {
	items := []WorkItem{
		{Title: "both-tags", Tags: []string{"a", "b"}},
		{Title: "only-a", Tags: []string{"a"}},
		{Title: "only-b", Tags: []string{"b"}},
		{Title: "camp", Projects: []string{"projects/camp"}},
		{Title: "fest", Projects: []string{"projects/fest"}},
		{Title: "obey", Projects: []string{"projects/obey"}},
	}

	got := FilterAdvanced(items, FilterOptions{Tags: []string{"a", "b"}, ShowParked: true})
	if len(got) != 1 || got[0].Title != "both-tags" {
		t.Fatalf("Tags AND = %v, want only both-tags", got)
	}

	got = FilterAdvanced(items, FilterOptions{Projects: []string{"projects/camp", "projects/fest"}, ShowParked: true})
	if len(got) != 2 || got[0].Title != "camp" || got[1].Title != "fest" {
		t.Fatalf("Projects OR = %v, want camp and fest", got)
	}
}

func TestFilterAdvanced_TagsProjectsCombineAcrossDimensions(t *testing.T) {
	items := []WorkItem{
		{Title: "match", WorkflowType: WorkflowTypeDesign, Tags: []string{"a", "b"}, Projects: []string{"projects/camp"}},
		{Title: "wrong-type", WorkflowType: WorkflowTypeIntent, Tags: []string{"a", "b"}, Projects: []string{"projects/camp"}},
		{Title: "missing-tag", WorkflowType: WorkflowTypeDesign, Tags: []string{"a"}, Projects: []string{"projects/camp"}},
		{Title: "wrong-project", WorkflowType: WorkflowTypeDesign, Tags: []string{"a", "b"}, Projects: []string{"projects/obey"}},
	}
	got := FilterAdvanced(items, FilterOptions{
		Types:      []string{"design"},
		Tags:       []string{"a", "b"},
		Projects:   []string{"projects/camp"},
		ShowParked: true,
	})
	if len(got) != 1 || got[0].Title != "match" {
		t.Fatalf("cross-dimension AND = %v, want only the item matching type, tags, and project", got)
	}
}

func TestFilterAdvanced_NoFilterOptionsReturnsAllItems(t *testing.T) {
	items := []WorkItem{
		{Title: "a", Tags: []string{"x"}},
		{Title: "b", Projects: []string{"projects/camp"}},
	}
	if got := FilterAdvanced(items, FilterOptions{}); len(got) != len(items) {
		t.Fatalf("empty options should return all %d items, got %d", len(items), len(got))
	}
	if got := FilterAdvanced(items, FilterOptions{ShowParked: true}); len(got) != len(items) {
		t.Fatalf("early-return guard should return all %d items, got %d", len(items), len(got))
	}
}

// Residents carry their original type and the folder's stage, so --type and
// --stage compose over them with no filter-side change.
func TestFilter_ComposesOverRailResidents(t *testing.T) {
	items := []WorkItem{
		// A workflow/ directory item is active by location, so stage alone does not
		// separate a root item from a rail-active resident; ready does.
		{Key: "design:workflow/design/root", WorkflowType: WorkflowTypeDesign,
			LifecycleStage: LifecycleStageActive, RelativePath: "workflow/design/root", Title: "root-design"},
		{Key: "design:festivals/active/res-a", WorkflowType: WorkflowTypeDesign,
			LifecycleStage: LifecycleStageActive, RelativePath: "festivals/active/res-a", Title: "active-resident"},
		{Key: "design:festivals/ready/res-b", WorkflowType: WorkflowTypeDesign,
			LifecycleStage: LifecycleStageReady, RelativePath: "festivals/ready/res-b", Title: "ready-resident"},
		{Key: "explore:festivals/active/res-c", WorkflowType: WorkflowTypeExplore,
			LifecycleStage: LifecycleStageActive, RelativePath: "festivals/active/res-c", Title: "explore-resident"},
		{Key: "festival:festivals/active/real", WorkflowType: WorkflowTypeFestival,
			LifecycleStage: LifecycleStageActive, RelativePath: "festivals/active/real", Title: "real-festival"},
	}

	tests := []struct {
		name   string
		types  []string
		stages []string
		want   []string
	}{
		{"type design finds the residents", []string{"design"}, nil,
			[]string{"root-design", "active-resident", "ready-resident"}},
		{"stage active spans types and homes", nil, []string{"active"},
			[]string{"root-design", "active-resident", "explore-resident", "real-festival"}},
		{"type and stage compose", []string{"design"}, []string{"active"},
			[]string{"root-design", "active-resident"}},
		{"stage ready", []string{"design"}, []string{"ready"},
			[]string{"ready-resident"}},
		{"type explore stage active", []string{"explore"}, []string{"active"},
			[]string{"explore-resident"}},
		{"stage ready isolates the ready resident", []string{"design"}, []string{"ready"},
			[]string{"ready-resident"}},
		{"festival type excludes residents", []string{"festival"}, nil,
			[]string{"real-festival"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := titlesOf(Filter(items, tc.types, tc.stages, ""))
			if !sameSet(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func titlesOf(items []WorkItem) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Title)
	}
	return out
}

func sameSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := make(map[string]int, len(got))
	for _, g := range got {
		seen[g]++
	}
	for _, w := range want {
		if seen[w] == 0 {
			return false
		}
		seen[w]--
	}
	return true
}
