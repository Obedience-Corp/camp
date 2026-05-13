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

func TestWorkItem_MetadataFieldsOmittedWhenEmpty(t *testing.T) {
	item := WorkItem{Key: "design:legacy"}
	data, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	for _, key := range []string{"stable_id", "description", "execution", "priority_info", "project", "lineage"} {
		if strings.Contains(out, `"`+key+`":`) {
			t.Errorf("expected %q to be omitted when empty, got: %s", key, out)
		}
	}
}

func TestWorkItem_MetadataFieldsPresentWhenPopulated(t *testing.T) {
	item := WorkItem{
		Key:         "design:with-md",
		StableID:    "x-001",
		Description: "Desc",
		Execution: &WorkItemExecution{
			Mode:     "design",
			Autonomy: "constrained",
			Risk:     "medium",
		},
		PriorityInfo: &WorkItemPriority{Level: "high", Reason: "launch"},
		Project:      &WorkItemProject{Name: "festival", Path: "projects/festival", Role: "affected"},
		WorkflowMeta: &WorkItemWorkflow{DocPath: "WORKFLOW.md", RuntimeDir: ".workflow", WorkflowID: "wf-x"},
		Lineage:      &WorkItemLineage{Supersedes: []string{"workflow/design/old"}},
	}
	data, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	for _, sub := range []string{
		`"stable_id":"x-001"`,
		`"description":"Desc"`,
		`"execution":{`,
		`"mode":"design"`,
		`"priority_info":{`,
		`"level":"high"`,
		`"project":{`,
		`"workflow":{`,
		`"lineage":{`,
		`"supersedes":["workflow/design/old"]`,
	} {
		if !strings.Contains(out, sub) {
			t.Errorf("output missing %q: %s", sub, out)
		}
	}
}

func TestSchemaVersion_IsV1Alpha4(t *testing.T) {
	if SchemaVersion != "workitems/v1alpha4" {
		t.Errorf("SchemaVersion = %q, want workitems/v1alpha4", SchemaVersion)
	}
}
