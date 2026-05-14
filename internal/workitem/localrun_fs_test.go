package workitem

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"
)

const (
	fsRoot         = "campaign-root"
	fsManifestPath = "campaign-root/.workflow/workflow.yaml"
	fsRunPath      = "campaign-root/.workflow/runs/r1/run.yaml"
	fsEventsPath   = "campaign-root/.workflow/runs/r1/progress_events.jsonl"
)

func mapFSWithManifest(manifest string) fstest.MapFS {
	return fstest.MapFS{fsManifestPath: {Data: []byte(manifest)}}
}

func mapFSWithRun(manifest, run, events string) fstest.MapFS {
	fsys := fstest.MapFS{
		fsManifestPath: {Data: []byte(manifest)},
		fsRunPath:      {Data: []byte(run)},
	}
	if events != "" {
		fsys[fsEventsPath] = &fstest.MapFile{Data: []byte(events)}
	}
	return fsys
}

func TestLoadLocalRunFS_NoWorkflowDir(t *testing.T) {
	got, err := LoadLocalRunFS(context.Background(), fstest.MapFS{}, fsRoot+"/workflow/feature/foo")
	if err != nil {
		t.Fatalf("LoadLocalRunFS: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil progress, got %+v", got)
	}
}

func TestLoadLocalRunFS_ManifestOnly(t *testing.T) {
	fsys := mapFSWithManifest(`workflow_id: wf-test
workitem_id: legacy-test-001
`)
	got, err := LoadLocalRunFS(context.Background(), fsys, fsRoot)
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

func TestLoadLocalRunFS_SummaryAsCache(t *testing.T) {
	fsys := mapFSWithRun(
		`workflow_id: wf-test
active_run_id: r1
`,
		`status: active
summary:
  current_step: 2
  total_steps: 5
  completed_steps: 1
  blocked: false
`,
		"",
	)
	got, err := LoadLocalRunFS(context.Background(), fsys, fsRoot)
	if err != nil {
		t.Fatal(err)
	}
	if got.TotalSteps != 5 {
		t.Errorf("TotalSteps = %d, want 5 (from cache when no events)", got.TotalSteps)
	}
}

func TestLoadLocalRunFS_BasicProgress(t *testing.T) {
	fsys := mapFSWithRun(
		`workflow_id: wf-foo
active_run_id: r1
`,
		`status: active
summary:
  total_steps: 3
`,
		`{"event_type":"workflow_run_started"}
{"event_type":"wf_step_start"}
{"event_type":"wf_step_done"}
{"event_type":"wf_step_start"}
`,
	)
	got, err := LoadLocalRunFS(context.Background(), fsys, fsRoot)
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkflowID != "wf-foo" {
		t.Errorf("WorkflowID = %q, want wf-foo", got.WorkflowID)
	}
	if got.TotalSteps != 3 {
		t.Errorf("TotalSteps = %d, want 3", got.TotalSteps)
	}
	if got.CompletedSteps != 1 {
		t.Errorf("CompletedSteps = %d, want 1", got.CompletedSteps)
	}
	if got.CurrentStep != 2 {
		t.Errorf("CurrentStep = %d, want 2", got.CurrentStep)
	}
}

func TestLoadLocalRunFS_EventReplayOverridesStaleSummary(t *testing.T) {
	fsys := mapFSWithRun(
		`workflow_id: wf-test
active_run_id: r1
`,
		`status: active
summary:
  current_step: 99
  total_steps: 3
  completed_steps: 99
  blocked: false
`,
		`{"event_type":"wf_step_start"}
{"event_type":"wf_step_done"}
`,
	)
	got, err := LoadLocalRunFS(context.Background(), fsys, fsRoot)
	if err != nil {
		t.Fatal(err)
	}
	if got.CompletedSteps != 1 {
		t.Errorf("CompletedSteps = %d, want 1 (events authoritative)", got.CompletedSteps)
	}
}

func TestLoadLocalRunFS_CompletedStatus(t *testing.T) {
	fsys := mapFSWithRun(
		`workflow_id: wf-test
active_run_id: r1
`,
		`status: active
summary:
  total_steps: 1
`,
		`{"event_type":"wf_step_start"}
{"event_type":"wf_step_done"}
{"event_type":"workflow_run_completed"}
`,
	)
	got, err := LoadLocalRunFS(context.Background(), fsys, fsRoot)
	if err != nil {
		t.Fatal(err)
	}
	if got.RunStatus != "completed" {
		t.Errorf("RunStatus = %q, want completed", got.RunStatus)
	}
}

func TestLoadLocalRunFS_BlockedStatus(t *testing.T) {
	fsys := mapFSWithRun(
		`workflow_id: wf-test
active_run_id: r1
`,
		`status: active
summary:
  total_steps: 3
`,
		`{"event_type":"wf_step_start"}
{"event_type":"wf_step_block"}
`,
	)
	got, err := LoadLocalRunFS(context.Background(), fsys, fsRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Blocked || got.RunStatus != "blocked" {
		t.Errorf("Blocked=%v RunStatus=%q, want true/blocked", got.Blocked, got.RunStatus)
	}
}

func TestLoadLocalRunFS_StaleCachedStatusDoesNotOverrideReplay(t *testing.T) {
	fsys := mapFSWithRun(
		`workflow_id: wf-test
active_run_id: r1
`,
		`status: completed
summary:
  total_steps: 3
`,
		`{"event_type":"wf_step_start"}
{"event_type":"wf_step_block"}
`,
	)
	got, err := LoadLocalRunFS(context.Background(), fsys, fsRoot)
	if err != nil {
		t.Fatal(err)
	}
	if got.RunStatus != "blocked" {
		t.Errorf("RunStatus = %q, want blocked (events override stale 'completed')", got.RunStatus)
	}
	if !got.Blocked {
		t.Errorf("Blocked should be true from wf_step_block event")
	}
}

func TestLoadLocalRunFS_SkipEventClearsBlocked(t *testing.T) {
	fsys := mapFSWithRun(
		`workflow_id: wf-test
active_run_id: r1
`,
		`status: active
summary:
  total_steps: 3
`,
		`{"event_type":"wf_step_start"}
{"event_type":"wf_step_block"}
{"event_type":"wf_step_skip"}
`,
	)
	got, err := LoadLocalRunFS(context.Background(), fsys, fsRoot)
	if err != nil {
		t.Fatal(err)
	}
	if got.Blocked {
		t.Errorf("Blocked should be cleared by wf_step_skip")
	}
	if got.RunStatus != "active" {
		t.Errorf("RunStatus = %q, want active after skip clears block", got.RunStatus)
	}
}

func TestLoadLocalRunFS_CreatedEventIsNoop(t *testing.T) {
	fsys := mapFSWithRun(
		`workflow_id: wf-test
active_run_id: r1
`,
		`status: active
summary:
  total_steps: 2
`,
		`{"event_type":"workflow_run_created"}
{"event_type":"workflow_run_started"}
{"event_type":"wf_step_start"}
`,
	)
	got, err := LoadLocalRunFS(context.Background(), fsys, fsRoot)
	if err != nil {
		t.Fatal(err)
	}
	if got.CurrentStep != 1 {
		t.Errorf("workflow_run_created should be noop; CurrentStep = %d, want 1", got.CurrentStep)
	}
}

func TestLoadLocalRunFS_LegacyWorkitemIDIgnored(t *testing.T) {
	fsys := mapFSWithRun(
		`workflow_id: wf-foo
workitem_id: legacy-feature-foo-2026-01-01
active_run_id: r1
`,
		`status: active
workitem_id: legacy-feature-foo-2026-01-01
summary:
  total_steps: 1
`,
		"",
	)
	got, err := LoadLocalRunFS(context.Background(), fsys, fsRoot)
	if err != nil {
		t.Fatalf("legacy workitem_id should parse silently: %v", err)
	}
	if got == nil || got.WorkflowID != "wf-foo" {
		t.Errorf("expected progress with WorkflowID=wf-foo, got %+v", got)
	}
}

func TestLoadLocalRunFS_MalformedYAML(t *testing.T) {
	fsys := mapFSWithManifest("::: not yaml :::\n")
	_, err := LoadLocalRunFS(context.Background(), fsys, fsRoot)
	if err == nil {
		t.Fatal("expected parse error for malformed yaml")
	}
	if !strings.Contains(err.Error(), "parsing") {
		t.Errorf("error should mention parsing, got %v", err)
	}
}

func TestLoadLocalRunFS_DocHashDrift(t *testing.T) {
	fsys := fstest.MapFS{
		fsManifestPath: {Data: []byte(`workflow_id: wf-test
doc_path: WORKFLOW.md
doc_hash: sha256:not_the_actual_hash
`)},
		fsRoot + "/WORKFLOW.md": {Data: []byte("original")},
	}
	got, err := LoadLocalRunFS(context.Background(), fsys, fsRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !got.DocHashChanged {
		t.Errorf("expected DocHashChanged=true on hash mismatch")
	}
}

// TestLoadLocalRun_HostWrapperSmoke is the single retained host-tempdir
// case: it verifies the os.DirFS("/")+TrimPrefix translation in the
// production wrapper does not regress. All replay/state correctness
// lives in the MapFS-driven cases above.
func TestLoadLocalRun_HostWrapperSmoke(t *testing.T) {
	dir := t.TempDir()
	manifestDir := dir + "/.workflow"
	if err := writeHostFile(manifestDir+"/workflow.yaml", "workflow_id: wf-host\n"); err != nil {
		t.Fatal(err)
	}
	got, err := LoadLocalRun(context.Background(), dir)
	if err != nil {
		t.Fatalf("LoadLocalRun host wrapper: %v", err)
	}
	if got == nil || got.WorkflowID != "wf-host" {
		t.Errorf("expected WorkflowID=wf-host via host wrapper, got %+v", got)
	}
}
