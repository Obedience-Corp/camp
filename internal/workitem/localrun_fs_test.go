package workitem

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"
)

func TestLoadLocalRunFS_NoWorkflowDir(t *testing.T) {
	got, err := LoadLocalRunFS(context.Background(), fstest.MapFS{}, "campaign-root/workflow/feature/foo")
	if err != nil {
		t.Fatalf("LoadLocalRunFS: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil progress, got %+v", got)
	}
}

func TestLoadLocalRunFS_BasicProgress(t *testing.T) {
	fsys := fstest.MapFS{
		"campaign-root/.workflow/workflow.yaml": {Data: []byte(`workflow_id: wf-foo
active_run_id: r1
`)},
		"campaign-root/.workflow/runs/r1/run.yaml": {Data: []byte(`status: active
summary:
  current_step: 0
  total_steps: 3
  completed_steps: 0
  blocked: false
`)},
		"campaign-root/.workflow/runs/r1/progress_events.jsonl": {Data: []byte(`{"event_type":"workflow_run_started"}
{"event_type":"wf_step_start"}
{"event_type":"wf_step_done"}
{"event_type":"wf_step_start"}
`)},
	}
	got, err := LoadLocalRunFS(context.Background(), fsys, "campaign-root")
	if err != nil {
		t.Fatalf("LoadLocalRunFS: %v", err)
	}
	if got == nil {
		t.Fatal("expected progress")
	}
	if got.WorkflowID != "wf-foo" {
		t.Errorf("WorkflowID = %q, want wf-foo", got.WorkflowID)
	}
	if got.TotalSteps != 3 {
		t.Errorf("TotalSteps = %d, want 3", got.TotalSteps)
	}
	if got.CompletedSteps != 1 {
		t.Errorf("CompletedSteps = %d, want 1 (one wf_step_done)", got.CompletedSteps)
	}
	if got.CurrentStep != 2 {
		t.Errorf("CurrentStep = %d, want 2 (two wf_step_start)", got.CurrentStep)
	}
}

func TestLoadLocalRunFS_LegacyWorkitemIDIgnored(t *testing.T) {
	fsys := fstest.MapFS{
		"campaign-root/.workflow/workflow.yaml": {Data: []byte(`workflow_id: wf-foo
workitem_id: legacy-feature-foo-2026-01-01
active_run_id: r1
`)},
		"campaign-root/.workflow/runs/r1/run.yaml": {Data: []byte(`status: active
workitem_id: legacy-feature-foo-2026-01-01
summary:
  current_step: 0
  total_steps: 1
  completed_steps: 0
  blocked: false
`)},
	}
	got, err := LoadLocalRunFS(context.Background(), fsys, "campaign-root")
	if err != nil {
		t.Fatalf("legacy workitem_id should parse silently: %v", err)
	}
	if got == nil || got.WorkflowID != "wf-foo" {
		t.Errorf("expected progress with WorkflowID=wf-foo, got %+v", got)
	}
}

func TestLoadLocalRunFS_MalformedYAML(t *testing.T) {
	fsys := fstest.MapFS{
		"campaign-root/.workflow/workflow.yaml": {Data: []byte("::: not yaml :::\n")},
	}
	_, err := LoadLocalRunFS(context.Background(), fsys, "campaign-root")
	if err == nil {
		t.Fatal("expected parse error for malformed yaml")
	}
	if !strings.Contains(err.Error(), "parsing") {
		t.Errorf("error should mention parsing, got %v", err)
	}
}
