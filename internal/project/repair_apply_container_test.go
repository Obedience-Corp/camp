//go:build container_fs && !windows

package project

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestApplyLinkRepairRejectsChangedPreview(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeTestCampaignConfig(t, root)
	t.Setenv("CAMP_REGISTRY_PATH", filepath.Join(t.TempDir(), "registry.json"))
	if err := os.MkdirAll(filepath.Join(root, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	linkPath := filepath.Join(root, "projects", "voice")
	if err := os.Symlink("/Volumes/old/voice", linkPath); err != nil {
		t.Fatal(err)
	}
	candidate := t.TempDir()
	if out, err := exec.Command("git", "-C", candidate, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("candidate git init: %v: %s", err, out)
	}
	preview, err := PreviewLinkRepair(ctx, root, "voice", candidate)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(linkPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/Volumes/changed/voice", linkPath); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyLinkRepair(ctx, root, preview); err == nil {
		t.Fatal("changed link accepted after preview")
	}
	if got, err := os.Readlink(linkPath); err != nil || got != "/Volumes/changed/voice" {
		t.Fatalf("link = %q, %v", got, err)
	}
	if err := os.Remove(linkPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/Volumes/old/voice", linkPath); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(candidate); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyLinkRepair(ctx, root, preview); err == nil {
		t.Fatal("deleted candidate accepted after preview")
	}
	if got, err := os.Readlink(linkPath); err != nil || got != "/Volumes/old/voice" {
		t.Fatalf("link after candidate deletion = %q, %v", got, err)
	}
}
