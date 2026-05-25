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

	// Title text from the legacy fixture should survive byte-identical to the
	// original (other than the inserted ref line) — verify by string contains.
	if !strings.Contains(string(after), "title: Legacy legacy") {
		t.Fatalf("title field clobbered:\n%s", after)
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
