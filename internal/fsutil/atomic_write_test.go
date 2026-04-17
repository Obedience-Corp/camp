package fsutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFileAtomically_NewFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.json")

	if err := WriteFileAtomically(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFileAtomically() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("file contents = %q, want %q", string(data), "hello")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("os.Stat() error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("file mode = %o, want %o", got, 0o644)
	}
}

func TestWriteFileAtomically_PreservesExistingMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing.json")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	if err := WriteFileAtomically(path, []byte("new"), 0o644); err != nil {
		t.Fatalf("WriteFileAtomically() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("os.Stat() error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("file mode = %o, want %o", got, 0o600)
	}
}

func TestWriteFileAtomically_NoTempLeftBehindOnSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.json")

	if err := WriteFileAtomically(path, []byte("ok"), 0o644); err != nil {
		t.Fatalf("WriteFileAtomically() error = %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("os.ReadDir() error = %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "cache.json.tmp-") {
			t.Fatalf("unexpected temp file left behind: %s", entry.Name())
		}
	}
}

func TestWriteFileAtomically_UnwritableDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "config.json")

	if err := WriteFileAtomically(path, []byte("nope"), 0o644); err == nil {
		t.Fatal("WriteFileAtomically() expected error for missing parent dir")
	}
}
