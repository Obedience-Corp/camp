package workitem

import (
	"context"
	"testing"
	"testing/fstest"
)

// completedMeta builds a WorkItemWorkflow in the REAL post-completion shape fest
// produces: active_run_id is CLEARED on completion, and the completed run
// survives as the latest run. (The previous fixtures set active_run_id at the
// completed run, a state fest never emits; that bug is what this PR fixes.)
func completedMeta() *WorkItemWorkflow {
	return &WorkItemWorkflow{LatestRunStatus: "completed", LatestRunID: "run-001"}
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
				WorkflowType: WorkflowType("chore"),
				RelativePath: "workflow/chore/foo",
				WorkflowMeta: nil,
			},
			wantIncluded: false,
		},
		{
			name: "a newer run is active is excluded (multi-run: latest completed but a new run started)",
			item: WorkItem{
				WorkflowType: WorkflowType("chore"),
				RelativePath: "workflow/chore/foo",
				WorkflowMeta: &WorkItemWorkflow{ActiveRunID: "run-002", RunStatus: "active", LatestRunStatus: "completed", LatestRunID: "run-001"},
			},
			wantIncluded: false,
		},
		{
			name: "an active (blocked) run is excluded",
			item: WorkItem{
				WorkflowType: WorkflowTypeExplore,
				RelativePath: "workflow/explore/foo",
				WorkflowMeta: &WorkItemWorkflow{ActiveRunID: "run-001", RunStatus: "blocked"},
			},
			wantIncluded: false,
		},
		{
			name: "latest run abandoned is excluded (not completed)",
			item: WorkItem{
				WorkflowType: WorkflowType("chore"),
				RelativePath: "workflow/chore/foo",
				WorkflowMeta: &WorkItemWorkflow{LatestRunStatus: "abandoned", LatestRunID: "run-001"},
			},
			wantIncluded: false,
		},
		{
			name: "latest completed but empty latest run id is excluded as malformed",
			item: WorkItem{
				WorkflowType: WorkflowType("chore"),
				RelativePath: "workflow/chore/foo",
				WorkflowMeta: &WorkItemWorkflow{LatestRunStatus: "completed", LatestRunID: ""},
			},
			wantIncluded: false,
		},
		{
			name: "relative path with dungeon segment is excluded by defensive guard",
			item: WorkItem{
				WorkflowType: WorkflowType("chore"),
				RelativePath: "workflow/chore/dungeon/foo",
				WorkflowMeta: completedMeta(),
			},
			wantIncluded: false,
		},
		{
			name: "workitem named my-dungeon-notes is NOT excluded (segment match, not substring)",
			item: WorkItem{
				WorkflowType: WorkflowType("chore"),
				RelativePath: "workflow/chore/my-dungeon-notes",
				WorkflowMeta: completedMeta(),
			},
			wantIncluded: true,
		},
		// Happy paths.
		{
			name: "chore item with completed run is included",
			item: WorkItem{
				WorkflowType: WorkflowType("chore"),
				RelativePath: "workflow/chore/foo",
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
			got := PlanSweep([]WorkItem{tc.item}).Candidates
			if tc.wantIncluded && len(got) != 1 {
				t.Fatalf("expected item included, got %d candidates", len(got))
			}
			if !tc.wantIncluded && len(got) != 0 {
				t.Fatalf("expected item excluded, got %d candidates: %+v", len(got), got)
			}
		})
	}
}

