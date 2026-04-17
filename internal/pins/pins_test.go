package pins

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
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
	} else if !errors.Is(err, camperrors.ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists, got: %v", err)
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
	} else if !errors.Is(err, camperrors.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
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

func TestFindByPath(t *testing.T) {
	s := NewStore("")
	_ = s.Add("alpha", "/home/user/a")
	_ = s.Add("beta", "/home/user/b")

	tests := []struct {
		name  string
		path  string
		want  bool
		wantN string
	}{
		{"found first", "/home/user/a", true, "alpha"},
		{"found second", "/home/user/b", true, "beta"},
		{"not found", "/home/user/c", false, ""},
		{"empty path", "", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pin, ok := s.FindByPath(tt.path)
			if ok != tt.want {
				t.Errorf("FindByPath(%q) ok = %v, want %v", tt.path, ok, tt.want)
			}
			if ok && pin.Name != tt.wantN {
				t.Errorf("FindByPath(%q).Name = %q, want %q", tt.path, pin.Name, tt.wantN)
			}
		})
	}
}

func TestNamesEmpty(t *testing.T) {
	s := NewStore("")
	names := s.Names()
	if len(names) != 0 {
		t.Errorf("Names() = %d items, want 0", len(names))
	}
}

func TestToggle(t *testing.T) {
	tests := []struct {
		name       string
		setup      []Pin
		toggleName string
		togglePath string
		wantResult ToggleResult
		wantPins   []string
	}{
		{
			name:       "fresh pin - no existing pins",
			toggleName: "mypin",
			togglePath: "/tmp/foo",
			wantResult: Pinned,
			wantPins:   []string{"mypin"},
		},
		{
			name:       "fresh pin - other pins exist",
			setup:      []Pin{{Name: "other", Path: "/tmp/other"}},
			toggleName: "mypin",
			togglePath: "/tmp/foo",
			wantResult: Pinned,
			wantPins:   []string{"other", "mypin"},
		},
		{
			name:       "toggle off - same name same path",
			setup:      []Pin{{Name: "mypin", Path: "/tmp/foo"}},
			toggleName: "mypin",
			togglePath: "/tmp/foo",
			wantResult: Unpinned,
			wantPins:   []string{},
		},
		{
			name: "toggle off preserves other pins",
			setup: []Pin{
				{Name: "first", Path: "/tmp/first"},
				{Name: "mypin", Path: "/tmp/foo"},
				{Name: "last", Path: "/tmp/last"},
			},
			toggleName: "mypin",
			togglePath: "/tmp/foo",
			wantResult: Unpinned,
			wantPins:   []string{"first", "last"},
		},
		{
			name:       "update path - same name different path",
			setup:      []Pin{{Name: "mypin", Path: "/tmp/old"}},
			toggleName: "mypin",
			togglePath: "/tmp/new",
			wantResult: Updated,
			wantPins:   []string{"mypin"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewStore("")
			for _, p := range tt.setup {
				_ = s.Add(p.Name, p.Path)
			}

			got := s.Toggle(tt.toggleName, tt.togglePath)
			if got != tt.wantResult {
				t.Errorf("Toggle() = %v, want %v", got, tt.wantResult)
			}

			gotNames := s.Names()
			if len(gotNames) != len(tt.wantPins) {
				t.Fatalf("got %d pins %v, want %d pins %v", len(gotNames), gotNames, len(tt.wantPins), tt.wantPins)
			}
			for i, name := range tt.wantPins {
				if gotNames[i] != name {
					t.Errorf("pin[%d] = %q, want %q", i, gotNames[i], name)
				}
			}

			if tt.wantResult == Updated {
				pin, ok := s.Get(tt.toggleName)
				if !ok {
					t.Fatal("pin not found after update")
				}
				if pin.Path != tt.togglePath {
					t.Errorf("pin path = %q, want %q", pin.Path, tt.togglePath)
				}
			}
		})
	}
}

func TestMigrateAbsoluteToRelative(t *testing.T) {
	root := "/home/user/campaign"

	tests := []struct {
		name        string
		pins        []Pin
		wantPins    []Pin
		wantChanged bool
	}{
		{
			name: "converts internal absolute pin",
			pins: []Pin{
				{Name: "proj", Path: "/home/user/campaign/projects/foo"},
			},
			wantPins: []Pin{
				{Name: "proj", Path: "projects/foo"},
			},
			wantChanged: true,
		},
		{
			name: "drops external absolute pin",
			pins: []Pin{
				{Name: "ext", Path: "/tmp/outside"},
			},
			wantPins:    []Pin{},
			wantChanged: true,
		},
		{
			name: "mixed: converts internal, drops external",
			pins: []Pin{
				{Name: "inside", Path: "/home/user/campaign/docs"},
				{Name: "outside", Path: "/etc/secrets"},
				{Name: "relative", Path: "already/relative"},
			},
			wantPins: []Pin{
				{Name: "inside", Path: "docs"},
				{Name: "relative", Path: "already/relative"},
			},
			wantChanged: true,
		},
		{
			name: "no change when all relative",
			pins: []Pin{
				{Name: "a", Path: "projects/a"},
			},
			wantPins: []Pin{
				{Name: "a", Path: "projects/a"},
			},
			wantChanged: false,
		},
		{
			name:        "empty list",
			pins:        []Pin{},
			wantPins:    []Pin{},
			wantChanged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewStore("")
			for _, p := range tt.pins {
				_ = s.Add(p.Name, p.Path)
			}

			changed := s.MigrateAbsoluteToRelative(root)
			if changed != tt.wantChanged {
				t.Errorf("MigrateAbsoluteToRelative() changed = %v, want %v", changed, tt.wantChanged)
			}

			got := s.List()
			if len(got) != len(tt.wantPins) {
				t.Fatalf("got %d pins, want %d", len(got), len(tt.wantPins))
			}
			for i, want := range tt.wantPins {
				if got[i].Name != want.Name {
					t.Errorf("pin[%d].Name = %q, want %q", i, got[i].Name, want.Name)
				}
				if got[i].Path != want.Path {
					t.Errorf("pin[%d].Path = %q, want %q", i, got[i].Path, want.Path)
				}
			}
		})
	}
}

func TestTogglePersistence(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "pins.json")

	store1 := NewStore(storePath)
	store1.Toggle("mypin", "/tmp/foo")
	if err := store1.Save(); err != nil {
		t.Fatal(err)
	}

	store2 := NewStore(storePath)
	if err := store2.Load(); err != nil {
		t.Fatal(err)
	}
	result := store2.Toggle("mypin", "/tmp/foo")
	if result != Unpinned {
		t.Errorf("expected Unpinned, got %v", result)
	}
	if err := store2.Save(); err != nil {
		t.Fatal(err)
	}

	store3 := NewStore(storePath)
	if err := store3.Load(); err != nil {
		t.Fatal(err)
	}
	if len(store3.List()) != 0 {
		t.Errorf("expected 0 pins after toggle-off, got %d", len(store3.List()))
	}
}
