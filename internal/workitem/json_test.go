package workitem

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNewPayload_SchemaVersion(t *testing.T) {
	p := NewPayload("/tmp/campaign", nil)
	if p.SchemaVersion != SchemaVersion {
		t.Errorf("schema = %q, want %s", p.SchemaVersion, SchemaVersion)
	}
}

func TestNewPayload_CountsByType(t *testing.T) {
	items := []WorkItem{
		{WorkflowType: WorkflowTypeIntent},
		{WorkflowType: WorkflowTypeIntent},
		{WorkflowType: WorkflowTypeDesign},
		{WorkflowType: WorkflowTypeExplore},
		{WorkflowType: WorkflowTypeFestival},
		{WorkflowType: WorkflowTypeFestival},
	}
	p := NewPayload("/tmp", items)

	if p.Counts.Total != 6 {
		t.Errorf("total = %d, want 6", p.Counts.Total)
	}
	if p.Counts.Intent != 2 {
		t.Errorf("intent = %d, want 2", p.Counts.Intent)
	}
	if p.Counts.Design != 1 {
		t.Errorf("design = %d, want 1", p.Counts.Design)
	}
	if p.Counts.Explore != 1 {
		t.Errorf("explore = %d, want 1", p.Counts.Explore)
	}
	if p.Counts.Festival != 2 {
		t.Errorf("festival = %d, want 2", p.Counts.Festival)
	}
}

func TestNewPayload_NilItemsBecomesEmptyArray(t *testing.T) {
	p := NewPayload("/tmp", nil)

	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	// items should be [] not null
	if !json.Valid(data) {
		t.Fatal("invalid JSON")
	}
	var raw map[string]json.RawMessage
	json.Unmarshal(data, &raw)
	if string(raw["items"]) == "null" {
		t.Error("items should be [] not null in JSON")
	}
}

func TestNewPayload_SortInfo(t *testing.T) {
	p := NewPayload("/tmp", nil)
	if p.Sort.Primary != "manual_priority" {
		t.Errorf("sort.primary = %q, want manual_priority", p.Sort.Primary)
	}
	if p.Sort.Secondary != "sort_timestamp" {
		t.Errorf("sort.secondary = %q, want sort_timestamp", p.Sort.Secondary)
	}
	if p.Sort.Direction != "desc" {
		t.Errorf("sort.direction = %q, want desc", p.Sort.Direction)
	}
}

func TestNewPayload_CampaignRoot(t *testing.T) {
	p := NewPayload("/my/campaign", nil)
	if p.CampaignRoot != "/my/campaign" {
		t.Errorf("campaign_root = %q, want /my/campaign", p.CampaignRoot)
	}
}

func TestNewPayload_StageVocabulary(t *testing.T) {
	p := NewPayload("/tmp", nil)
	intentStages := p.StageVocabulary[string(WorkflowTypeIntent)]
	if len(intentStages) == 0 {
		t.Fatalf("missing intent stage vocabulary: %#v", p.StageVocabulary)
	}
	if !containsString(intentStages, string(LifecycleStageInbox)) {
		t.Fatalf("intent stage vocabulary = %#v, want inbox", intentStages)
	}

	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"stage_vocabulary"`) {
		t.Fatalf("payload missing stage_vocabulary: %s", data)
	}
}

func TestWorkItem_ManualPriorityOmitEmpty(t *testing.T) {
	item := WorkItem{Key: "intent:foo"}
	data, _ := json.Marshal(item)
	if strings.Contains(string(data), "manual_priority") {
		t.Error("manual_priority should be omitted when empty")
	}

	item.ManualPriority = "high"
	data, _ = json.Marshal(item)
	if !strings.Contains(string(data), `"manual_priority":"high"`) {
		t.Errorf("manual_priority should be present, got: %s", data)
	}
}

func TestWorkItem_StableIDOmittedWhenEmpty(t *testing.T) {
	item := WorkItem{Key: "design:legacy"}
	data, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"stable_id":`) {
		t.Errorf("expected stable_id to be omitted when empty, got: %s", data)
	}
}

