package workitem

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
)

// TestResolveSweepLocation_WorkflowHome locks today's pass-through behavior:
// a design item resolves to its type-local dungeon at workflow/design/dungeon,
// outside a dungeon. When phase 3 adds festivals/ support to DetectFromCwd this
// baseline stays valid and the festivals/ negative case below flips; that is
// the visible seam this task exists to create.
func TestResolveSweepLocation_WorkflowHome(t *testing.T) {
	root := t.TempDir()
	itemRel := filepath.Join("workflow", "design", "some-item")
	if err := os.MkdirAll(filepath.Join(root, itemRel), 0o755); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}

	loc, err := resolveSweepLocation(root, wkitem.WorkItem{RelativePath: itemRel})
	if err != nil {
		t.Fatalf("resolveSweepLocation: %v", err)
	}
	if loc.InDungeon {
		t.Errorf("InDungeon = true, want false for a live workflow home")
	}
	wantDungeon := filepath.Join(root, "workflow", "design", "dungeon")
	// EvalSymlinks resolves the temp root (macOS /var -> /private/var), so
	// compare on the resolved root rather than the raw t.TempDir() value.
	if resolved, rerr := filepath.EvalSymlinks(root); rerr == nil {
		wantDungeon = filepath.Join(resolved, "workflow", "design", "dungeon")
	}
	if loc.DungeonPath != wantDungeon {
		t.Errorf("DungeonPath = %q, want %q", loc.DungeonPath, wantDungeon)
	}
}

// TestResolveSweepLocation_FestivalsHomeNotYetSupported locks the current
// error for a festivals/ path. No sweep candidate has such a RelativePath
// before phase 3 (PlanSweep excludes festivals), so this asserts the baseline
// that phase 3 must deliberately change when it teaches DetectFromCwd about
// festivals/ homes. Asserting the exact message catches a silent behavior drift.
func TestResolveSweepLocation_FestivalsHomeNotYetSupported(t *testing.T) {
	root := t.TempDir()
	itemRel := filepath.Join("festivals", "ready", "foo-item")
	if err := os.MkdirAll(filepath.Join(root, itemRel), 0o755); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}

	_, err := resolveSweepLocation(root, wkitem.WorkItem{RelativePath: itemRel})
	if err == nil {
		t.Fatal("expected error for festivals/ home, got nil")
	}
	const want = "not inside a workitem; cwd must be under workflow/<type>/<slug>/"
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

func TestWorkitemSweepJSONVersion(t *testing.T) {
	if WorkitemSweepJSONVersion != "workitem-sweep/v1alpha1" {
		t.Errorf("WorkitemSweepJSONVersion = %q, want workitem-sweep/v1alpha1", WorkitemSweepJSONVersion)
	}
}

// TestSweepPlanEnvelopeShape builds the dry-run plan from a constructed
// []SweepCandidate (no discovery pass) and asserts the --json envelope's field
// names and per-item mapping, so a contract change is a deliberate edit. Uses a
// temp root only for DetectFromCwd's symlink/path resolution; it mutates
// nothing (creates empty dirs so EvalSymlinks resolves).
func TestSweepPlanEnvelopeShape(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "workflow", "design", "alpha"), 0o755); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}
	candidates := []wkitem.SweepCandidate{{
		Item: wkitem.WorkItem{
			Key:          "design:workflow/design/alpha",
			WorkflowType: wkitem.WorkflowTypeDesign,
			RelativePath: "workflow/design/alpha",
		},
		Reason:      wkitem.EvidenceWorkflowRunCompleted,
		ActiveRunID: "run-007",
	}}

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	result := workitemSweepResult{SchemaVersion: WorkitemSweepJSONVersion, DryRun: true, Candidates: len(candidates)}
	if err := emitSweepPlan(cmd, root, candidates, &result, true); err != nil {
		t.Fatalf("emitSweepPlan: %v", err)
	}

	// Field-name contract: assert the raw JSON keys, not just the decoded shape.
	raw := buf.String()
	for _, key := range []string{
		`"schema_version"`, `"dry_run"`, `"candidates"`, `"items"`,
		`"id"`, `"type"`, `"from"`, `"to"`, `"evidence"`, `"active_run_id"`,
	} {
		if !bytes.Contains(buf.Bytes(), []byte(key)) {
			t.Errorf("envelope missing key %s in output:\n%s", key, raw)
		}
	}

	var decoded workitemSweepResult
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("unmarshal envelope: %v\n%s", err, raw)
	}
	if decoded.SchemaVersion != WorkitemSweepJSONVersion {
		t.Errorf("schema_version = %q, want %q", decoded.SchemaVersion, WorkitemSweepJSONVersion)
	}
	if !decoded.DryRun || decoded.Candidates != 1 {
		t.Errorf("envelope top-level fields wrong: %+v", decoded)
	}
	if len(decoded.Items) != 1 {
		t.Fatalf("want 1 item, got %d", len(decoded.Items))
	}
	it := decoded.Items[0]
	if it.Type != "design" || it.From != "workflow/design/alpha" ||
		it.Evidence != wkitem.EvidenceWorkflowRunCompleted || it.ActiveRunID != "run-007" {
		t.Errorf("item did not map from candidate: %+v", it)
	}
	if it.To == "" {
		t.Errorf("dry-run item should carry a destination, got empty")
	}
}
