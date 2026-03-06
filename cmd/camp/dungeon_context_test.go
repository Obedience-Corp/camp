package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func writeTestCampaignConfig(t *testing.T, root string) {
	t.Helper()
	campaignDir := filepath.Join(root, ".campaign")
	if err := os.MkdirAll(campaignDir, 0755); err != nil {
		t.Fatalf("mkdir .campaign: %v", err)
	}
	content := []byte("name: test-campaign\ntype: product\n")
	if err := os.WriteFile(filepath.Join(campaignDir, "campaign.yaml"), content, 0644); err != nil {
		t.Fatalf("write campaign.yaml: %v", err)
	}
}

func TestResolveDungeonCommandContext_UsesNearestDungeon(t *testing.T) {
	root := t.TempDir()
	writeTestCampaignConfig(t, root)

	nested := filepath.Join(root, "workflow", "design", "api")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	nearestParent := filepath.Join(root, "workflow", "design")
	if err := os.MkdirAll(filepath.Join(nearestParent, "dungeon"), 0755); err != nil {
		t.Fatalf("mkdir nearest dungeon: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "dungeon"), 0755); err != nil {
		t.Fatalf("mkdir root dungeon: %v", err)
	}

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(nested); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	got, err := resolveDungeonCommandContext(context.Background())
	if err != nil {
		t.Fatalf("resolveDungeonCommandContext() error = %v", err)
	}

	wantDungeon, err := filepath.EvalSymlinks(filepath.Join(nearestParent, "dungeon"))
	if err != nil {
		t.Fatalf("eval want dungeon: %v", err)
	}
	wantParent, err := filepath.EvalSymlinks(nearestParent)
	if err != nil {
		t.Fatalf("eval want parent: %v", err)
	}

	if got.Dungeon.DungeonPath != wantDungeon {
		t.Fatalf("DungeonPath = %q, want %q", got.Dungeon.DungeonPath, wantDungeon)
	}
	if got.Dungeon.ParentPath != wantParent {
		t.Fatalf("ParentPath = %q, want %q", got.Dungeon.ParentPath, wantParent)
	}
}

func TestResolveDungeonCommandContext_NoDungeon(t *testing.T) {
	root := t.TempDir()
	writeTestCampaignConfig(t, root)

	cwd := filepath.Join(root, "docs", "api")
	if err := os.MkdirAll(cwd, 0755); err != nil {
		t.Fatalf("mkdir cwd: %v", err)
	}

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	_, err = resolveDungeonCommandContext(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
