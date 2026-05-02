package pins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestPin_AbsPath_RoundTrip is part of the non-project-symlinks design.
// Pins targeting attachment-marker directories outside the campaign root must
// be persistable via an AbsPath field. In-tree pins continue to use Path
// (campaign-root-relative) as today. This test will start passing once Pin
// gains an AbsPath JSON field.
func TestPin_AbsPath_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pins.json")

	// Hand-write a pins file that contains both an in-tree pin and an
	// attachment pin. The attachment pin uses abs_path; Path is empty.
	raw := []byte(`[
  {
    "name": "in-tree",
    "path": "festivals/active/foo",
    "created_at": "2026-05-02T00:00:00Z"
  },
  {
    "name": "ext",
    "abs_path": "/tmp/some-attached-dir",
    "created_at": "2026-05-02T00:00:00Z"
  }
]`)
	if err := os.WriteFile(path, raw, 0644); err != nil {
		t.Fatal(err)
	}

	s := NewStore(path)
	if err := s.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	pinList := s.List()
	if len(pinList) != 2 {
		t.Fatalf("len(pins) = %d, want 2", len(pinList))
	}

	// In-tree pin: Path set, AbsPath empty.
	var inTree, ext Pin
	for _, p := range pinList {
		switch p.Name {
		case "in-tree":
			inTree = p
		case "ext":
			ext = p
		}
	}

	if inTree.Path != "festivals/active/foo" {
		t.Errorf("in-tree Path = %q, want %q", inTree.Path, "festivals/active/foo")
	}
	if inTree.AbsPath != "" {
		t.Errorf("in-tree AbsPath = %q, want empty", inTree.AbsPath)
	}

	if ext.AbsPath != "/tmp/some-attached-dir" {
		t.Errorf("attachment AbsPath = %q, want %q", ext.AbsPath, "/tmp/some-attached-dir")
	}
	if ext.Path != "" {
		t.Errorf("attachment Path = %q, want empty", ext.Path)
	}

	// Save and reload to confirm AbsPath survives the round-trip.
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var parsed []map[string]any
	if err := json.Unmarshal(saved, &parsed); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	var sawAbsPath bool
	for _, m := range parsed {
		if v, ok := m["abs_path"].(string); ok && v == "/tmp/some-attached-dir" {
			sawAbsPath = true
		}
	}
	if !sawAbsPath {
		t.Errorf("saved pins file does not contain abs_path; got: %s", string(saved))
	}
}
