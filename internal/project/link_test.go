package project

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
)

func writeTestCampaignConfig(t *testing.T, root string) {
	t.Helper()

	cfg := &config.CampaignConfig{
		ID:   "camp-test",
		Name: "test-campaign",
		Type: config.CampaignTypeProduct,
	}
	if err := config.SaveCampaignConfig(context.Background(), root, cfg); err != nil {
		t.Fatalf("write campaign config: %v", err)
	}
}

func TestAddLinked_WritesMarkerBeforeSymlink(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	writeTestCampaignConfig(t, root)

	source := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := AddLinked(context.Background(), root, source, LinkOptions{Name: "linked"})
	if err != nil {
		t.Fatalf("AddLinked() error = %v", err)
	}
	if result.Path != filepath.Join("projects", "linked") {
		t.Fatalf("result.Path = %q, want projects/linked", result.Path)
	}

	if _, err := os.Lstat(filepath.Join(root, "projects", "linked")); err != nil {
		t.Fatalf("linked symlink missing: %v", err)
	}
	marker, err := campaign.ReadMarker(source)
	if err != nil {
		t.Fatalf("marker missing: %v", err)
	}
	if marker.Kind != campaign.KindProject || marker.ActiveCampaignID != "camp-test" {
		t.Fatalf("marker = %+v, want project marker for camp-test", marker)
	}
}

func TestAddLinked_RemovesMarkerWhenSymlinkFails(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	writeTestCampaignConfig(t, root)

	source := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}

	projectsDir := filepath.Join(root, "projects")
	if err := os.MkdirAll(projectsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(projectsDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(projectsDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(projectsDir, info.Mode().Perm())
	})

	_, err = AddLinked(context.Background(), root, source, LinkOptions{Name: "linked"})
	if err == nil {
		_ = os.Remove(filepath.Join(root, "projects", "linked"))
		t.Skip("symlink creation unexpectedly succeeded in read-only projects directory")
	}
	if _, markerErr := os.Stat(campaign.MarkerPath(source)); !os.IsNotExist(markerErr) {
		t.Fatalf("marker should be removed after symlink failure, stat err = %v", markerErr)
	}
}

func TestAddLinked_RestoresExistingMarkerWhenSymlinkFails(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	writeTestCampaignConfig(t, root)

	source := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := campaign.WriteMarker(source, campaign.LinkMarker{
		Version:          2,
		Kind:             campaign.KindProject,
		ActiveCampaignID: "camp-test",
		ProjectName:      "existing",
	}); err != nil {
		t.Fatalf("write existing marker: %v", err)
	}
	wantMarker, err := os.ReadFile(campaign.MarkerPath(source))
	if err != nil {
		t.Fatalf("read existing marker: %v", err)
	}

	projectsDir := filepath.Join(root, "projects")
	if err := os.MkdirAll(projectsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(projectsDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(projectsDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(projectsDir, info.Mode().Perm())
	})

	_, err = AddLinked(context.Background(), root, source, LinkOptions{Name: "linked"})
	if err == nil {
		_ = os.Remove(filepath.Join(root, "projects", "linked"))
		t.Skip("symlink creation unexpectedly succeeded in read-only projects directory")
	}

	gotMarker, err := os.ReadFile(campaign.MarkerPath(source))
	if err != nil {
		t.Fatalf("marker should be restored after symlink failure: %v", err)
	}
	if !bytes.Equal(gotMarker, wantMarker) {
		t.Fatalf("marker changed after symlink failure:\ngot:\n%s\nwant:\n%s", gotMarker, wantMarker)
	}
}
