package statuspath

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDateDir(t *testing.T) {
	got := DateDir(time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC))
	if got != "2026-03-10" {
		t.Fatalf("DateDir() = %q, want %q", got, "2026-03-10")
	}
}

func TestExistingItemPath(t *testing.T) {
	root := t.TempDir()

	legacyPath := filepath.Join(root, "legacy.md")
	if err := os.WriteFile(legacyPath, []byte("legacy"), 0o644); err != nil {
		t.Fatalf("failed to create legacy item: %v", err)
	}

	got, exists, err := ExistingItemPath(root, "legacy.md")
	if err != nil {
		t.Fatalf("ExistingItemPath() error = %v", err)
	}
	if !exists {
		t.Fatal("ExistingItemPath() should report existing legacy item")
	}
	if got != legacyPath {
		t.Fatalf("ExistingItemPath() = %q, want %q", got, legacyPath)
	}

	datedDir := filepath.Join(root, "2026-03-10")
	if err := os.MkdirAll(datedDir, 0o755); err != nil {
		t.Fatalf("failed to create dated dir: %v", err)
	}
	datedPath := filepath.Join(datedDir, "dated.md")
	if err := os.WriteFile(datedPath, []byte("dated"), 0o644); err != nil {
		t.Fatalf("failed to create dated item: %v", err)
	}

	got, exists, err = ExistingItemPath(root, "dated.md")
	if err != nil {
		t.Fatalf("ExistingItemPath() error = %v", err)
	}
	if !exists {
		t.Fatal("ExistingItemPath() should report existing dated item")
	}
	if got != datedPath {
		t.Fatalf("ExistingItemPath() = %q, want %q", got, datedPath)
	}
}

func TestCountItems(t *testing.T) {
	root := t.TempDir()

	if err := os.WriteFile(filepath.Join(root, "legacy.md"), []byte("legacy"), 0o644); err != nil {
		t.Fatalf("failed to create legacy item: %v", err)
	}
	datedDir := filepath.Join(root, "2026-03-10")
	if err := os.MkdirAll(datedDir, 0o755); err != nil {
		t.Fatalf("failed to create dated dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(datedDir, "done.md"), []byte("done"), 0o644); err != nil {
		t.Fatalf("failed to create dated item: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitkeep"), []byte(""), 0o644); err != nil {
		t.Fatalf("failed to create gitkeep: %v", err)
	}

	got, err := CountItems(root)
	if err != nil {
		t.Fatalf("CountItems() error = %v", err)
	}
	if got != 2 {
		t.Fatalf("CountItems() = %d, want 2", got)
	}
}
