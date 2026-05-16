package workitem

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests cover the LoadLocalRun -> WorkItem.WorkflowMeta merge in
// discoverWorkflowDocs (009.02 #281 review feedback). LoadLocalRun's parsing
// is exhaustively tested via fstest.MapFS in localrun_fs_test.go; this file
// only exercises the discovery-level wiring (merge happens, malformed runtime
// degrades to nil + warn log + continued discovery, JSON payload carries the
// workflow block when populated). Discovery uses os.ReadDir directly so this
// test uses t.TempDir matching discover_test.go's established pattern.

func TestDiscoverDesign_PopulatesWorkflowMetaFromLocalRun(t *testing.T) {
	root, resolver := setupTestCampaign(t)
	ctx := context.Background()

	dir := filepath.Join(root, "workflow/design/with-runtime")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "README.md"), "# With Runtime\n\n")
	writeFile(t, filepath.Join(dir, ".workitem"), `version: v1alpha5
kind: workitem
id: design-with-runtime-001
type: design
title: With Runtime
`)
	writeFile(t, filepath.Join(dir, ".workflow", "workflow.yaml"), `workflow_id: wf-with-runtime
active_run_id: r1
`)
	writeFile(t, filepath.Join(dir, ".workflow", "runs", "r1", "run.yaml"), `status: active
summary:
  total_steps: 3
`)
	writeFile(t, filepath.Join(dir, ".workflow", "runs", "r1", "progress_events.jsonl"),
		`{"event_type":"workflow_run_started"}
{"event_type":"wf_step_start"}
{"event_type":"wf_step_done"}
{"event_type":"wf_step_start"}
`)

	items, err := discoverDesign(ctx, root, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	got := items[0]
	if got.WorkflowMeta == nil {
		t.Fatal("WorkflowMeta should be populated when .workflow/workflow.yaml exists")
	}
	if got.WorkflowMeta.WorkflowID != "wf-with-runtime" {
		t.Errorf("WorkflowID = %q, want wf-with-runtime", got.WorkflowMeta.WorkflowID)
	}
	if got.WorkflowMeta.TotalSteps != 3 {
		t.Errorf("TotalSteps = %d, want 3", got.WorkflowMeta.TotalSteps)
	}
	if got.WorkflowMeta.CompletedSteps != 1 {
		t.Errorf("CompletedSteps = %d, want 1 (one wf_step_done in events)", got.WorkflowMeta.CompletedSteps)
	}
	if got.WorkflowMeta.CurrentStep != 2 {
		t.Errorf("CurrentStep = %d, want 2 (two wf_step_start in events)", got.WorkflowMeta.CurrentStep)
	}
	if got.WorkflowMeta.RunStatus != "active" {
		t.Errorf("RunStatus = %q, want active", got.WorkflowMeta.RunStatus)
	}
}

func TestDiscoverDesign_MalformedRuntimeDegradesGracefully(t *testing.T) {
	root, resolver := setupTestCampaign(t)
	ctx := context.Background()

	dir := filepath.Join(root, "workflow/design/bad-runtime")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "README.md"), "# Bad Runtime\n")
	writeFile(t, filepath.Join(dir, ".workflow", "workflow.yaml"), "::: not yaml :::\n")

	siblingDir := filepath.Join(root, "workflow/design/clean")
	if err := os.MkdirAll(siblingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(siblingDir, "README.md"), "# Clean\n")

	items, err := discoverDesign(ctx, root, resolver)
	if err != nil {
		t.Fatalf("malformed runtime must not abort discovery: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items (bad runtime kept without WorkflowMeta + clean sibling), got %d", len(items))
	}
	var bad, clean *WorkItem
	for i := range items {
		switch filepath.Base(items[i].RelativePath) {
		case "bad-runtime":
			bad = &items[i]
		case "clean":
			clean = &items[i]
		}
	}
	if bad == nil || clean == nil {
		t.Fatalf("missing expected items: %+v", items)
	}
	if bad.WorkflowMeta != nil {
		t.Errorf("bad-runtime should have nil WorkflowMeta, got %+v", bad.WorkflowMeta)
	}
	if clean.WorkflowMeta != nil {
		t.Errorf("clean sibling should have no WorkflowMeta, got %+v", clean.WorkflowMeta)
	}
}

func TestDiscoverDesign_JSONPayloadCarriesWorkflowBlock(t *testing.T) {
	root, resolver := setupTestCampaign(t)
	ctx := context.Background()

	dir := filepath.Join(root, "workflow/design/payload-check")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "README.md"), "# Payload Check\n")
	writeFile(t, filepath.Join(dir, ".workflow", "workflow.yaml"), `workflow_id: wf-payload
active_run_id: r1
`)
	writeFile(t, filepath.Join(dir, ".workflow", "runs", "r1", "run.yaml"), `status: active
summary:
  total_steps: 5
`)

	items, err := discoverDesign(ctx, root, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	payload := NewPayload(root, items)
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	out := string(raw)
	if !strings.Contains(out, `"workflow":{`) {
		t.Errorf("v1alpha5 payload should carry workflow block, got: %s", out)
	}
	if !strings.Contains(out, `"workflow_id":"wf-payload"`) {
		t.Errorf("payload missing workflow_id, got: %s", out)
	}
	if !strings.Contains(out, `"total_steps":5`) {
		t.Errorf("payload missing total_steps, got: %s", out)
	}
}
