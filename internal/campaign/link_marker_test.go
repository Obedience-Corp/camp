package campaign

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDetect_FromLinkedProjectMarker(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	campaignRoot := filepath.Join(tmpDir, "campaign")
	projectRoot := filepath.Join(tmpDir, "linked-project")
	nestedDir := filepath.Join(projectRoot, "src", "pkg")

	if err := os.MkdirAll(filepath.Join(campaignRoot, CampaignDir), 0o755); err != nil {
		t.Fatalf("create campaign dir: %v", err)
	}
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("create linked project dir: %v", err)
	}

	registryPath := filepath.Join(tmpDir, "registry.json")
	t.Setenv("CAMP_REGISTRY_PATH", registryPath)
	if err := writeTestRegistry(registryPath, "campaign-123", campaignRoot); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	if err := WriteMarker(projectRoot, LinkMarker{
		Version:          LinkMarkerVersion,
		ActiveCampaignID: "campaign-123",
	}); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	got, err := Detect(context.Background(), nestedDir)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if got != campaignRoot {
		t.Fatalf("Detect() = %q, want %q", got, campaignRoot)
	}
}

func TestDetect_FromLegacyLinkedProjectMarker(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	campaignRoot := filepath.Join(tmpDir, "campaign")
	projectRoot := filepath.Join(tmpDir, "linked-project")
	nestedDir := filepath.Join(projectRoot, "src", "pkg")

	if err := os.MkdirAll(filepath.Join(campaignRoot, CampaignDir), 0o755); err != nil {
		t.Fatalf("create campaign dir: %v", err)
	}
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("create linked project dir: %v", err)
	}

	if err := WriteMarker(projectRoot, LinkMarker{
		Version:      1,
		CampaignRoot: campaignRoot,
		ProjectName:  "linked-project",
	}); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	got, err := Detect(context.Background(), nestedDir)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if got != campaignRoot {
		t.Fatalf("Detect() = %q, want %q", got, campaignRoot)
	}
}

func writeTestRegistry(path, campaignID, campaignRoot string) error {
	data := []byte("{\n  \"version\": 2,\n  \"campaigns\": {\n    \"" + campaignID + "\": {\n      \"name\": \"test\",\n      \"path\": \"" + campaignRoot + "\"\n    }\n  }\n}\n")
	return os.WriteFile(path, data, 0o644)
}
