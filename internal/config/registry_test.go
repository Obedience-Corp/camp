package config

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

func TestLoadRegistry_Empty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	ctx := context.Background()
	reg, err := LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("LoadRegistry() error = %v", err)
	}

	if reg.Campaigns == nil {
		t.Error("Campaigns map is nil")
	}
	if len(reg.Campaigns) != 0 {
		t.Errorf("len(Campaigns) = %d, want 0", len(reg.Campaigns))
	}
}

func TestLoadRegistry_FromFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	configDir := filepath.Join(dir, AppName)
	os.MkdirAll(configDir, 0755)

	registryContent := `
campaigns:
  my-campaign:
    path: /home/user/my-campaign
    type: product
  other-campaign:
    path: /home/user/other
    type: research
`
	registryPath := filepath.Join(configDir, "registry.yaml")
	os.WriteFile(registryPath, []byte(registryContent), 0644)

	ctx := context.Background()
	reg, err := LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("LoadRegistry() error = %v", err)
	}

	if len(reg.Campaigns) != 2 {
		t.Errorf("len(Campaigns) = %d, want 2", len(reg.Campaigns))
	}

	c, ok := reg.Campaigns["my-campaign"]
	if !ok {
		t.Fatal("my-campaign not found in registry")
	}
	if c.Path != "/home/user/my-campaign" {
		t.Errorf("Path = %q, want %q", c.Path, "/home/user/my-campaign")
	}
	if c.Type != CampaignTypeProduct {
		t.Errorf("Type = %q, want %q", c.Type, CampaignTypeProduct)
	}
}

func TestLoadRegistry_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := LoadRegistry(ctx)
	if err != context.Canceled {
		t.Errorf("LoadRegistry() error = %v, want %v", err, context.Canceled)
	}
}

func TestSaveRegistry(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	reg := NewRegistry()
	reg.Register("test-campaign", "/tmp/test", CampaignTypeProduct)

	ctx := context.Background()
	err := SaveRegistry(ctx, reg)
	if err != nil {
		t.Fatalf("SaveRegistry() error = %v", err)
	}

	// Verify file was created
	path := RegistryPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("registry file was not created")
	}

	// Load and verify
	loaded, err := LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("LoadRegistry() error = %v", err)
	}

	if len(loaded.Campaigns) != 1 {
		t.Errorf("len(Campaigns) = %d, want 1", len(loaded.Campaigns))
	}
	c, ok := loaded.Campaigns["test-campaign"]
	if !ok {
		t.Fatal("test-campaign not found in loaded registry")
	}
	if c.Path != "/tmp/test" {
		t.Errorf("Path = %q, want %q", c.Path, "/tmp/test")
	}
}

func TestSaveRegistry_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	reg := NewRegistry()
	err := SaveRegistry(ctx, reg)
	if err != context.Canceled {
		t.Errorf("SaveRegistry() error = %v, want %v", err, context.Canceled)
	}
}

func TestRegistry_Register(t *testing.T) {
	reg := NewRegistry()

	reg.Register("test", "/path/to/test", CampaignTypeProduct)

	c, ok := reg.Campaigns["test"]
	if !ok {
		t.Fatal("campaign not found after register")
	}
	if c.Path != "/path/to/test" {
		t.Errorf("Path = %q, want %q", c.Path, "/path/to/test")
	}
	if c.Type != CampaignTypeProduct {
		t.Errorf("Type = %q, want %q", c.Type, CampaignTypeProduct)
	}
	if c.LastAccess.IsZero() {
		t.Error("LastAccess is zero")
	}
}

func TestRegistry_Register_NilMap(t *testing.T) {
	reg := &Registry{} // Campaigns is nil

	// Should not panic
	reg.Register("test", "/path/to/test", CampaignTypeProduct)

	if reg.Campaigns == nil {
		t.Error("Campaigns should be initialized")
	}
	if _, ok := reg.Campaigns["test"]; !ok {
		t.Error("campaign not found after register")
	}
}

func TestRegistry_Unregister(t *testing.T) {
	reg := NewRegistry()
	reg.Register("test", "/path/to/test", CampaignTypeProduct)

	reg.Unregister("test")

	if _, ok := reg.Campaigns["test"]; ok {
		t.Error("campaign should be removed after unregister")
	}
}

func TestRegistry_Unregister_NilMap(t *testing.T) {
	reg := &Registry{} // Campaigns is nil

	// Should not panic
	reg.Unregister("test")
}

func TestRegistry_Get(t *testing.T) {
	reg := NewRegistry()
	reg.Register("test", "/path/to/test", CampaignTypeProduct)

	c, ok := reg.Get("test")
	if !ok {
		t.Fatal("Get() returned false for existing campaign")
	}
	if c.Path != "/path/to/test" {
		t.Errorf("Path = %q, want %q", c.Path, "/path/to/test")
	}

	_, ok = reg.Get("nonexistent")
	if ok {
		t.Error("Get() returned true for nonexistent campaign")
	}
}

