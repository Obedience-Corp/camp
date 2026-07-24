package workitem

import (
	"context"
	"testing"
	"testing/fstest"
)

// completedMeta builds a WorkItemWorkflow in the completed state with a
// non-empty active run id, the shape a real loop-completion produces.
func completedMeta() *WorkItemWorkflow {
	return &WorkItemWorkflow{ActiveRunID: "run-001", RunStatus: "completed"}
}

func TestPlanSweep_Eligibility(t *testing.T) {
	tests := []struct {
		name         string
		item         WorkItem
		wantIncluded bool
	}{
		// Exclusion cases first (campaign convention: error paths before happy paths).
		{
			name: "festival with forced completed meta is excluded by type",
			item: WorkItem{
				WorkflowType: WorkflowTypeFestival,
				RelativePath: "festivals/active/foo-FA0001",
				WorkflowMeta: completedMeta(),
			},
			wantIncluded: false,
		},
		{
			name: "intent with forced completed meta is excluded by type",
			item: WorkItem{
				WorkflowType: WorkflowTypeIntent,
				RelativePath: ".campaign/intents/active/foo.md",
				WorkflowMeta: completedMeta(),
			},
			wantIncluded: false,
		},
		{
			name: "nil workflow meta is excluded and never panics",
			item: WorkItem{
				WorkflowType: WorkflowTypeDesign,
				RelativePath: "workflow/design/foo",
				WorkflowMeta: nil,
			},
			wantIncluded: false,
		},
		{
			name: "run status active is excluded",
			item: WorkItem{
				WorkflowType: WorkflowTypeDesign,
				RelativePath: "workflow/design/foo",
				WorkflowMeta: &WorkItemWorkflow{ActiveRunID: "run-001", RunStatus: "active"},
			},
			wantIncluded: false,
		},
		{
			name: "run status blocked is excluded",
			item: WorkItem{
				WorkflowType: WorkflowTypeExplore,
				RelativePath: "workflow/explore/foo",
				WorkflowMeta: &WorkItemWorkflow{ActiveRunID: "run-001", RunStatus: "blocked"},
			},
			wantIncluded: false,
		},
		{
			name: "run status abandoned is excluded",
			item: WorkItem{
				WorkflowType: WorkflowTypeDesign,
				RelativePath: "workflow/design/foo",
				WorkflowMeta: &WorkItemWorkflow{ActiveRunID: "run-001", RunStatus: "abandoned"},
			},
			wantIncluded: false,
		},
		{
			name: "completed with empty active run id is excluded as malformed",
			item: WorkItem{
				WorkflowType: WorkflowTypeDesign,
				RelativePath: "workflow/design/foo",
				WorkflowMeta: &WorkItemWorkflow{ActiveRunID: "", RunStatus: "completed"},
			},
			wantIncluded: false,
		},
		{
			name: "relative path with dungeon segment is excluded by defensive guard",
			item: WorkItem{
				WorkflowType: WorkflowTypeDesign,
				RelativePath: "workflow/design/dungeon/foo",
				WorkflowMeta: completedMeta(),
			},
			wantIncluded: false,
		},
		{
			name: "workitem named my-dungeon-notes is NOT excluded (segment match, not substring)",
			item: WorkItem{
				WorkflowType: WorkflowTypeDesign,
				RelativePath: "workflow/design/my-dungeon-notes",
				WorkflowMeta: completedMeta(),
			},
			wantIncluded: true,
		},
		// Happy paths.
		{
			name: "design item with completed run is included",
			item: WorkItem{
				WorkflowType: WorkflowTypeDesign,
				RelativePath: "workflow/design/foo",
				WorkflowMeta: completedMeta(),
			},
			wantIncluded: true,
		},
		{
			name: "explore item with completed run is included",
			item: WorkItem{
				WorkflowType: WorkflowTypeExplore,
				RelativePath: "workflow/explore/bar",
				WorkflowMeta: completedMeta(),
			},
			wantIncluded: true,
		},
		{
			name: "custom-type item with completed run is included",
			item: WorkItem{
				WorkflowType: WorkflowType("research"),
				RelativePath: "workflow/research/baz",
				WorkflowMeta: completedMeta(),
			},
			wantIncluded: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := PlanSweep([]WorkItem{tc.item})
			if tc.wantIncluded && len(got) != 1 {
				t.Fatalf("expected item included, got %d candidates", len(got))
			}
			if !tc.wantIncluded && len(got) != 0 {
				t.Fatalf("expected item excluded, got %d candidates: %+v", len(got), got)
			}
		})
	}
}

