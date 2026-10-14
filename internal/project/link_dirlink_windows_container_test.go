//go:build windows && container_fs

package project

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/camp/internal/campaign"
)

func TestAddLinked_CaseOnlyNameCollision(t *testing.T) {
	root := t.TempDir()
	writeTestCampaignConfig(t, root)
	sourceA := filepath.Join(t.TempDir(), "source-a")
	sourceB := filepath.Join(t.TempDir(), "source-b")
	for _, source := range []string{sourceA, sourceB} {
		if err := os.MkdirAll(source, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	keep := filepath.Join(sourceA, "keep.txt")
	if err := os.WriteFile(keep, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := AddLinked(context.Background(), root, sourceA, LinkOptions{Name: "MixedCase"}); err != nil {
		t.Fatalf("first AddLinked: %v", err)
	}
	markerBefore, err := os.ReadFile(campaign.MarkerPath(sourceA))
	if err != nil {
		t.Fatalf("read first source marker: %v", err)
	}
	_, err = AddLinked(context.Background(), root, sourceB, LinkOptions{Name: "mixedcase"})
	var exists *ErrProjectExists
	if !errors.As(err, &exists) {
		t.Fatalf("case-only link name collision error = %v, want ErrProjectExists", err)
	}
	if got, err := os.ReadFile(filepath.Join(root, "projects", "MixedCase", "keep.txt")); err != nil || string(got) != "original" {
		t.Fatalf("existing link changed after case collision: %q, %v", got, err)
	}
	if _, err := os.Stat(campaign.MarkerPath(sourceB)); !os.IsNotExist(err) {
		t.Fatalf("rejected source retained a .camp marker: %v", err)
	}
	markerAfter, err := os.ReadFile(campaign.MarkerPath(sourceA))
	if err != nil || string(markerAfter) != string(markerBefore) {
		t.Fatalf("first source marker changed after case collision: %q, %v", markerAfter, err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "projects"))
	if err != nil || len(entries) != 1 || entries[0].Name() != "MixedCase" {
		t.Fatalf("project links after case collision = %v, %v; want only MixedCase", entries, err)
	}
}