func TestRegistry_Get_NilMap(t *testing.T) {
	reg := &Registry{} // Campaigns is nil

	_, ok := reg.Get("test")
	if ok {
		t.Error("Get() returned true for nil map")
	}
}

func TestRegistry_UpdateLastAccess(t *testing.T) {
	reg := NewRegistry()
	reg.Register("test", "/path/to/test", CampaignTypeProduct)

	// Get initial time
	c1, _ := reg.Get("test")
	initial := c1.LastAccess

	// Wait a bit and update
	time.Sleep(1 * time.Millisecond)
	reg.UpdateLastAccess("test")

	// Get updated time
	c2, _ := reg.Get("test")
	if !c2.LastAccess.After(initial) {
		t.Error("LastAccess was not updated")
	}
}

func TestRegistry_UpdateLastAccess_Nonexistent(t *testing.T) {
	reg := NewRegistry()

	// Should not panic
	reg.UpdateLastAccess("nonexistent")
}

func TestRegistry_UpdateLastAccess_NilMap(t *testing.T) {
	reg := &Registry{} // Campaigns is nil

	// Should not panic
	reg.UpdateLastAccess("test")
}

func TestRegistry_List(t *testing.T) {
	reg := NewRegistry()
	reg.Register("alpha", "/path/to/alpha", CampaignTypeProduct)
	reg.Register("beta", "/path/to/beta", CampaignTypeResearch)
	reg.Register("gamma", "/path/to/gamma", CampaignTypeTools)

	names := reg.List()
	if len(names) != 3 {
		t.Errorf("len(List()) = %d, want 3", len(names))
	}

	// Sort for comparison
	sort.Strings(names)
	expected := []string{"alpha", "beta", "gamma"}
	for i, name := range names {
		if name != expected[i] {
			t.Errorf("names[%d] = %q, want %q", i, name, expected[i])
		}
	}
}

func TestRegistry_List_NilMap(t *testing.T) {
	reg := &Registry{} // Campaigns is nil

	names := reg.List()
	if names != nil {
		t.Errorf("List() = %v, want nil", names)
	}
}

func TestRegistry_Len(t *testing.T) {
	reg := NewRegistry()
	if reg.Len() != 0 {
		t.Errorf("Len() = %d, want 0", reg.Len())
	}

	reg.Register("test", "/path", CampaignTypeProduct)
	if reg.Len() != 1 {
		t.Errorf("Len() = %d, want 1", reg.Len())
	}
}

func TestRegistry_Len_NilMap(t *testing.T) {
	reg := &Registry{} // Campaigns is nil

	if reg.Len() != 0 {
		t.Errorf("Len() = %d, want 0", reg.Len())
	}
}

func TestRegistry_FindByPath(t *testing.T) {
	reg := NewRegistry()
	reg.Register("test", "/path/to/test", CampaignTypeProduct)

	name, c, ok := reg.FindByPath("/path/to/test")
	if !ok {
		t.Fatal("FindByPath() returned false for existing path")
	}
	if name != "test" {
		t.Errorf("name = %q, want %q", name, "test")
	}
	if c.Path != "/path/to/test" {
		t.Errorf("Path = %q, want %q", c.Path, "/path/to/test")
	}

	_, _, ok = reg.FindByPath("/nonexistent")
	if ok {
		t.Error("FindByPath() returned true for nonexistent path")
	}
}

func TestRegistry_FindByPath_NilMap(t *testing.T) {
	reg := &Registry{} // Campaigns is nil

	_, _, ok := reg.FindByPath("/path")
	if ok {
		t.Error("FindByPath() returned true for nil map")
	}
}

func TestRegistryPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	got := RegistryPath()
	want := filepath.Join(dir, AppName, "registry.yaml")
	if got != want {
		t.Errorf("RegistryPath() = %q, want %q", got, want)
	}
}

func TestRegistryRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	reg := NewRegistry()
	reg.Register("campaign-1", "/home/user/c1", CampaignTypeProduct)
	reg.Register("campaign-2", "/home/user/c2", CampaignTypeResearch)

	ctx := context.Background()

	// Save
	err := SaveRegistry(ctx, reg)
	if err != nil {
		t.Fatalf("SaveRegistry() error = %v", err)
	}

	// Load
	loaded, err := LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("LoadRegistry() error = %v", err)
	}

	// Verify
	if loaded.Len() != reg.Len() {
		t.Errorf("loaded.Len() = %d, want %d", loaded.Len(), reg.Len())
	}

	c, ok := loaded.Get("campaign-1")
	if !ok {
		t.Fatal("campaign-1 not found in loaded registry")
	}
	if c.Path != "/home/user/c1" {
		t.Errorf("campaign-1 Path = %q, want %q", c.Path, "/home/user/c1")
	}
}
