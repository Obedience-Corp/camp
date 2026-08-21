package workitem

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
)

func writeLegacyWorkitem(t *testing.T, root, slug string) {
	t.Helper()
	dir := filepath.Join(root, "workflow", "design", slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "version: v1alpha5\nkind: workitem\nid: design-" + slug + "-2026-05-24\n" +
		"type: design\ntitle: Legacy " + slug + "\n"
	if err := os.WriteFile(filepath.Join(dir, ".workitem"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDoctor_MissingRefIsWarning(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	writeLegacyWorkitem(t, root, "legacy")

	cmd := &cobra.Command{}
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	if err := runDoctor(context.Background(), cmd, false, false); err != nil {
		t.Fatalf("doctor should exit 0 for warnings only: %v", err)
	}
	if !strings.Contains(stdout.String(), codeMissingRefField) {
		t.Fatalf("expected %s finding, got %q", codeMissingRefField, stdout.String())
	}
}

func TestDoctor_FixBackfillsRefAndPreservesFields(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	writeLegacyWorkitem(t, root, "legacy")
	beforePath := filepath.Join(root, "workflow", "design", "legacy", ".workitem")
	before, err := os.ReadFile(beforePath)
	if err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := runDoctor(context.Background(), cmd, false, true); err != nil {
		t.Fatalf("doctor --fix: %v", err)
	}

	after, err := os.ReadFile(beforePath)
	if err != nil {
		t.Fatal(err)
	}

	meta, err := wkitem.LoadMetadata(context.Background(), filepath.Dir(beforePath))
	if err != nil {
		t.Fatal(err)
	}
	if meta.Ref == "" {
		t.Fatalf("ref was not backfilled\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if meta.ID == "" || meta.Type == "" || meta.Title == "" {
		t.Fatalf("backfill clobbered preserved fields: %#v", meta)
	}
	if expected := wkitem.Derive(meta.ID); meta.Ref != expected {
		t.Fatalf("ref %q != Derive(id) %q", meta.Ref, expected)
	}

	expectedBytes := strings.Replace(string(before),
		"id: design-legacy-2026-05-24\n",
		"id: design-legacy-2026-05-24\nref: "+meta.Ref+"\n",
		1,
	)
	if string(after) != expectedBytes {
		t.Fatalf("backfill diff was not minimal\nwant:\n%s\ngot:\n%s", expectedBytes, after)
	}

	// Re-run doctor: no missing-ref findings remain.
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	if err := runDoctor(context.Background(), cmd, false, false); err != nil {
		t.Fatalf("post-fix doctor: %v", err)
	}
	if strings.Contains(stdout.String(), codeMissingRefField) {
		t.Fatalf("missing-ref finding still present after --fix:\n%s", stdout.String())
	}
}

func TestDoctor_FixUpdatesEmptyRefWithoutDuplicateKey(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	dir := filepath.Join(root, "workflow", "design", "empty-ref")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "version: v1alpha5\nkind: workitem\nid: design-empty-ref-2026-05-24\n" +
		"ref: \"\"\ntype: design\ntitle: Empty Ref\n"
	if err := os.WriteFile(filepath.Join(dir, ".workitem"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := runDoctor(context.Background(), cmd, false, true); err != nil {
		t.Fatalf("doctor --fix: %v", err)
	}

	after, err := os.ReadFile(filepath.Join(dir, ".workitem"))
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(after), "\nref:"); count != 1 {
		t.Fatalf("ref key count = %d, want 1\n%s", count, after)
	}
	meta, err := wkitem.LoadMetadata(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Ref == "" {
		t.Fatalf("empty ref was not backfilled:\n%s", after)
	}
}

func TestDoctor_FixHandlesCollisionsAcrossManyWorkitems(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	// Three legacy workitems with very similar IDs to exercise the
	// collision-retry path in DeriveUnique. They cannot actually hash-collide
	// (sha256 is too wide for that on three inputs), but this test pins
	// down the contract that --fix produces a unique ref per workitem and
	// the registry stays internally consistent.
	for _, slug := range []string{"alpha", "beta", "gamma"} {
		writeLegacyWorkitem(t, root, slug)
	}

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := runDoctor(context.Background(), cmd, false, true); err != nil {
		t.Fatalf("doctor --fix: %v", err)
	}

	seen := map[string]string{}
	for _, slug := range []string{"alpha", "beta", "gamma"} {
		meta, err := wkitem.LoadMetadata(context.Background(),
			filepath.Join(root, "workflow", "design", slug))
		if err != nil {
			t.Fatal(err)
		}
		if meta.Ref == "" {
			t.Fatalf("%s: ref was not backfilled", slug)
		}
		if other, dup := seen[meta.Ref]; dup {
			t.Fatalf("ref collision: %s and %s both got %q", slug, other, meta.Ref)
		}
		seen[meta.Ref] = slug
	}
}

func TestDoctorFindingCodeScanFailedStable(t *testing.T) {
	// Discover failures now abort from workitemIDsOnDisk, but the dotted-domain
	// code stays part of workitem-doctor/v1alpha1 so consumers keep a stable set.
	if codeWorkitemScanFailed != "workitem.scan.failed" {
		t.Fatalf("scan-failed code drifted: %s", codeWorkitemScanFailed)
	}
}

func TestWorkitemPathsMissingRef_UsesProvidedItemsNotDiskWalk(t *testing.T) {
	root := t.TempDir()
	writeLegacyWorkitem(t, root, "in-slice")
	writeLegacyWorkitem(t, root, "on-disk-only")
	writeLegacyWorkitem(t, root, "has-ref")
	writeLegacyWorkitem(t, root, "z-later")

	items := []wkitem.WorkItem{
		{RelativePath: "workflow/design/z-later", SourceMetadata: map[string]any{}},
		{RelativePath: "workflow/design/has-ref", SourceMetadata: map[string]any{"ref": "WI-abcdef"}},
		{RelativePath: "workflow/design/no-marker", SourceMetadata: map[string]any{}},
		{RelativePath: "workflow/design/in-slice", SourceMetadata: map[string]any{}},
	}

	got := workitemPathsMissingRef(root, items)
	want := []string{"workflow/design/in-slice", "workflow/design/z-later"}
	if len(got) != len(want) {
		t.Fatalf("missing-ref paths = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("missing-ref paths = %v, want %v", got, want)
		}
	}
}

func TestBackfillMissingRefs_UsesProvidedItemsNotDiskWalk(t *testing.T) {
	root := t.TempDir()
	writeLegacyWorkitem(t, root, "passed")
	writeLegacyWorkitem(t, root, "ignored")

	items := []wkitem.WorkItem{{
		RelativePath:   "workflow/design/passed",
		StableID:       "design-passed-2026-05-24",
		SourceMetadata: map[string]any{},
	}}

	n, failures, err := backfillMissingRefs(context.Background(), root, items)
	if err != nil {
		t.Fatalf("backfillMissingRefs: %v", err)
	}
	if n != 1 {
		t.Fatalf("applied = %d, want 1", n)
	}
	if len(failures) != 0 {
		t.Fatalf("failures = %#v, want none", failures)
	}

	passed, err := wkitem.LoadMetadata(context.Background(), filepath.Join(root, "workflow", "design", "passed"))
	if err != nil {
		t.Fatal(err)
	}
	if passed.Ref == "" {
		t.Fatal("provided item was not backfilled")
	}
	ignored, err := wkitem.LoadMetadata(context.Background(), filepath.Join(root, "workflow", "design", "ignored"))
	if err != nil {
		t.Fatal(err)
	}
	if ignored.Ref != "" {
		t.Fatalf("item omitted from the snapshot was backfilled: %q", ignored.Ref)
	}
}

func TestBackfillMissingRefs_ContextCancelled(t *testing.T) {
	root := t.TempDir()
	writeLegacyWorkitem(t, root, "legacy")
	items := []wkitem.WorkItem{{
		RelativePath:   "workflow/design/legacy",
		StableID:       "design-legacy-2026-05-24",
		SourceMetadata: map[string]any{},
	}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	n, _, err := backfillMissingRefs(ctx, root, items)
	if err == nil {
		t.Fatal("backfillMissingRefs() error = nil, want context cancellation")
	}
	if n != 0 {
		t.Fatalf("applied = %d, want 0", n)
	}
}
