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

	// New format: campaigns keyed by ID with name field
	registryContent := `
campaigns:
  550e8400-e29b-41d4-a716-446655440000:
    name: my-campaign
    path: /home/user/my-campaign
    type: product
  a1b2c3d4-e5f6-7890-abcd-ef1234567890:
    name: other-campaign
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

	c, ok := reg.Campaigns["550e8400-e29b-41d4-a716-446655440000"]
	if !ok {
		t.Fatal("my-campaign not found in registry by ID")
	}
	if c.Name != "my-campaign" {
		t.Errorf("Name = %q, want %q", c.Name, "my-campaign")
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
	reg.Register("test-id-123", "test-campaign", "/tmp/test", CampaignTypeProduct)

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
	c, ok := loaded.GetByID("test-id-123")
	if !ok {
		t.Fatal("test-campaign not found in loaded registry")
	}
	if c.Path != "/tmp/test" {
		t.Errorf("Path = %q, want %q", c.Path, "/tmp/test")
	}
	if c.Name != "test-campaign" {
		t.Errorf("Name = %q, want %q", c.Name, "test-campaign")
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

	reg.Register("test-id", "test", "/path/to/test", CampaignTypeProduct)

	c, ok := reg.Campaigns["test-id"]
	if !ok {
		t.Fatal("campaign not found after register")
	}
	if c.ID != "test-id" {
		t.Errorf("ID = %q, want %q", c.ID, "test-id")
	}
	if c.Name != "test" {
		t.Errorf("Name = %q, want %q", c.Name, "test")
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
	reg.Register("test-id", "test", "/path/to/test", CampaignTypeProduct)

	if reg.Campaigns == nil {
		t.Error("Campaigns should be initialized")
	}
	if _, ok := reg.Campaigns["test-id"]; !ok {
		t.Error("campaign not found after register")
	}
}

func TestRegistry_UnregisterByID(t *testing.T) {
	reg := NewRegistry()
	reg.Register("test-id", "test", "/path/to/test", CampaignTypeProduct)

	reg.UnregisterByID("test-id")

	if _, ok := reg.Campaigns["test-id"]; ok {
		t.Error("campaign should be removed after unregister")
	}
}

func TestRegistry_UnregisterByID_NilMap(t *testing.T) {
	reg := &Registry{} // Campaigns is nil

	// Should not panic
	reg.UnregisterByID("test-id")
}

func TestRegistry_UnregisterByName(t *testing.T) {
	reg := NewRegistry()
	reg.Register("test-id", "test", "/path/to/test", CampaignTypeProduct)

	ok := reg.UnregisterByName("test")
	if !ok {
		t.Error("UnregisterByName() should return true for existing campaign")
	}

	if _, found := reg.Campaigns["test-id"]; found {
		t.Error("campaign should be removed after unregister")
	}

	// Try unregistering nonexistent
	ok = reg.UnregisterByName("nonexistent")
	if ok {
		t.Error("UnregisterByName() should return false for nonexistent campaign")
	}
}

func TestRegistry_GetByID(t *testing.T) {
	reg := NewRegistry()
	reg.Register("test-id", "test", "/path/to/test", CampaignTypeProduct)

	c, ok := reg.GetByID("test-id")
	if !ok {
		t.Fatal("GetByID() returned false for existing campaign")
	}
	if c.Path != "/path/to/test" {
		t.Errorf("Path = %q, want %q", c.Path, "/path/to/test")
	}

	_, ok = reg.GetByID("nonexistent")
	if ok {
		t.Error("GetByID() returned true for nonexistent campaign")
	}
}

func TestRegistry_GetByName(t *testing.T) {
	reg := NewRegistry()
	reg.Register("test-id", "test", "/path/to/test", CampaignTypeProduct)

	c, ok := reg.GetByName("test")
	if !ok {
		t.Fatal("GetByName() returned false for existing campaign")
	}
	if c.Path != "/path/to/test" {
		t.Errorf("Path = %q, want %q", c.Path, "/path/to/test")
	}
	if c.ID != "test-id" {
		t.Errorf("ID = %q, want %q", c.ID, "test-id")
	}

	_, ok = reg.GetByName("nonexistent")
	if ok {
		t.Error("GetByName() returned true for nonexistent campaign")
	}
}

func TestRegistry_GetByIDPrefix(t *testing.T) {
	reg := NewRegistry()
	reg.Register("550e8400-e29b-41d4-a716-446655440000", "test1", "/path/to/test1", CampaignTypeProduct)
	reg.Register("a1b2c3d4-e5f6-7890-abcd-ef1234567890", "test2", "/path/to/test2", CampaignTypeResearch)

	// Full ID match
	c, err := reg.GetByIDPrefix("550e8400-e29b-41d4-a716-446655440000")
	if err != nil {
		t.Fatalf("GetByIDPrefix() error = %v", err)
	}
	if c.Name != "test1" {
		t.Errorf("Name = %q, want %q", c.Name, "test1")
	}

	// Prefix match (unique)
	c, err = reg.GetByIDPrefix("550e84")
	if err != nil {
		t.Fatalf("GetByIDPrefix() error = %v", err)
	}
	if c.Name != "test1" {
		t.Errorf("Name = %q, want %q", c.Name, "test1")
	}

	// Another prefix match
	c, err = reg.GetByIDPrefix("a1b2c3")
	if err != nil {
		t.Fatalf("GetByIDPrefix() error = %v", err)
	}
	if c.Name != "test2" {
		t.Errorf("Name = %q, want %q", c.Name, "test2")
	}

	// Nonexistent prefix
	_, err = reg.GetByIDPrefix("xyz")
	if err != ErrCampaignNotFound {
		t.Errorf("GetByIDPrefix() error = %v, want %v", err, ErrCampaignNotFound)
	}
}

func TestRegistry_GetByIDPrefix_MultipleMatches(t *testing.T) {
	reg := NewRegistry()
	reg.Register("abc123-xxx", "test1", "/path/to/test1", CampaignTypeProduct)
	reg.Register("abc456-yyy", "test2", "/path/to/test2", CampaignTypeResearch)

	// Prefix matches multiple
	_, err := reg.GetByIDPrefix("abc")
	if err != ErrMultipleMatches {
		t.Errorf("GetByIDPrefix() error = %v, want %v", err, ErrMultipleMatches)
	}
}

func TestRegistry_Get(t *testing.T) {
	reg := NewRegistry()
	reg.Register("550e8400-e29b-41d4-a716-446655440000", "my-campaign", "/path/to/test", CampaignTypeProduct)

	// Get by full ID
	c, ok := reg.Get("550e8400-e29b-41d4-a716-446655440000")
	if !ok {
		t.Fatal("Get() returned false for full ID")
	}
	if c.Name != "my-campaign" {
		t.Errorf("Name = %q, want %q", c.Name, "my-campaign")
	}

	// Get by ID prefix
	c, ok = reg.Get("550e84")
	if !ok {
		t.Fatal("Get() returned false for ID prefix")
	}
	if c.Name != "my-campaign" {
		t.Errorf("Name = %q, want %q", c.Name, "my-campaign")
	}

	// Get by name
	c, ok = reg.Get("my-campaign")
	if !ok {
		t.Fatal("Get() returned false for name")
	}
	if c.ID != "550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("ID = %q, want %q", c.ID, "550e8400-e29b-41d4-a716-446655440000")
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
	reg.Register("test-id", "test", "/path/to/test", CampaignTypeProduct)

	// Get initial time
	c1, _ := reg.GetByID("test-id")
	initial := c1.LastAccess

	// Wait a bit and update
	time.Sleep(1 * time.Millisecond)
	reg.UpdateLastAccess("test-id")

	// Get updated time
	c2, _ := reg.GetByID("test-id")
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

func TestRegistry_ListIDs(t *testing.T) {
	reg := NewRegistry()
	reg.Register("id-alpha", "alpha", "/path/to/alpha", CampaignTypeProduct)
	reg.Register("id-beta", "beta", "/path/to/beta", CampaignTypeResearch)
	reg.Register("id-gamma", "gamma", "/path/to/gamma", CampaignTypeTools)

	ids := reg.ListIDs()
	if len(ids) != 3 {
		t.Errorf("len(ListIDs()) = %d, want 3", len(ids))
	}

	// Sort for comparison
	sort.Strings(ids)
	expected := []string{"id-alpha", "id-beta", "id-gamma"}
	for i, id := range ids {
		if id != expected[i] {
			t.Errorf("ids[%d] = %q, want %q", i, id, expected[i])
		}
	}
}

func TestRegistry_List(t *testing.T) {
	reg := NewRegistry()
	reg.Register("id-alpha", "alpha", "/path/to/alpha", CampaignTypeProduct)
	reg.Register("id-beta", "beta", "/path/to/beta", CampaignTypeResearch)
	reg.Register("id-gamma", "gamma", "/path/to/gamma", CampaignTypeTools)

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

	reg.Register("test-id", "test", "/path", CampaignTypeProduct)
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
	reg.Register("test-id", "test", "/path/to/test", CampaignTypeProduct)

	c, ok := reg.FindByPath("/path/to/test")
	if !ok {
		t.Fatal("FindByPath() returned false for existing path")
	}
	if c.Name != "test" {
		t.Errorf("Name = %q, want %q", c.Name, "test")
	}
	if c.ID != "test-id" {
		t.Errorf("ID = %q, want %q", c.ID, "test-id")
	}
	if c.Path != "/path/to/test" {
		t.Errorf("Path = %q, want %q", c.Path, "/path/to/test")
	}

	_, ok = reg.FindByPath("/nonexistent")
	if ok {
		t.Error("FindByPath() returned true for nonexistent path")
	}
}

func TestRegistry_FindByPath_NilMap(t *testing.T) {
	reg := &Registry{} // Campaigns is nil

	_, ok := reg.FindByPath("/path")
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
	reg.Register("id-1", "campaign-1", "/home/user/c1", CampaignTypeProduct)
	reg.Register("id-2", "campaign-2", "/home/user/c2", CampaignTypeResearch)

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
	if c.ID != "id-1" {
		t.Errorf("campaign-1 ID = %q, want %q", c.ID, "id-1")
	}
}
