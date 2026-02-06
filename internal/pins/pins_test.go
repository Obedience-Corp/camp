package pins

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewStore(t *testing.T) {
	s := NewStore("/tmp/test.json")
	if s == nil {
		t.Fatal("NewStore returned nil")
	}
	if s.path != "/tmp/test.json" {
		t.Errorf("path = %q, want %q", s.path, "/tmp/test.json")
	}
}

func TestLoadMissingFile(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "missing.json"))
	if err := s.Load(); err != nil {
		t.Fatalf("Load() on missing file returned error: %v", err)
	}
	if len(s.List()) != 0 {
		t.Errorf("List() = %d items, want 0", len(s.List()))
	}
}

func TestLoadEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.json")
	if err := os.WriteFile(path, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}
	s := NewStore(path)
	if err := s.Load(); err != nil {
		t.Fatalf("Load() on empty file returned error: %v", err)
	}
	if len(s.List()) != 0 {
		t.Errorf("List() = %d items, want 0", len(s.List()))
	}
}

func TestLoadCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.json")
	if err := os.WriteFile(path, []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}
	s := NewStore(path)
	if err := s.Load(); err == nil {
		t.Fatal("Load() on corrupt file should return error")
	}
}

func TestSaveAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pins.json")
	s := NewStore(path)

	if err := s.Add("proj", "/home/user/project"); err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	if err := s.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Load into a new store
	s2 := NewStore(path)
	if err := s2.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	pins := s2.List()
	if len(pins) != 1 {
		t.Fatalf("List() = %d items, want 1", len(pins))
	}
	if pins[0].Name != "proj" {
		t.Errorf("Name = %q, want %q", pins[0].Name, "proj")
	}
	if pins[0].Path != "/home/user/project" {
		t.Errorf("Path = %q, want %q", pins[0].Path, "/home/user/project")
	}
}

func TestSaveCreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "dir")
	path := filepath.Join(dir, "pins.json")
	s := NewStore(path)

	if err := s.Add("test", "/tmp"); err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	if err := s.Save(); err != nil {
		t.Fatalf("Save() should create parent directories: %v", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("pins.json was not created")
	}
}

func TestGet(t *testing.T) {
	s := NewStore("")
	_ = s.Add("alpha", "/a")
	_ = s.Add("beta", "/b")

	tests := []struct {
		name  string
		query string
		want  bool
	}{
		{"found", "alpha", true},
		{"found second", "beta", true},
		{"not found", "gamma", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := s.Get(tt.query)
			if ok != tt.want {
				t.Errorf("Get(%q) ok = %v, want %v", tt.query, ok, tt.want)
			}
		})
	}
}

func TestAddDuplicate(t *testing.T) {
	s := NewStore("")
	if err := s.Add("dup", "/a"); err != nil {
		t.Fatalf("first Add() error: %v", err)
	}
	if err := s.Add("dup", "/b"); err == nil {
		t.Fatal("second Add() with same name should return error")
	}
}

func TestRemove(t *testing.T) {
	s := NewStore("")
	_ = s.Add("alpha", "/a")
	_ = s.Add("beta", "/b")
	_ = s.Add("gamma", "/c")

	if err := s.Remove("beta"); err != nil {
		t.Fatalf("Remove() error: %v", err)
	}

	if len(s.List()) != 2 {
		t.Fatalf("List() = %d items, want 2", len(s.List()))
	}
	if _, ok := s.Get("beta"); ok {
		t.Error("beta should have been removed")
	}
	if _, ok := s.Get("alpha"); !ok {
		t.Error("alpha should still exist")
	}
	if _, ok := s.Get("gamma"); !ok {
		t.Error("gamma should still exist")
	}
}

func TestRemoveNotFound(t *testing.T) {
	s := NewStore("")
	if err := s.Remove("nonexistent"); err == nil {
		t.Fatal("Remove() on missing pin should return error")
	}
}

func TestNames(t *testing.T) {
	s := NewStore("")
	_ = s.Add("alpha", "/a")
	_ = s.Add("beta", "/b")

	names := s.Names()
	if len(names) != 2 {
		t.Fatalf("Names() = %d items, want 2", len(names))
	}
	if names[0] != "alpha" || names[1] != "beta" {
		t.Errorf("Names() = %v, want [alpha beta]", names)
	}
}

func TestNamesEmpty(t *testing.T) {
	s := NewStore("")
	names := s.Names()
	if len(names) != 0 {
		t.Errorf("Names() = %d items, want 0", len(names))
	}
}
