package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomically_NewFile(t *testing.T) {
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "marker.json")

	if err := WriteFileAtomically(target, []byte("hello"), 0644); err != nil {
		t.Fatalf("WriteFileAtomically() error = %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Errorf("content = %q, want %q", got, "hello")
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0644 {
		t.Errorf("mode = %o, want 0644", mode)
	}
}

func TestWriteFileAtomically_PreservesExistingMode(t *testing.T) {
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "marker.json")

	if err := os.WriteFile(target, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}

	// defaultMode is 0644 but the file already exists at 0600 — the existing
	// mode should win.
	if err := WriteFileAtomically(target, []byte("new"), 0644); err != nil {
		t.Fatalf("WriteFileAtomically() error = %v", err)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Errorf("mode = %o, want 0600 (existing mode preserved)", mode)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Errorf("content = %q, want %q", got, "new")
	}
}

func TestWriteFileAtomically_NoTempLeftBehindOnSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "marker.json")

	if err := WriteFileAtomically(target, []byte("x"), 0644); err != nil {
		t.Fatalf("WriteFileAtomically() error = %v", err)
	}

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("expected only the target file, got %d entries: %v", len(entries), names)
	}
}

func TestWriteFileAtomically_UnwritableDir(t *testing.T) {
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "nonexistent-subdir", "marker.json")

	if err := WriteFileAtomically(target, []byte("x"), 0644); err == nil {
		t.Error("expected error writing to nonexistent dir, got nil")
	}
}