// TestPlanSweep_TypeGating is the type-aware half of the plan: a completed
// authoring run means something different for each kind of work, so it must not
// be turned into the same action for all of them.
func TestPlanSweep_TypeGating(t *testing.T) {
	tests := []struct {
		name            string
		workflowType    WorkflowType
		wantCandidate   bool
		wantDisposition SweepDisposition
		wantSkipReason  string
	}{
		{
			name:           "design is never a candidate on run completion",
			workflowType:   WorkflowTypeDesign,
			wantSkipReason: SkipDesignAwaitsImplementation,
		},
		{
			name:            "explore is a candidate that needs routing",
			workflowType:    WorkflowTypeExplore,
			wantCandidate:   true,
			wantDisposition: DispositionRoute,
		},
		{
			name:            "research is routed like explore despite being a custom type",
			workflowType:    WorkflowTypeResearch,
			wantCandidate:   true,
			wantDisposition: DispositionRoute,
		},
		{
			name:            "bug is promotable",
			workflowType:    WorkflowType("bug"),
			wantCandidate:   true,
			wantDisposition: DispositionPromote,
		},
		{
			name:            "chore is promotable",
			workflowType:    WorkflowType("chore"),
			wantCandidate:   true,
			wantDisposition: DispositionPromote,
		},
		{
			name:            "feature is promotable",
			workflowType:    WorkflowType("feature"),
			wantCandidate:   true,
			wantDisposition: DispositionPromote,
		},
		{
			name:            "an unknown custom type is promotable",
			workflowType:    WorkflowType("incident"),
			wantCandidate:   true,
			wantDisposition: DispositionPromote,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			item := WorkItem{
				WorkflowType: tc.workflowType,
				RelativePath: "workflow/" + string(tc.workflowType) + "/thing",
				WorkflowMeta: completedMeta(),
			}
			plan := PlanSweep([]WorkItem{item})

			if !tc.wantCandidate {
				if len(plan.Candidates) != 0 {
					t.Fatalf("expected no candidates, got %+v", plan.Candidates)
				}
				if len(plan.Skipped) != 1 {
					t.Fatalf("expected exactly 1 reported skip, got %d", len(plan.Skipped))
				}
				if plan.Skipped[0].Reason != tc.wantSkipReason {
					t.Errorf("skip reason = %q, want %q", plan.Skipped[0].Reason, tc.wantSkipReason)
				}
				if plan.Skipped[0].Detail == "" {
					t.Error("a reported skip must carry a human-readable detail")
				}
				if plan.Skipped[0].RunID != "run-001" {
					t.Errorf("skip RunID = %q, want run-001", plan.Skipped[0].RunID)
				}
				return
			}

			if len(plan.Skipped) != 0 {
				t.Fatalf("expected no skips, got %+v", plan.Skipped)
			}
			if len(plan.Candidates) != 1 {
				t.Fatalf("expected 1 candidate, got %d", len(plan.Candidates))
			}
			if plan.Candidates[0].Disposition != tc.wantDisposition {
				t.Errorf("Disposition = %q, want %q", plan.Candidates[0].Disposition, tc.wantDisposition)
			}
		})
	}
}

