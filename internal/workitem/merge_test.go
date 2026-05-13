package workitem

import (
	"reflect"
	"testing"
)

func baseDerivedItem() WorkItem {
	return WorkItem{
		Key:            "design:workflow/design/foo",
		WorkflowType:   WorkflowTypeDesign,
		LifecycleStage: "active",
		Title:          "Foo",
		RelativePath:   "workflow/design/foo",
		PrimaryDoc:     "workflow/design/foo/README.md",
		ItemKind:       ItemKindDirectory,
	}
}

func TestApplyMetadata_Nil(t *testing.T) {
	in := baseDerivedItem()
	out := ApplyMetadata(in, nil)
	if !reflect.DeepEqual(in, out) {
		t.Errorf("nil metadata should not change item")
	}
}

func TestApplyMetadata_TitleOverride(t *testing.T) {
	in := baseDerivedItem()
	in.Title = "derived-foo"
	md := &Metadata{Version: 1, Kind: "workitem", ID: "x", Type: "design", Title: "Custom Title"}
	out := ApplyMetadata(in, md)
	if out.Title != "Custom Title" {
		t.Errorf("Title = %q, want %q", out.Title, "Custom Title")
	}
}

func TestApplyMetadata_TitleEmptyPreservesDerived(t *testing.T) {
	in := baseDerivedItem()
	in.Title = "derived-foo"
	md := &Metadata{Version: 1, Kind: "workitem", ID: "x", Type: "design", Title: ""}
	out := ApplyMetadata(in, md)
	if out.Title != "derived-foo" {
		t.Errorf("Title = %q, want %q (empty metadata.Title should not override)", out.Title, "derived-foo")
	}
}

func TestApplyMetadata_SetsStableID(t *testing.T) {
	in := baseDerivedItem()
	md := &Metadata{Version: 1, Kind: "workitem", ID: "x-001", Type: "design", Title: "T"}
	out := ApplyMetadata(in, md)
	if out.StableID != "x-001" {
		t.Errorf("StableID = %q", out.StableID)
	}
}

func TestApplyMetadata_DoesNotOverridePath(t *testing.T) {
	in := baseDerivedItem()
	derivedPath := in.RelativePath
	md := &Metadata{
		Version: 1, Kind: "workitem", ID: "x", Type: "design", Title: "T",
		Collection: &MetadataCollection{
			RelativePath: "workflow/design/STALE_PATH",
		},
	}
	out := ApplyMetadata(in, md)
	if out.RelativePath != derivedPath {
		t.Errorf("RelativePath changed from %q to %q (filesystem placement must win)", derivedPath, out.RelativePath)
	}
}

func TestApplyMetadata_ExecutionPriorityProjectLineage(t *testing.T) {
	in := baseDerivedItem()
	md := &Metadata{
		Version: 1, Kind: "workitem", ID: "x", Type: "design", Title: "T",
		Execution: &MetadataExecution{
			Mode:          "design",
			Autonomy:      "constrained",
			Risk:          "medium",
			BlockedReason: "waiting on upstream",
		},
		Priority: &MetadataPriority{Level: "high", Reason: "launch"},
		Project:  &MetadataProject{Name: "festival", Path: "projects/festival", Role: "affected"},
		Lineage: &MetadataLineage{
			PromotedFrom: []string{"intent:foo"},
			PromotedTo:   []string{"festival:bar"},
			Supersedes:   []string{"workflow/design/old"},
		},
	}
	out := ApplyMetadata(in, md)

	if out.Execution == nil || out.Execution.Mode != "design" || out.Execution.Autonomy != "constrained" || out.Execution.Risk != "medium" || out.Execution.BlockedReason != "waiting on upstream" {
		t.Errorf("Execution = %+v", out.Execution)
	}
	if out.PriorityInfo == nil || out.PriorityInfo.Level != "high" || out.PriorityInfo.Reason != "launch" {
		t.Errorf("PriorityInfo = %+v", out.PriorityInfo)
	}
	if out.Project == nil || out.Project.Name != "festival" || out.Project.Path != "projects/festival" || out.Project.Role != "affected" {
		t.Errorf("Project = %+v", out.Project)
	}
	if out.Lineage == nil || len(out.Lineage.PromotedFrom) != 1 || len(out.Lineage.PromotedTo) != 1 || len(out.Lineage.Supersedes) != 1 {
		t.Errorf("Lineage = %+v", out.Lineage)
	}
}

func TestApplyMetadata_WorkflowBlock(t *testing.T) {
	in := baseDerivedItem()
	md := &Metadata{
		Version: 1, Kind: "workitem", ID: "x", Type: "design", Title: "T",
		Workflow: &MetadataWorkflow{
			DocPath:     "WORKFLOW.md",
			RuntimeDir:  ".workflow",
			WorkflowID:  "wf-foo",
			ActiveRunID: "run-001",
		},
	}
	out := ApplyMetadata(in, md)
	if out.WorkflowMeta == nil || out.WorkflowMeta.DocPath != "WORKFLOW.md" || out.WorkflowMeta.RuntimeDir != ".workflow" || out.WorkflowMeta.WorkflowID != "wf-foo" || out.WorkflowMeta.ActiveRunID != "run-001" {
		t.Errorf("WorkflowMeta = %+v", out.WorkflowMeta)
	}
}

func TestApplyMetadata_DescriptionEmptyPreservesDerived(t *testing.T) {
	in := baseDerivedItem()
	in.Description = "pre-existing"
	md := &Metadata{Version: 1, Kind: "workitem", ID: "x", Type: "design", Title: "T", Description: ""}
	out := ApplyMetadata(in, md)
	if out.Description != "pre-existing" {
		t.Errorf("Description = %q, want pre-existing", out.Description)
	}
}
