package workitem

import (
	"encoding/json"
	"testing"
)

func TestNewPayload_SchemaVersion(t *testing.T) {
	p := NewPayload("/tmp/campaign", nil)
	if p.SchemaVersion != "workitems/v1alpha1" {
		t.Errorf("schema = %q, want workitems/v1alpha1", p.SchemaVersion)
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
	if p.Sort.Primary != "updated_at" {
		t.Errorf("sort.primary = %q, want updated_at", p.Sort.Primary)
	}
	if p.Sort.Secondary != "created_at" {
		t.Errorf("sort.secondary = %q, want created_at", p.Sort.Secondary)
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
