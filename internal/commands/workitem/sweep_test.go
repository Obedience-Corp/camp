package workitem

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
)

// TestExecuteSweepCandidates_ContextCancelledPropagates locks that a mid-sweep
// context cancellation is surfaced, never swallowed into a clean success.
func TestExecuteSweepCandidates_ContextCancelledPropagates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var result workitemSweepResult
	candidates := []wkitem.SweepCandidate{{
		Item:   wkitem.WorkItem{WorkflowType: wkitem.WorkflowTypeDesign, RelativePath: "workflow/design/foo"},
		Reason: wkitem.EvidenceWorkflowRunCompleted,
	}}

	err := executeSweepCandidates(ctx, &cobra.Command{}, &config.CampaignConfig{}, t.TempDir(), candidates, &result)
	if err == nil {
		t.Fatal("expected context cancellation to propagate, got nil")
	}
	if result.Swept != 0 || result.Committed {
		t.Errorf("cancelled sweep must not report success: swept=%d committed=%v", result.Swept, result.Committed)
	}
}

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

// TestResolveSweepLocation_FestivalsHomeRejectsUnstamped is the phase 3 half of
// what used to be TestResolveSweepLocation_FestivalsHomeNotYetSupported. A
// festivals/ path is now a resolvable home, but only when the directory carries
// a .workitem marker: a bare directory sitting in a lifecycle folder is not a
// resident camp can move, and must say so rather than resolve to a guess.
func TestResolveSweepLocation_FestivalsHomeRejectsUnstamped(t *testing.T) {
	root := t.TempDir()
	itemRel := filepath.Join("festivals", "ready", "foo-item")
	if err := os.MkdirAll(filepath.Join(root, itemRel), 0o755); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}

	_, err := resolveSweepLocation(root, wkitem.WorkItem{RelativePath: itemRel})
	if err == nil {
		t.Fatal("expected error for unstamped festivals/ home, got nil")
	}
	const want = "not a lifecycle resident; festivals/ready/foo-item has no .workitem marker (run camp workitem doctor)"
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

// TestResolveSweepLocation_FestivalsResidentHome locks the destination rule that
// sequence 04 depends on: a stamped resident sweeps into the festival-local
// dungeon, not back into the dungeon of the workflow type it came from.
func TestResolveSweepLocation_FestivalsResidentHome(t *testing.T) {
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	itemRel := filepath.Join("festivals", "active", "foo-item")
	dir := filepath.Join(root, itemRel)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}
	marker := "version: v1alpha8\nkind: workitem\nid: design-foo-item-2026-07-24\ntype: design\ntitle: Foo\n"
	if err := os.WriteFile(filepath.Join(dir, ".workitem"), []byte(marker), 0o644); err != nil {
		t.Fatalf("write .workitem: %v", err)
	}

	loc, err := resolveSweepLocation(root, wkitem.WorkItem{RelativePath: itemRel})
	if err != nil {
		t.Fatalf("resolveSweepLocation: %v", err)
	}
	if loc.Type != "design" {
		t.Errorf("Type = %q, want design (from the marker, not the path)", loc.Type)
	}
	if want := filepath.Join(root, "festivals", "active"); loc.ParentPath != want {
		t.Errorf("ParentPath = %q, want %q", loc.ParentPath, want)
	}
	if want := filepath.Join(root, "festivals", ".dungeon"); loc.DungeonPath != want {
		t.Errorf("DungeonPath = %q, want %q (residents complete into the festival-local dungeon)", loc.DungeonPath, want)
	}
	if loc.InDungeon {
		t.Error("InDungeon = true, want false for a working-stage resident")
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
		Reason: wkitem.EvidenceWorkflowRunCompleted,
		RunID:  "run-007",
	}}

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	result := workitemSweepResult{SchemaVersion: WorkitemSweepJSONVersion, DryRun: true, Candidates: len(candidates)}
	fillSweepPlan(root, candidates, &result)
	if err := emitSweepResult(cmd, &result, true); err != nil {
		t.Fatalf("emitSweepResult: %v", err)
	}

	// Field-name contract: assert the raw JSON keys, not just the decoded shape.
	raw := buf.String()
	for _, key := range []string{
		`"schema_version"`, `"dry_run"`, `"candidates"`, `"items"`,
		`"id"`, `"type"`, `"from"`, `"to"`, `"evidence"`, `"run_id"`,
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
		it.Evidence != wkitem.EvidenceWorkflowRunCompleted || it.RunID != "run-007" {
		t.Errorf("item did not map from candidate: %+v", it)
	}
	if it.To == "" {
		t.Errorf("dry-run item should carry a destination, got empty")
	}
}
