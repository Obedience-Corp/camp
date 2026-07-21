package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/camp/internal/workitem/links"
)

func writeCommitContextCampaign(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".campaign"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".campaign", "campaign.yaml"),
		[]byte("version: campaign/v1\nid: test-campaign\nname: Test\ntype: product\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestResolveCommitContext_GracefulOutsideCampaign(t *testing.T) {
	tmp := t.TempDir()
	// Set a working directory outside any campaign so the resolver returns
	// (none, none).
	cur, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cur) }()
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}

	questID, festivalRef, ref := resolveCommitContext(context.Background(), filepath.Join(tmp, "no-campaign"), "")
	if questID != "" || festivalRef != "" || ref != "" {
		t.Fatalf("expected empty strings for non-campaign root, got quest=%q festival=%q ref=%q", questID, festivalRef, ref)
	}
}

func TestResolveCommitContext_DoesNotInheritCurrentWorkitem(t *testing.T) {
	root := writeCommitContextCampaign(t)
	wiDir := filepath.Join(root, "workflow", "design", "example")
	if err := os.MkdirAll(wiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wiDir, ".workitem"), []byte(
		"version: v1alpha5\nkind: workitem\nid: design-example-2026-05-24\ntype: design\ntitle: Example\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := links.SaveCurrent(context.Background(), root, &links.Current{
		Version:    links.CurrentSchemaVersion,
		WorkitemID: "design-example-2026-05-24",
	}); err != nil {
		t.Fatal(err)
	}

	gotQuest, gotFestival, gotWorkitem := resolveCommitContext(context.Background(), root, "")
	if gotQuest != "" || gotFestival != "" || gotWorkitem != "" {
		t.Fatalf("commit context inherited current workitem: quest=%q festival=%q workitem=%q", gotQuest, gotFestival, gotWorkitem)
	}
}
