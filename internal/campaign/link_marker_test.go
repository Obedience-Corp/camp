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

	if err := WriteMarker(projectRoot, LinkMarker{
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
