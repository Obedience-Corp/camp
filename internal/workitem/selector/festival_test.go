package selector

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func writeFestivalCampaign(t *testing.T, festDir, festID string) string {
	t.Helper()
	root := t.TempDir()

	mustWriteFile(t, filepath.Join(root, ".campaign", "campaign.yaml"),
		"version: campaign/v1\nid: testcampaign\nname: test\ntype: product\n")

	festYAML := "version: fest/v1\nmetadata:\n  id: " + festID + "\n  name: " + festDir + "\n  festival_type: standard\n"
	mustWriteFile(t, filepath.Join(root, "festivals", "planning", festDir, "fest.yaml"), festYAML)
	mustWriteFile(t, filepath.Join(root, "festivals", "planning", festDir, "FESTIVAL_GOAL.md"), "# goal\n")
	return root
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolve_FestivalByFestYAMLID(t *testing.T) {
	root := writeFestivalCampaign(t, "sync-clone-transport-SC0001", "SC0001")

	for _, query := range []string{"SC0001", "sc0001"} {
		wi, err := Resolve(context.Background(), root, query, ResolveOptions{})
		if err != nil {
			t.Fatalf("Resolve(%q) error: %v", query, err)
		}
		if wi.WorkflowType != "festival" {
			t.Fatalf("Resolve(%q) resolved a %q, want festival", query, wi.WorkflowType)
		}
		if wi.SourceID != "SC0001" {
			t.Fatalf("Resolve(%q) SourceID = %q, want SC0001", query, wi.SourceID)
		}
	}
}

func TestResolve_UnknownFestivalIDIsNotFound(t *testing.T) {
	root := writeFestivalCampaign(t, "sync-clone-transport-SC0001", "SC0001")

	if _, err := Resolve(context.Background(), root, "SC9999", ResolveOptions{}); err == nil {
		t.Fatal("Resolve(unknown festival id) should error")
	}
}
