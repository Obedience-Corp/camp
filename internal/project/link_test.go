package project

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

// A linked project is a symlink that leaves the camp by design. A second link
// under the same name must report the name conflict, not follow that symlink
// and call it a boundary violation.
func TestPlanLinked_NameTakenByAnotherLinkReportsConflict(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	writeTestCampaignConfig(t, root)

	first := filepath.Join(t.TempDir(), "app")
	second := filepath.Join(t.TempDir(), "app")
	for _, dir := range []string{first, second} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := AddLinked(t.Context(), root, first, LinkOptions{}); err != nil {
		t.Fatalf("AddLinked(first) error = %v", err)
	}

	_, err := PlanLinked(t.Context(), root, second, LinkOptions{})
	var exists *ErrProjectExists
	if !errors.As(err, &exists) {
		t.Fatalf("PlanLinked(second) error = %v; want ErrProjectExists", err)
	}
	if exists.Name != "app" {
		t.Fatalf("ErrProjectExists.Name = %q; want app", exists.Name)
	}
	if strings.Contains(err.Error(), "boundary") {
		t.Fatalf("PlanLinked(second) error = %q; a name conflict must not read as a boundary violation", err)
	}
	if _, statErr := os.Lstat(campaign.MarkerPath(second)); !os.IsNotExist(statErr) {
		t.Fatalf("a rejected plan wrote a .camp marker: %v", statErr)
	}
}

func TestAvailableLinkName(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	writeTestCampaignConfig(t, root)

	got, err := AvailableLinkName(root, "app")
	if err != nil || got != "app" {
		t.Fatalf("AvailableLinkName() with nothing linked = %q, %v; want app", got, err)
	}

	link := func(name string) {
		t.Helper()
		source := filepath.Join(t.TempDir(), "app")
		if err := os.MkdirAll(source, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := AddLinked(t.Context(), root, source, LinkOptions{Name: name}); err != nil {
			t.Fatalf("AddLinked(%s) error = %v", name, err)
		}
	}

	link("app")
	if got, err := AvailableLinkName(root, "app"); err != nil || got != "app-2" {
		t.Fatalf("AvailableLinkName() with app taken = %q, %v; want app-2", got, err)
	}
	link("app-2")
	if got, err := AvailableLinkName(root, "app"); err != nil || got != "app-3" {
		t.Fatalf("AvailableLinkName() with app and app-2 taken = %q, %v; want app-3", got, err)
	}

	if _, err := AvailableLinkName(root, "not a name"); !errors.Is(err, ErrInvalidProjectName) {
		t.Fatalf("AvailableLinkName() with an invalid base error = %v; want ErrInvalidProjectName", err)
	}
}
