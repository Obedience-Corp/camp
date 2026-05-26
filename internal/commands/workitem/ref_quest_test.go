package workitem

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
)

// refQuestTestCampaign builds a minimal campaign root.
func refQuestTestCampaign(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".campaign"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".campaign", "campaign.yaml"),
		[]byte("id: test\nname: Test\ntype: product\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func loadMarker(t *testing.T, path string) wkitem.Metadata {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var meta wkitem.Metadata
	if err := yaml.Unmarshal(data, &meta); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	return meta
}

func TestRunCreate_WritesRefAndOmitsQuestWhenNoneActive(t *testing.T) {
	root := refQuestTestCampaign(t)
	restore := chdir(t, root)
	defer restore()
	// Ensure no CAMP_QUEST env var leaks into the test.
	t.Setenv("CAMP_QUEST", "")

	cmd := &cobra.Command{}
	cmd.SetOut(os.Stdout)
	cmd.SetErr(os.Stderr)

	if err := runCreate(context.Background(), cmd, "alpha", "design", "Alpha", "", "", "", false); err != nil {
		t.Fatalf("runCreate: %v", err)
	}
	meta := loadMarker(t, filepath.Join(root, "workflow", "design", "alpha", ".workitem"))
	if meta.Version != wkitem.WorkitemSchemaVersion {
		t.Fatalf("version = %q, want %q", meta.Version, wkitem.WorkitemSchemaVersion)
	}
	if meta.Ref == "" {
		t.Fatal("expected ref to be set on create")
	}
	if !strings.HasPrefix(meta.Ref, "WI-") || len(meta.Ref) != 9 {
		t.Fatalf("ref %q has unexpected shape", meta.Ref)
	}
	if meta.QuestID != "" {
		t.Fatalf("expected empty quest_id with no active quest, got %q", meta.QuestID)
	}
	if meta.ID != "design-alpha-"+meta.ID[len("design-alpha-"):] {
		t.Fatalf("id = %q (sanity check)", meta.ID)
	}
	if expected := wkitem.Derive(meta.ID); meta.Ref != expected {
		t.Fatalf("ref %q != Derive(id) %q", meta.Ref, expected)
	}
}

func TestRunCreate_RefsAreUniqueAcrossWorkitems(t *testing.T) {
	root := refQuestTestCampaign(t)
	restore := chdir(t, root)
	defer restore()
	t.Setenv("CAMP_QUEST", "")

	cmd := &cobra.Command{}
	cmd.SetOut(os.Stdout)
	cmd.SetErr(os.Stderr)

	for _, slug := range []string{"alpha", "beta", "gamma"} {
		if err := runCreate(context.Background(), cmd, slug, "design", slug, "", "", "", false); err != nil {
			t.Fatalf("runCreate %s: %v", slug, err)
		}
	}
	seen := make(map[string]bool)
	for _, slug := range []string{"alpha", "beta", "gamma"} {
		meta := loadMarker(t, filepath.Join(root, "workflow", "design", slug, ".workitem"))
		if seen[meta.Ref] {
			t.Fatalf("duplicate ref across workitems: %q", meta.Ref)
		}
		seen[meta.Ref] = true
	}
}

func TestRunAdopt_WritesRef(t *testing.T) {
	root := refQuestTestCampaign(t)
	restore := chdir(t, root)
	defer restore()
	t.Setenv("CAMP_QUEST", "")

	adoptDir := filepath.Join(root, "workflow", "design", "legacy")
	if err := os.MkdirAll(adoptDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{}
	cmd.SetOut(os.Stdout)
	cmd.SetErr(os.Stderr)
	if err := runAdopt(context.Background(), cmd, "workflow/design/legacy", "design", "Legacy", "", ""); err != nil {
		t.Fatalf("runAdopt: %v", err)
	}
	meta := loadMarker(t, filepath.Join(adoptDir, ".workitem"))
	if meta.Ref == "" {
		t.Fatal("expected ref on adopt")
	}
	if expected := wkitem.Derive(meta.ID); meta.Ref != expected {
		t.Fatalf("ref %q != Derive(id) %q", meta.Ref, expected)
	}
}

func TestLoadMetadata_LegacyV1Alpha5StillLoads(t *testing.T) {
	root := refQuestTestCampaign(t)
	dir := filepath.Join(root, "workflow", "design", "legacy")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	const legacy = `version: v1alpha5
kind: workitem
id: design-legacy-2026-05-24
type: design
title: Legacy
`
	if err := os.WriteFile(filepath.Join(dir, ".workitem"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	meta, err := wkitem.LoadMetadata(context.Background(), dir)
	if err != nil {
		t.Fatalf("LoadMetadata legacy: %v", err)
	}
	if meta == nil || meta.Version != "v1alpha5" {
		t.Fatalf("legacy load returned %#v", meta)
	}
	if meta.Ref != "" || meta.QuestID != "" {
		t.Fatalf("legacy meta should have empty Ref/QuestID, got %#v", meta)
	}
}
