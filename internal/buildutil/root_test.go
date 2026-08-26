package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindCampRepoRootRefusesOutsideRepo(t *testing.T) {
	tmpDir := t.TempDir()
	sentinel := filepath.Join(tmpDir, "keep.bak")
	if err := os.WriteFile(sentinel, []byte("keep"), 0644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	_, err := findCampRepoRoot(tmpDir)
	if err == nil {
		t.Fatal("findCampRepoRoot() error = nil, want refusal outside camp repo root")
	}
	if !strings.Contains(err.Error(), "cannot find camp repo root") {
		t.Fatalf("findCampRepoRoot() error = %v, want camp repo root refusal", err)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("sentinel should remain after refused lookup: %v", err)
	}
}

func TestFindCampRepoRootAcceptsCampModule(t *testing.T) {
	root, err := findCampRepoRoot(".")
	if err != nil {
		t.Fatalf("findCampRepoRoot(.) error = %v", err)
	}
	mod := filepath.Join(root, "go.mod")
	data, err := os.ReadFile(mod)
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	if !strings.Contains(string(data), campModulePath) {
		t.Fatalf("resolved root %s is not the camp module", root)
	}
}