func TestWorkItem_StableIDPresentWhenPopulated(t *testing.T) {
	item := WorkItem{Key: "design:with-md", StableID: "x-001"}
	data, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"stable_id":"x-001"`) {
		t.Errorf("output missing stable_id: %s", data)
	}
}

func TestWorkItemWorkflow_ZeroValuesAreEmitted(t *testing.T) {
	item := WorkItem{Key: "design:workflow", WorkflowMeta: &WorkItemWorkflow{}}
	data, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	for _, field := range []string{
		`"current_step":0`,
		`"total_steps":0`,
		`"completed_steps":0`,
		`"blocked":false`,
		`"doc_hash_changed":false`,
	} {
		if !strings.Contains(out, field) {
			t.Fatalf("workflow JSON missing %s: %s", field, out)
		}
	}
}

func TestSchemaVersion_IsV1Alpha9(t *testing.T) {
	if SchemaVersion != "workitems/v1alpha9" {
		t.Errorf("SchemaVersion = %q, want workitems/v1alpha9", SchemaVersion)
	}
}

func TestNewPayload_AttentionAndSections(t *testing.T) {
	items := []WorkItem{
		{Key: "a", Title: "A", AttentionStage: "current", AttentionStageSource: "explicit", Group: "camp-workflow"},
		{Key: "b", Title: "B", AttentionStage: "active", AttentionStageSource: "derived"},
	}
	p := NewPayloadWithGrouping("/tmp", items, "group")
	if len(p.AttentionStageVocabulary) == 0 {
		t.Fatal("missing attention stage vocabulary")
	}
	if len(p.GroupVocabulary) != 1 || p.GroupVocabulary[0] != "camp-workflow" {
		t.Fatalf("group vocabulary = %#v, want camp-workflow", p.GroupVocabulary)
	}
	if p.Grouping.GroupBy != "group" {
		t.Fatalf("group_by = %q, want group", p.Grouping.GroupBy)
	}
	if len(p.Sections) != 2 {
		t.Fatalf("len(sections) = %d, want 2", len(p.Sections))
	}
}

func TestNewPayload_CategoryGroupingAndCounts(t *testing.T) {
	items := []WorkItem{
		{Key: "a", Title: "A", WorkflowType: WorkflowTypeDesign, WorkflowCategory: "plan"},
		{Key: "b", Title: "B", WorkflowType: WorkflowTypeExplore, WorkflowCategory: "research"},
		{Key: "c", Title: "C", WorkflowType: WorkflowTypeFestival, WorkflowCategory: "plan"},
	}
	p := NewPayloadWithGrouping("/tmp", items, "category")

	if p.CategoryCounts["plan"] != 2 || p.CategoryCounts["research"] != 1 {
		t.Fatalf("category_counts = %#v, want plan:2 research:1", p.CategoryCounts)
	}
	if !containsString(p.Grouping.AvailableGroupBy, "category") {
		t.Fatalf("available_group_by missing category: %#v", p.Grouping.AvailableGroupBy)
	}
	if len(p.Sections) != 2 {
		t.Fatalf("category sections = %d, want 2", len(p.Sections))
	}
}

func TestNewPayload_CategoryFieldsNonNull(t *testing.T) {
	p := NewPayload("/tmp", nil)
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["category_vocabulary"]) == "null" {
		t.Error("category_vocabulary should be [] not null")
	}
	if string(raw["category_counts"]) == "null" {
		t.Error("category_counts should be {} not null")
	}
}

func TestWorkItem_WorkflowCategoryOmitEmpty(t *testing.T) {
	item := WorkItem{Key: "design:foo"}
	data, _ := json.Marshal(item)
	if strings.Contains(string(data), "workflow_category") {
		t.Error("workflow_category should be omitted when empty")
	}

	item.WorkflowCategory = "plan"
	data, _ = json.Marshal(item)
	if !strings.Contains(string(data), `"workflow_category":"plan"`) {
		t.Errorf("workflow_category should be present, got: %s", data)
	}
}

func TestApplyMetadata_EmptyTagsProjectsNeverNull(t *testing.T) {
	item, err := ApplyMetadata(
		WorkItem{Key: "design:foo", RelativePath: "workflow/design/foo"},
		&Metadata{ID: "design-foo", Type: "design"},
	)
	if err != nil {
		t.Fatalf("ApplyMetadata: %v", err)
	}
	data, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, field := range []string{"tags", "projects"} {
		val, ok := raw[field]
		if !ok {
			t.Errorf("%s key missing, want []", field)
			continue
		}
		if string(val) != "[]" {
			t.Errorf("%s = %s, want []", field, val)
		}
	}
}

func TestWorkItem_PopulatedTagsProjectsRoundTrip(t *testing.T) {
	item, err := ApplyMetadata(
		WorkItem{Key: "design:foo", RelativePath: "workflow/design/foo"},
		&Metadata{
			ID:       "design-foo",
			Type:     "design",
			Tags:     []string{"ux", "public-launch"},
			Projects: []string{"projects/camp", "projects/fest"},
		},
	)
	if err != nil {
		t.Fatalf("ApplyMetadata: %v", err)
	}
	got := string(mustMarshal(t, item))
	if !strings.Contains(got, `"tags":["ux","public-launch"]`) {
		t.Errorf("tags not preserved in order: %s", got)
	}
	if !strings.Contains(got, `"projects":["projects/camp","projects/fest"]`) {
		t.Errorf("projects not preserved in order: %s", got)
	}
}

func TestNewPayload_TagsProjectsSerialization(t *testing.T) {
	items := []WorkItem{
		{Key: "a", Title: "A", WorkflowType: WorkflowTypeDesign, Tags: []string{"ux"}, Projects: []string{"projects/camp"}},
		{Key: "b", Title: "B", WorkflowType: WorkflowTypeIntent, Tags: []string{}, Projects: []string{}},
	}
	got := string(mustMarshal(t, NewPayloadWithGrouping("/tmp", items, "type")))
	if !strings.Contains(got, `"tags":["ux"]`) {
		t.Errorf("populated tags missing from payload: %s", got)
	}
	if !strings.Contains(got, `"tags":[]`) {
		t.Errorf("empty tags should serialize as [] in payload: %s", got)
	}
	if strings.Contains(got, `"tags":null`) || strings.Contains(got, `"projects":null`) {
		t.Errorf("payload must never contain null tags or projects: %s", got)
	}
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

func containsString(vals []string, want string) bool {
	for _, got := range vals {
		if got == want {
			return true
		}
	}
	return false
}
