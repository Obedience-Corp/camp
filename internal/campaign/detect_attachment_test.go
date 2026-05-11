package campaign

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestDetect_AttachmentMarker_ResolvesViaRegistry is part of the
// non-project-symlinks design. A directory outside the campaign tree that
// carries a Kind="attachment" marker should resolve to the campaign root via
// the registry, exactly as a linked-project marker does today.
//
// This test sets CAMP_REGISTRY_PATH to an isolated registry file and writes a
// V3 attachment marker into an external directory.
func TestDetect_AttachmentMarker_ResolvesViaRegistry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink test not supported on Windows")
	}

	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	// Real campaign root.
	campaignRoot := filepath.Join(tmpDir, "real-campaign")
	if err := os.MkdirAll(filepath.Join(campaignRoot, CampaignDir), 0755); err != nil {
		t.Fatalf("mkdir campaign: %v", err)
	}

	// Isolated registry file with this campaign registered.
	regPath := filepath.Join(tmpDir, "registry.json")
	regData := []byte(`{
  "version": 1,
  "campaigns": {
    "campaign-xyz": {
      "name": "real-campaign",
      "path": "` + campaignRoot + `"
    }
  }
}`)
	if err := os.WriteFile(regPath, regData, 0644); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	t.Setenv("CAMP_REGISTRY_PATH", regPath)

	// External directory with attachment marker pointing at campaign-xyz.
	external := filepath.Join(tmpDir, "external", "some-dir")
	if err := os.MkdirAll(external, 0755); err != nil {
		t.Fatalf("mkdir external: %v", err)
	}

	marker := LinkMarker{
		Version:          3,
		Kind:             "attachment",
		ActiveCampaignID: "campaign-xyz",
	}
	if err := WriteMarker(external, marker); err != nil {
		t.Fatalf("WriteMarker: %v", err)
	}

	// Detection from inside the attached external directory should resolve
	// to the campaign root.
	ClearCache()
	got, err := Detect(context.Background(), external)
	if err != nil {
		t.Fatalf("Detect from attached dir: %v", err)
	}
	if got != campaignRoot {
		t.Errorf("Detect = %q, want %q", got, campaignRoot)
	}
}

// TestDetect_AttachmentMarker_ViaSymlinkInTree mirrors the canonical user
// workflow: a symlink lives inside the campaign tree, its target lives
// outside the campaign tree and carries an attachment marker. cwd-ing through
// the symlink and detecting from there should reach the campaign.
func TestDetect_AttachmentMarker_ViaSymlinkInTree(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink test not supported on Windows")
	}

	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	campaignRoot := filepath.Join(tmpDir, "real-campaign")
	if err := os.MkdirAll(filepath.Join(campaignRoot, CampaignDir), 0755); err != nil {
		t.Fatalf("mkdir campaign: %v", err)
	}

	regPath := filepath.Join(tmpDir, "registry.json")
	regData := []byte(`{
  "version": 1,
  "campaigns": {
    "campaign-xyz": {
      "name": "real-campaign",
      "path": "` + campaignRoot + `"
    }
  }
}`)
	if err := os.WriteFile(regPath, regData, 0644); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	t.Setenv("CAMP_REGISTRY_PATH", regPath)

	// External directory with attachment marker.
	external := filepath.Join(tmpDir, "external", "some-dir")
	if err := os.MkdirAll(external, 0755); err != nil {
		t.Fatalf("mkdir external: %v", err)
	}
	marker := LinkMarker{
		Version:          3,
		Kind:             "attachment",
		ActiveCampaignID: "campaign-xyz",
	}
	if err := WriteMarker(external, marker); err != nil {
		t.Fatalf("WriteMarker: %v", err)
	}

	// Symlink inside the campaign tree pointing at the external dir.
	symlinkParent := filepath.Join(campaignRoot, "ai_docs", "examples")
	if err := os.MkdirAll(symlinkParent, 0755); err != nil {
		t.Fatalf("mkdir symlink parent: %v", err)
	}
	symlinkPath := filepath.Join(symlinkParent, "external-repo")
	if err := os.Symlink(external, symlinkPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// Detection from the symlink path should reach the campaign root via
	// either the in-tree walk-up (logical path) or the resolved-path walk.
	ClearCache()
	got, err := Detect(context.Background(), symlinkPath)
	if err != nil {
		t.Fatalf("Detect from symlink: %v", err)
	}
	if got != campaignRoot {
		t.Errorf("Detect = %q, want %q", got, campaignRoot)
	}
}
