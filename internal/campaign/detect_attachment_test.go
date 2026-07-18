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

func TestDetect_SharedAttachmentUsesLogicalCampaignSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink test not supported on Windows")
	}

	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	campaignA := filepath.Join(tmpDir, "campaign-a")
	campaignB := filepath.Join(tmpDir, "campaign-b")
	for _, root := range []string{campaignA, campaignB} {
		if err := os.MkdirAll(filepath.Join(root, CampaignDir), 0755); err != nil {
			t.Fatalf("mkdir campaign: %v", err)
		}
	}

	regPath := filepath.Join(tmpDir, "registry.json")
	regData := []byte(`{
  "version": 1,
  "campaigns": {
    "campaign-a": {"name": "campaign-a", "path": "` + campaignA + `"},
    "campaign-b": {"name": "campaign-b", "path": "` + campaignB + `"}
  }
}`)
	if err := os.WriteFile(regPath, regData, 0644); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	t.Setenv("CAMP_REGISTRY_PATH", regPath)

	external := filepath.Join(tmpDir, "external", "shared")
	if err := os.MkdirAll(external, 0755); err != nil {
		t.Fatalf("mkdir external: %v", err)
	}
	marker := LinkMarker{
		Version:          LinkMarkerVersion,
		Kind:             KindAttachment,
		ActiveCampaignID: "campaign-b",
		CampaignIDs:      []string{"campaign-a"},
	}
	if err := WriteMarker(external, marker); err != nil {
		t.Fatalf("WriteMarker: %v", err)
	}

	symlinkA := filepath.Join(campaignA, "docs", "shared")
	symlinkB := filepath.Join(campaignB, "docs", "shared")
	for _, link := range []string{symlinkA, symlinkB} {
		if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
			t.Fatalf("mkdir symlink parent: %v", err)
		}
		if err := os.Symlink(external, link); err != nil {
			t.Fatalf("symlink %s: %v", link, err)
		}
	}

	ClearCache()
	if got, err := Detect(context.Background(), symlinkA); err != nil {
		t.Fatalf("Detect from campaign A symlink: %v", err)
	} else if got != campaignA {
		t.Errorf("Detect from campaign A symlink = %q, want %q", got, campaignA)
	}

	ClearCache()
	if got, err := Detect(context.Background(), symlinkB); err != nil {
		t.Fatalf("Detect from campaign B symlink: %v", err)
	} else if got != campaignB {
		t.Errorf("Detect from campaign B symlink = %q, want %q", got, campaignB)
	}

	ClearCache()
	if got, err := Detect(context.Background(), external); err != nil {
		t.Fatalf("Detect from direct attachment path: %v", err)
	} else if got != campaignB {
		t.Errorf("Detect from direct attachment path = %q, want active campaign %q", got, campaignB)
	}
}

func TestDetect_SharedAttachmentUsesLogicalPWDFromCwd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink test not supported on Windows")
	}

	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	campaignA := filepath.Join(tmpDir, "campaign-a")
	campaignB := filepath.Join(tmpDir, "campaign-b")
	for _, root := range []string{campaignA, campaignB} {
		if err := os.MkdirAll(filepath.Join(root, CampaignDir), 0755); err != nil {
			t.Fatalf("mkdir campaign: %v", err)
		}
	}

	regPath := filepath.Join(tmpDir, "registry.json")
	regData := []byte(`{
  "version": 1,
  "campaigns": {
    "campaign-a": {"name": "campaign-a", "path": "` + campaignA + `"},
    "campaign-b": {"name": "campaign-b", "path": "` + campaignB + `"}
  }
}`)
	if err := os.WriteFile(regPath, regData, 0644); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	t.Setenv("CAMP_REGISTRY_PATH", regPath)

	external := filepath.Join(tmpDir, "external", "shared")
	if err := os.MkdirAll(external, 0755); err != nil {
		t.Fatalf("mkdir external: %v", err)
	}
	marker := LinkMarker{
		Version:          LinkMarkerVersion,
		Kind:             KindAttachment,
		ActiveCampaignID: "campaign-b",
		CampaignIDs:      []string{"campaign-a"},
	}
	if err := WriteMarker(external, marker); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	symlinkA := filepath.Join(campaignA, "docs", "shared")
	symlinkB := filepath.Join(campaignB, "docs", "shared")
	for _, link := range []string{symlinkA, symlinkB} {
		if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
			t.Fatalf("mkdir symlink parent: %v", err)
		}
		if err := os.Symlink(external, link); err != nil {
			t.Fatalf("symlink %s: %v", link, err)
		}
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalWD) })

	if err := os.Chdir(symlinkA); err != nil {
		t.Fatalf("chdir campaign A symlink: %v", err)
	}
	t.Setenv("PWD", symlinkA)
	ClearCache()
	if got, err := DetectFromCwd(context.Background()); err != nil {
		t.Fatalf("DetectFromCwd from campaign A symlink: %v", err)
	} else if got != campaignA {
		t.Errorf("DetectFromCwd from campaign A symlink = %q, want %q", got, campaignA)
	}

	if err := os.Chdir(symlinkB); err != nil {
		t.Fatalf("chdir campaign B symlink: %v", err)
	}
	t.Setenv("PWD", symlinkB)
	ClearCache()
	if got, err := DetectCached(context.Background()); err != nil {
		t.Fatalf("DetectCached from campaign B symlink: %v", err)
	} else if got != campaignB {
		t.Errorf("DetectCached from campaign B symlink = %q, want %q", got, campaignB)
	}
}
