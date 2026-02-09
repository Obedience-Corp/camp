package flow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRegistryPath(t *testing.T) {
	got := RegistryPath("/home/user/campaign")
	want := "/home/user/campaign/.campaign/flows/registry.yaml"
	if got != want {
		t.Errorf("RegistryPath() = %q, want %q", got, want)
	}
}

func TestLoadRegistry_MissingFile(t *testing.T) {
	tmp := t.TempDir()
	reg, err := LoadRegistry(tmp)
	if err != nil {
		t.Fatalf("LoadRegistry() error = %v", err)
	}
	if reg.Version != 1 {
		t.Errorf("Version = %d, want 1", reg.Version)
	}
	if len(reg.Flows) != 0 {
		t.Errorf("Flows should be empty, got %d", len(reg.Flows))
	}
}

func TestLoadRegistry_ValidFile(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, ".campaign", "flows")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}

	yaml := `version: 1
flows:
  build:
    description: Build the project
    command: just build
    workdir: projects/camp
  test:
    description: Run tests
    command: just test all
    workdir: projects/camp
    tags:
      - ci
`
	if err := os.WriteFile(filepath.Join(dir, "registry.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	reg, err := LoadRegistry(tmp)
	if err != nil {
		t.Fatalf("LoadRegistry() error = %v", err)
	}
	if reg.Version != 1 {
		t.Errorf("Version = %d, want 1", reg.Version)
	}
	if len(reg.Flows) != 2 {
		t.Errorf("Flows count = %d, want 2", len(reg.Flows))
	}

	build, ok := reg.Get("build")
	if !ok {
		t.Fatal("expected 'build' flow")
	}
	if build.Command != "just build" {
		t.Errorf("build.Command = %q, want %q", build.Command, "just build")
	}
	if build.WorkDir != "projects/camp" {
		t.Errorf("build.WorkDir = %q, want %q", build.WorkDir, "projects/camp")
	}
}

func TestSaveRegistry(t *testing.T) {
	tmp := t.TempDir()
	reg := &Registry{
		Version: 1,
		Flows: map[string]Flow{
			"deploy": {
				Description: "Deploy to prod",
				Command:     "make deploy",
				WorkDir:     ".",
			},
		},
	}

	if err := SaveRegistry(tmp, reg); err != nil {
		t.Fatalf("SaveRegistry() error = %v", err)
	}

	// Verify file exists
	path := RegistryPath(tmp)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("registry file not created: %v", err)
	}

	// Load and verify
	loaded, err := LoadRegistry(tmp)
	if err != nil {
		t.Fatalf("LoadRegistry() error = %v", err)
	}
	if len(loaded.Flows) != 1 {
		t.Errorf("loaded Flows count = %d, want 1", len(loaded.Flows))
	}
	deploy, ok := loaded.Get("deploy")
	if !ok {
		t.Fatal("expected 'deploy' flow after round-trip")
	}
	if deploy.Command != "make deploy" {
		t.Errorf("deploy.Command = %q, want %q", deploy.Command, "make deploy")
	}
}

func TestRegistry_List(t *testing.T) {
	reg := &Registry{
		Version: 1,
		Flows: map[string]Flow{
			"build":  {Command: "make build"},
			"test":   {Command: "make test"},
			"deploy": {Command: "make deploy"},
		},
	}

	names := reg.List()
	if len(names) != 3 {
		t.Fatalf("List() returned %d names, want 3", len(names))
	}
	// Should be sorted
	if names[0] != "build" || names[1] != "deploy" || names[2] != "test" {
		t.Errorf("List() = %v, want [build deploy test]", names)
	}
}

func TestRegistry_Get_NotFound(t *testing.T) {
	reg := &Registry{
		Version: 1,
		Flows:   make(map[string]Flow),
	}

	_, ok := reg.Get("nonexistent")
	if ok {
		t.Error("expected not found")
	}
}
