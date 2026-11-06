//go:build container_fs && !windows

package project

import (
	"context"
	"encoding/json"
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

func TestUndoLinkRepairKeepsOtherCloneState(t *testing.T) {
	ctx := context.Background()
	registryPath := filepath.Join(t.TempDir(), "registry.json")
	t.Setenv("CAMP_REGISTRY_PATH", registryPath)
	rootA, rootB := t.TempDir(), t.TempDir()
	for _, root := range []string{rootA, rootB} {
		writeTestCampaignConfig(t, root) // Both clones have the same camp ID.
		if err := os.MkdirAll(filepath.Join(root, "projects"), 0o755); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
			t.Fatalf("git init: %v: %s", err, out)
		}
		if err := os.Symlink("/Volumes/old/voice", filepath.Join(root, "projects", "voice")); err != nil {
			t.Fatal(err)
		}
	}
	candidate := t.TempDir()
	if out, err := exec.Command("git", "-C", candidate, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("candidate git init: %v: %s", err, out)
	}
	preview, err := PreviewLinkRepair(ctx, rootA, "voice", candidate)
	if err != nil {
		t.Fatal(err)
	}
	state, err := ApplyLinkRepair(ctx, rootA, preview)
	if err != nil {
		t.Fatal(err)
	}
	if state.LinkPath == "" {
		t.Fatal("repair state lacks physical link path")
	}
	stateDir := filepath.Join(filepath.Dir(registryPath), "link-repairs")
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	var statePath string
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".json" {
			statePath = filepath.Join(stateDir, entry.Name())
		}
	}
	if statePath == "" {
		t.Fatal("repair state was not saved")
	}
	if _, err := UndoLinkRepair(ctx, rootB, "voice"); err == nil {
		t.Fatal("other clone consumed the repair state")
	}
	if got, err := os.Readlink(filepath.Join(rootA, "projects", "voice")); err != nil || got != candidate {
		t.Fatalf("clone A link = %q, %v; want repaired candidate", got, err)
	}
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("clone B removed clone A undo state: %v", err)
	}
	original, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var changed LinkRepairState
	if err := json.Unmarshal(original, &changed); err != nil {
		t.Fatal(err)
	}
	changed.LinkPath = filepath.Join(rootB, "projects", "voice")
	tampered, err := json.Marshal(changed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := UndoLinkRepair(ctx, rootA, "voice"); err == nil {
		t.Fatal("undo accepted state for another physical link")
	}
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("mismatched state was deleted: %v", err)
	}
	if err := os.WriteFile(statePath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := UndoLinkRepair(ctx, rootA, "voice"); err != nil {
		t.Fatalf("owning clone cannot undo: %v", err)
	}
	if got, err := os.Readlink(filepath.Join(rootA, "projects", "voice")); err != nil || got != "/Volumes/old/voice" {
		t.Fatalf("restored link = %q, %v", got, err)
	}
}