func TestPlanSweep_CompletionState(t *testing.T) {
	tests := []struct {
		name       string
		completion *CompletionState
		want       bool
	}{
		{name: "default remains eligible", want: true},
		{name: "same run reviewed", completion: &CompletionState{Policy: CompletionPolicyReview, ReviewedRunID: "run-001"}},
		{name: "older run reviewed", completion: &CompletionState{Policy: CompletionPolicyReview, ReviewedRunID: "run-000"}, want: true},
		{name: "recurring", completion: &CompletionState{Policy: CompletionPolicyRecurring}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plan := PlanSweep([]WorkItem{{
				WorkflowType: WorkflowTypeExplore,
				RelativePath: "workflow/explore/example",
				WorkflowMeta: completedMeta(),
				Completion:   tc.completion,
			}})
			if got := len(plan.Candidates) == 1; got != tc.want {
				t.Fatalf("candidate = %v, want %v (plan=%+v)", got, tc.want, plan)
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
		{1, "1 workitem has completed runs; run camp workitem sweep --prompt"},
		{2, "2 workitems have completed runs; run camp workitem sweep --prompt"},
	}
	for _, tc := range tests {
		if got := SweepBannerText(tc.n); got != tc.want {
			t.Errorf("SweepBannerText(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

func TestPlanSweep_CandidatePayload(t *testing.T) {
	item := WorkItem{
		WorkflowType: WorkflowType("chore"),
		RelativePath: "workflow/chore/foo",
		WorkflowMeta: &WorkItemWorkflow{LatestRunStatus: "completed", LatestRunID: "run-042"},
	}
	got := PlanSweep([]WorkItem{item}).Candidates
	if len(got) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(got))
	}
	if got[0].Reason != EvidenceWorkflowRunCompleted {
		t.Errorf("Reason = %q, want %q", got[0].Reason, EvidenceWorkflowRunCompleted)
	}
	if got[0].RunID != "run-042" {
		t.Errorf("RunID = %q, want run-042", got[0].RunID)
	}
	if got[0].Item.RelativePath != item.RelativePath {
		t.Errorf("Item.RelativePath = %q, want %q", got[0].Item.RelativePath, item.RelativePath)
	}
}

func TestPlanSweep_MixedSliceReturnsOnlyEligible(t *testing.T) {
	items := []WorkItem{
		{WorkflowType: WorkflowTypeFestival, RelativePath: "festivals/active/x-FA0001", WorkflowMeta: completedMeta()},
		{WorkflowType: WorkflowType("chore"), RelativePath: "workflow/chore/keep", WorkflowMeta: completedMeta()},
		{WorkflowType: WorkflowTypeExplore, RelativePath: "workflow/explore/drop", WorkflowMeta: &WorkItemWorkflow{ActiveRunID: "run-001", RunStatus: "active"}}, // a run is active -> excluded
		{WorkflowType: WorkflowTypeExplore, RelativePath: "workflow/explore/keep2", WorkflowMeta: completedMeta()},
	}
	got := PlanSweep(items).Candidates
	if len(got) != 2 {
		t.Fatalf("expected 2 candidates, got %d: %+v", len(got), got)
	}
	paths := map[string]bool{got[0].Item.RelativePath: true, got[1].Item.RelativePath: true}
	if !paths["workflow/chore/keep"] || !paths["workflow/explore/keep2"] {
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
		WorkflowType: WorkflowType("chore"),
		RelativePath: "workflow/chore/multi",
		WorkflowMeta: &WorkItemWorkflow{ActiveRunID: got.ActiveRunID, RunStatus: got.RunStatus},
	}
	plan := PlanSweep([]WorkItem{item})
	if len(plan.Candidates) != 0 || len(plan.Skipped) != 0 {
		t.Errorf("expected multi-run item excluded (active run), got %d candidates and %d skips",
			len(plan.Candidates), len(plan.Skipped))
	}
}

// TestLoadLocalRunFS_LatestRunWhenActiveCleared verifies the REAL post-completion
// shape: fest clears active_run_id and the completed run survives in runs[].
// LoadLocalRunFS must surface the latest run's terminal status (replayed from
// its events), and PlanSweep must treat the item as eligible. This is the
// scenario the previous fixtures never modeled (the bug this PR fixes).
func TestLoadLocalRunFS_LatestRunWhenActiveCleared(t *testing.T) {
	const base = "campaign-root"
	fsys := fstest.MapFS{
		// No active_run_id: fest cleared it on completion. The completed run is
		// indexed in runs[].
		base + "/.workflow/workflow.yaml": {Data: []byte(`workflow_id: wf-done
runs:
    - run_id: run-001
      status: completed
      ended_at: "2026-07-24T19:00:00Z"
`)},
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
	}

	got, err := LoadLocalRunFS(context.Background(), fsys, base)
	if err != nil {
		t.Fatalf("LoadLocalRunFS: %v", err)
	}
	if got == nil {
		t.Fatal("expected progress, got nil")
	}
	if got.ActiveRunID != "" {
		t.Errorf("ActiveRunID = %q, want empty (fest cleared it on completion)", got.ActiveRunID)
	}
	if got.LatestRunID != "run-001" {
		t.Errorf("LatestRunID = %q, want run-001", got.LatestRunID)
	}
	if got.LatestRunStatus != "completed" {
		t.Errorf("LatestRunStatus = %q, want completed (replayed from events)", got.LatestRunStatus)
	}

	item := WorkItem{
		WorkflowType: WorkflowType("chore"),
		RelativePath: "workflow/chore/done",
		WorkflowMeta: &WorkItemWorkflow{ActiveRunID: got.ActiveRunID, LatestRunID: got.LatestRunID, LatestRunStatus: got.LatestRunStatus},
	}
	cands := PlanSweep([]WorkItem{item}).Candidates
	if len(cands) != 1 {
		t.Fatalf("expected the completed item eligible, got %d candidates", len(cands))
	}
	if cands[0].RunID != "run-001" {
		t.Errorf("candidate RunID = %q, want run-001", cands[0].RunID)
	}
}
