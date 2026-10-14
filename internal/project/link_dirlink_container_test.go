//go:build container_fs

package project

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/camp/internal/campaign"
)

func TestAddLinked_MarkerRollbackOnLinkCollision(t *testing.T) {
	root := t.TempDir()
	writeTestCampaignConfig(t, root)

	source := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := AddLinked(context.Background(), root, source, LinkOptions{Name: "dup"}); err != nil {
		t.Fatalf("first AddLinked: %v", err)
	}

	other := filepath.Join(t.TempDir(), "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := AddLinked(context.Background(), root, other, LinkOptions{Name: "dup"})
	if err == nil {
		t.Fatal("expected collision error")
	}
	if _, markerErr := os.Stat(campaign.MarkerPath(other)); !os.IsNotExist(markerErr) {
		t.Fatalf("collision must not write .camp marker on rejected target, stat err = %v", markerErr)
	}
}

func TestUnlinkProject_RemovesLinkOnly(t *testing.T) {
	root := t.TempDir()
	writeTestCampaignConfig(t, root)

	source := filepath.Join(t.TempDir(), "source")
	keep := filepath.Join(source, "keep.txt")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keep, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := AddLinked(context.Background(), root, source, LinkOptions{Name: "linked"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UnlinkProject(context.Background(), root, res.Name, res.Source); err != nil {
		t.Fatalf("UnlinkProject: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("unlink removed external file: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "projects", "linked")); !os.IsNotExist(err) {
		t.Fatalf("camp link still present: %v", err)
	}
}

func TestUnlinkProject_RejectsOrdinaryDirectory(t *testing.T) {
	root := t.TempDir()
	projectPath := filepath.Join(root, "projects", "ordinary")
	keep := filepath.Join(projectPath, "keep.txt")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keep, []byte("stay"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := UnlinkProject(context.Background(), root, "ordinary", ""); err == nil {
		t.Fatal("expected ordinary directory to be rejected")
	}
	if got, err := os.ReadFile(keep); err != nil || string(got) != "stay" {
		t.Fatalf("ordinary project content changed: %q, %v", got, err)
	}
}
