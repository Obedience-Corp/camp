package workitem

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverCustomWorkflowTypes_MarkerGated(t *testing.T) {
	root, resolver := setupTestCampaign(t)
	ctx := context.Background()

	withMarker := filepath.Join(root, "workflow/feature/created-via-cli")
	if err := os.MkdirAll(withMarker, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(withMarker, ".workitem"), `version: v1alpha5
kind: workitem
id: feature-created-via-cli-001
type: feature
title: Created Via CLI
`)

	withoutMarker := filepath.Join(root, "workflow/feature/legacy-no-marker")
	if err := os.MkdirAll(withoutMarker, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(withoutMarker, "README.md"), "# Legacy\n")

	customMarker := filepath.Join(root, "workflow/incident/p99-spike")
	if err := os.MkdirAll(customMarker, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(customMarker, ".workitem"), `version: v1alpha5
kind: workitem
id: incident-p99-spike-001
type: incident
title: P99 Spike
`)

	items, err := discoverCustomWorkflowTypes(ctx, root, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items (marker-gated), got %d: %+v", len(items), items)
	}

	byType := map[WorkflowType]WorkItem{}
	for _, it := range items {
		byType[it.WorkflowType] = it
	}
	if _, ok := byType[WorkflowType("feature")]; !ok {
		t.Errorf("missing feature item; got %+v", items)
	}
	if _, ok := byType[WorkflowType("incident")]; !ok {
		t.Errorf("missing incident item; got %+v", items)
	}
}

func TestDiscoverCustomWorkflowTypes_SkipsBuiltinAndDungeon(t *testing.T) {
	root, resolver := setupTestCampaign(t)
	ctx := context.Background()

	designWithMarker := filepath.Join(root, "workflow/design/already-handled")
	if err := os.MkdirAll(designWithMarker, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(designWithMarker, ".workitem"), `version: v1alpha5
kind: workitem
id: design-already-handled-001
type: design
title: Already Handled
`)

	dungeonDir := filepath.Join(root, "workflow/dungeon/old-thing")
	if err := os.MkdirAll(dungeonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dungeonDir, ".workitem"), `version: v1alpha5
kind: workitem
id: x
type: dungeon
title: X
`)

	items, err := discoverCustomWorkflowTypes(ctx, root, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Errorf("expected 0 items (builtin/dungeon skipped), got %d: %+v", len(items), items)
	}
}

func TestDiscover_FullWiresCustomTypes(t *testing.T) {
	root, resolver := setupTestCampaign(t)
	ctx := context.Background()

	created := filepath.Join(root, "workflow/feature/integrated-create")
	if err := os.MkdirAll(created, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(created, ".workitem"), `version: v1alpha5
kind: workitem
id: feature-integrated-create-001
type: feature
title: Integrated Create
`)

	items, err := Discover(ctx, root, resolver)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, it := range items {
		if it.RelativePath == "workflow/feature/integrated-create" {
			found = true
			if it.WorkflowType != WorkflowType("feature") {
				t.Errorf("WorkflowType = %q, want feature", it.WorkflowType)
			}
			if it.StableID != "feature-integrated-create-001" {
				t.Errorf("StableID = %q, want feature-integrated-create-001", it.StableID)
			}
		}
	}
	if !found {
		t.Errorf("created custom-type workitem not surfaced by top-level Discover; got %+v", items)
	}
}