func TestSweepBannerText(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, ""},
		{-3, ""},
		{1, "1 workitem have completed runs; run camp workitem sweep"},
		{2, "2 workitems have completed runs; run camp workitem sweep"},
	}
	for _, tc := range tests {
		if got := SweepBannerText(tc.n); got != tc.want {
			t.Errorf("SweepBannerText(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

func TestPlanSweep_CandidatePayload(t *testing.T) {
	item := WorkItem{
		WorkflowType: WorkflowTypeDesign,
		RelativePath: "workflow/design/foo",
		WorkflowMeta: &WorkItemWorkflow{ActiveRunID: "run-042", RunStatus: "completed"},
	}
	got := PlanSweep([]WorkItem{item})
	if len(got) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(got))
	}
	if got[0].Reason != EvidenceWorkflowRunCompleted {
		t.Errorf("Reason = %q, want %q", got[0].Reason, EvidenceWorkflowRunCompleted)
	}
	if got[0].ActiveRunID != "run-042" {
		t.Errorf("ActiveRunID = %q, want run-042", got[0].ActiveRunID)
	}
	if got[0].Item.RelativePath != item.RelativePath {
		t.Errorf("Item.RelativePath = %q, want %q", got[0].Item.RelativePath, item.RelativePath)
	}
}

func TestPlanSweep_MixedSliceReturnsOnlyEligible(t *testing.T) {
	items := []WorkItem{
		{WorkflowType: WorkflowTypeFestival, RelativePath: "festivals/active/x-FA0001", WorkflowMeta: completedMeta()},
		{WorkflowType: WorkflowTypeDesign, RelativePath: "workflow/design/keep", WorkflowMeta: completedMeta()},
		{WorkflowType: WorkflowTypeExplore, RelativePath: "workflow/explore/drop", WorkflowMeta: &WorkItemWorkflow{ActiveRunID: "run-001", RunStatus: "active"}},
		{WorkflowType: WorkflowTypeExplore, RelativePath: "workflow/explore/keep2", WorkflowMeta: completedMeta()},
	}
	got := PlanSweep(items)
	if len(got) != 2 {
		t.Fatalf("expected 2 candidates, got %d: %+v", len(got), got)
	}
	paths := map[string]bool{got[0].Item.RelativePath: true, got[1].Item.RelativePath: true}
	if !paths["workflow/design/keep"] || !paths["workflow/explore/keep2"] {
		t.Errorf("unexpected candidate set: %v", paths)
	}
}

// TestPlanSweep_MultiRunCaveat verifies the read side the planner depends on:
// when a new run starts after a completed one and workflow.yaml's
// active_run_id points at the new run, LoadLocalRunFS replays only the active
// run and never leaks the prior run's stale "completed" status. This is the
// multi-run caveat from spec doc 03 (lines 63-67), verified in phase 1 rather
// than deferred to the phase-3 verification spike.
func TestPlanSweep_MultiRunCaveat(t *testing.T) {
	const base = "campaign-root"
	fsys := fstest.MapFS{
		base + "/.workflow/workflow.yaml": {Data: []byte(`workflow_id: wf-multi
active_run_id: run-002
`)},
		// Prior, completed run. Its completed event must never be replayed
		// because it is not the active run.
		base + "/.workflow/runs/run-001/run.yaml": {Data: []byte(`status: completed
summary:
  total_steps: 2
`)},
		base + "/.workflow/runs/run-001/progress_events.jsonl": {Data: []byte(`{"event_type":"wf_step_start"}
{"event_type":"wf_step_done"}
{"event_type":"wf_step_start"}
{"event_type":"wf_step_done"}
{"event_type":"workflow_run_completed"}
`)},
		// New active run, in progress.
		base + "/.workflow/runs/run-002/run.yaml": {Data: []byte(`status: active
summary:
  total_steps: 3
`)},
		base + "/.workflow/runs/run-002/progress_events.jsonl": {Data: []byte(`{"event_type":"workflow_run_started"}
{"event_type":"wf_step_start"}
`)},
	}

	got, err := LoadLocalRunFS(context.Background(), fsys, base)
	if err != nil {
		t.Fatalf("LoadLocalRunFS: %v", err)
	}
	if got == nil {
		t.Fatal("expected progress, got nil")
	}
	if got.ActiveRunID != "run-002" {
		t.Errorf("ActiveRunID = %q, want run-002", got.ActiveRunID)
	}
	if got.RunStatus == "completed" {
		t.Fatalf("RunStatus = %q; stale completed from run-001 leaked into active run-002", got.RunStatus)
	}
	if got.RunStatus != "active" {
		t.Errorf("RunStatus = %q, want active (run-002 is in progress)", got.RunStatus)
	}

	// The planner consuming this meta must NOT treat the item as eligible:
	// run-002 is active, not completed.
	item := WorkItem{
		WorkflowType: WorkflowTypeDesign,
		RelativePath: "workflow/design/multi",
		WorkflowMeta: &WorkItemWorkflow{ActiveRunID: got.ActiveRunID, RunStatus: got.RunStatus},
	}
	if cands := PlanSweep([]WorkItem{item}); len(cands) != 0 {
		t.Errorf("expected multi-run item excluded (active run), got %d candidates", len(cands))
	}
}
