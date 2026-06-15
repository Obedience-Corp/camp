package tasks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCleanRefusesOutsideRepoRoot(t *testing.T) {
	tmpDir := t.TempDir()
	sentinel := filepath.Join(tmpDir, "keep.bak")
	if err := os.WriteFile(sentinel, []byte("keep"), 0644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origDir)
	})
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}

	err = Clean(false)
	if err == nil {
		t.Fatal("Clean() error = nil, want refusal outside camp repo root")
	}
	if !strings.Contains(err.Error(), "cannot find camp repo root") {
		t.Fatalf("Clean() error = %v, want camp repo root refusal", err)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("sentinel should remain after refused clean: %v", err)
	}
}
