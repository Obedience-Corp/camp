package workitem

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func writeFileT(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadLocalRun_NoRuntime(t *testing.T) {
	dir := t.TempDir()
	got, err := LoadLocalRun(context.Background(), dir)
	if err != nil {
		t.Fatalf("missing .workflow should not error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestLoadLocalRun_ManifestOnly(t *testing.T) {
	dir := t.TempDir()
	writeFileT(t, filepath.Join(dir, ".workflow", "workflow.yaml"), `version: 1
kind: workflow-runtime
workflow_id: wf-test
workitem_id: test-001
`)
	got, err := LoadLocalRun(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.WorkflowID != "wf-test" {
		t.Errorf("expected WorkflowID populated: %+v", got)
	}
	if got.CurrentStep != 0 || got.TotalSteps != 0 {
		t.Errorf("expected zero progress with no active run: %+v", got)
	}
}

func TestLoadLocalRun_SummaryAsCache(t *testing.T) {
	dir := t.TempDir()
	writeFileT(t, filepath.Join(dir, ".workflow", "workflow.yaml"), `version: 1
kind: workflow-runtime
workflow_id: wf-test
workitem_id: test-001
active_run_id: run-001
`)
	writeFileT(t, filepath.Join(dir, ".workflow", "runs", "run-001", "run.yaml"), `version: 1
kind: workflow-run
run_id: run-001
status: active
summary:
  current_step: 2
  total_steps: 6
  completed_steps: 1
  blocked: false
`)
	// Empty events file: replay sees no events, returns zero CurrentStep.
	writeFileT(t, filepath.Join(dir, ".workflow", "runs", "run-001", "progress_events.jsonl"), "")

	got, err := LoadLocalRun(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.TotalSteps != 6 {
		t.Errorf("TotalSteps from cache = %d, want 6", got.TotalSteps)
	}
}

func TestLoadLocalRun_EventReplayOverridesStaleSummary(t *testing.T) {
	dir := t.TempDir()
	writeFileT(t, filepath.Join(dir, ".workflow", "workflow.yaml"), `version: 1
kind: workflow-runtime
workflow_id: wf-test
workitem_id: test-001
active_run_id: run-001
`)
	writeFileT(t, filepath.Join(dir, ".workflow", "runs", "run-001", "run.yaml"), `version: 1
kind: workflow-run
run_id: run-001
status: active
summary:
  current_step: 999
  total_steps: 6
  completed_steps: 999
  blocked: false
`)
	writeFileT(t, filepath.Join(dir, ".workflow", "runs", "run-001", "progress_events.jsonl"), `{"event_type":"wf_step_start"}
{"event_type":"wf_step_done"}
{"event_type":"wf_step_start"}
`)

	got, err := LoadLocalRun(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.CurrentStep != 2 {
		t.Errorf("CurrentStep from replay = %d, want 2 (events: 2 step_start)", got.CurrentStep)
	}
	if got.CompletedSteps != 1 {
		t.Errorf("CompletedSteps from replay = %d, want 1", got.CompletedSteps)
	}
}

func TestLoadLocalRun_CompletedStatus(t *testing.T) {
	dir := t.TempDir()
	writeFileT(t, filepath.Join(dir, ".workflow", "workflow.yaml"), `version: 1
kind: workflow-runtime
workflow_id: wf-test
workitem_id: test-001
active_run_id: run-001
`)
	writeFileT(t, filepath.Join(dir, ".workflow", "runs", "run-001", "run.yaml"), `version: 1
kind: workflow-run
run_id: run-001
status: active
summary:
  total_steps: 1
`)
	writeFileT(t, filepath.Join(dir, ".workflow", "runs", "run-001", "progress_events.jsonl"), `{"event_type":"wf_step_start"}
{"event_type":"wf_step_done"}
{"event_type":"workflow_run_completed"}
`)
	got, err := LoadLocalRun(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.RunStatus != "completed" {
		t.Errorf("RunStatus = %q, want completed", got.RunStatus)
	}
}

func TestLoadLocalRun_BlockedStatus(t *testing.T) {
	dir := t.TempDir()
	writeFileT(t, filepath.Join(dir, ".workflow", "workflow.yaml"), `version: 1
kind: workflow-runtime
workflow_id: wf-test
workitem_id: test-001
active_run_id: run-001
`)
	writeFileT(t, filepath.Join(dir, ".workflow", "runs", "run-001", "run.yaml"), `version: 1
kind: workflow-run
run_id: run-001
status: active
summary:
  total_steps: 1
`)
	writeFileT(t, filepath.Join(dir, ".workflow", "runs", "run-001", "progress_events.jsonl"), `{"event_type":"wf_step_start"}
{"event_type":"wf_step_block"}
`)
	got, err := LoadLocalRun(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Blocked || got.RunStatus != "blocked" {
		t.Errorf("Blocked=%v RunStatus=%q, want true/blocked", got.Blocked, got.RunStatus)
	}
}

func TestLoadLocalRun_MalformedManifestErrors(t *testing.T) {
	dir := t.TempDir()
	writeFileT(t, filepath.Join(dir, ".workflow", "workflow.yaml"), "not: valid: yaml: ::\n")
	_, err := LoadLocalRun(context.Background(), dir)
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestLoadLocalRun_DocHashDrift(t *testing.T) {
	dir := t.TempDir()
	// Write WORKFLOW.md and capture its hash by computing it here.
	wfDoc := filepath.Join(dir, "WORKFLOW.md")
	writeFileT(t, wfDoc, "original")
	// Use a different hash to simulate drift.
	writeFileT(t, filepath.Join(dir, ".workflow", "workflow.yaml"), `version: 1
kind: workflow-runtime
workflow_id: wf-test
workitem_id: test-001
doc_path: WORKFLOW.md
doc_hash: sha256:not_the_actual_hash
`)
	got, err := LoadLocalRun(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if !got.DocHashChanged {
		t.Errorf("expected DocHashChanged=true on hash mismatch")
	}
}
