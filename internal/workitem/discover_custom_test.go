package workitem

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEmitCandidate_BuiltinAlwaysEmits(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "x")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ok, reason := emitCandidate("design", dir)
	if !ok || reason != "builtin" {
		t.Errorf("design built-in: ok=%v reason=%q", ok, reason)
	}
}

func TestEmitCandidate_CustomTypeRequiresMarker(t *testing.T) {
	root := t.TempDir()
	noMarker := filepath.Join(root, "incident-no-marker")
	if err := os.MkdirAll(noMarker, 0o755); err != nil {
		t.Fatal(err)
	}
	ok, reason := emitCandidate("incident", noMarker)
	if ok || reason != "no-marker" {
		t.Errorf("custom type without marker: ok=%v reason=%q", ok, reason)
	}

	withMarker := filepath.Join(root, "incident-with-marker")
	if err := os.MkdirAll(withMarker, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(withMarker, MetadataFilename), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, reason = emitCandidate("incident", withMarker)
	if !ok || reason != "marker" {
		t.Errorf("custom type with marker: ok=%v reason=%q", ok, reason)
	}
}
