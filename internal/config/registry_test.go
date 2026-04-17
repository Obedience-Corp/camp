package config

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
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

	configDir := filepath.Join(dir, OrgName, AppName)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	// JSON format: campaigns keyed by ID with name field
	registryContent := `{
  "version": 2,
  "campaigns": {
    "550e8400-e29b-41d4-a716-446655440000": {
      "name": "my-campaign",
      "path": "/home/user/my-campaign",
      "type": "product"
    },
    "a1b2c3d4-e5f6-7890-abcd-ef1234567890": {
      "name": "other-campaign",
      "path": "/home/user/other",
      "type": "research"
    }
  }
}`
	registryPath := filepath.Join(configDir, "registry.json")
	if err := os.WriteFile(registryPath, []byte(registryContent), 0644); err != nil {
		t.Fatalf("failed to write registry file: %v", err)
	}

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
	if err := reg.Register("test-id-123", "test-campaign", "/tmp/test", CampaignTypeProduct); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

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

	if err := reg.Register("test-id", "test", "/path/to/test", CampaignTypeProduct); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

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
	if err := reg.Register("test-id", "test", "/path/to/test", CampaignTypeProduct); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if reg.Campaigns == nil {
		t.Error("Campaigns should be initialized")
	}
	if _, ok := reg.Campaigns["test-id"]; !ok {
		t.Error("campaign not found after register")
	}
}

func TestRegistry_UnregisterByID(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register("test-id", "test", "/path/to/test", CampaignTypeProduct); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

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
	if err := reg.Register("test-id", "test", "/path/to/test", CampaignTypeProduct); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

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
	if err := reg.Register("test-id", "test", "/path/to/test", CampaignTypeProduct); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

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
	if err := reg.Register("test-id", "test", "/path/to/test", CampaignTypeProduct); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

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
	if err := reg.Register("550e8400-e29b-41d4-a716-446655440000", "test1", "/path/to/test1", CampaignTypeProduct); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := reg.Register("a1b2c3d4-e5f6-7890-abcd-ef1234567890", "test2", "/path/to/test2", CampaignTypeResearch); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

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
	if err := reg.Register("abc123-xxx", "test1", "/path/to/test1", CampaignTypeProduct); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := reg.Register("abc456-yyy", "test2", "/path/to/test2", CampaignTypeResearch); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	// Prefix matches multiple
	_, err := reg.GetByIDPrefix("abc")
	if err != ErrMultipleMatches {
		t.Errorf("GetByIDPrefix() error = %v, want %v", err, ErrMultipleMatches)
	}
}

func TestRegistry_Get(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register("550e8400-e29b-41d4-a716-446655440000", "my-campaign", "/path/to/test", CampaignTypeProduct); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

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
	if err := reg.Register("test-id", "test", "/path/to/test", CampaignTypeProduct); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

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
	if err := reg.Register("id-alpha", "alpha", "/path/to/alpha", CampaignTypeProduct); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := reg.Register("id-beta", "beta", "/path/to/beta", CampaignTypeResearch); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := reg.Register("id-gamma", "gamma", "/path/to/gamma", CampaignTypeTools); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

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
	if err := reg.Register("id-alpha", "alpha", "/path/to/alpha", CampaignTypeProduct); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := reg.Register("id-beta", "beta", "/path/to/beta", CampaignTypeResearch); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := reg.Register("id-gamma", "gamma", "/path/to/gamma", CampaignTypeTools); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

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

	if err := reg.Register("test-id", "test", "/path", CampaignTypeProduct); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
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
	if err := reg.Register("test-id", "test", "/path/to/test", CampaignTypeProduct); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

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
	want := filepath.Join(dir, OrgName, AppName, "registry.json")
	if got != want {
		t.Errorf("RegistryPath() = %q, want %q", got, want)
	}
}

func TestRegistryPath_Override(t *testing.T) {
	dir := t.TempDir()
	customPath := filepath.Join(dir, "custom-registry.json")
	t.Setenv("CAMP_REGISTRY_PATH", customPath)

	got := RegistryPath()
	if got != customPath {
		t.Errorf("RegistryPath() = %q, want %q", got, customPath)
	}
}

func TestRegistryRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	reg := NewRegistry()
	if err := reg.Register("id-1", "campaign-1", "/home/user/c1", CampaignTypeProduct); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := reg.Register("id-2", "campaign-2", "/home/user/c2", CampaignTypeResearch); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

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

func TestRegistry_Register_EmptyID(t *testing.T) {
	reg := NewRegistry()

	err := reg.Register("", "test", "/path/to/test", CampaignTypeProduct)
	if err != ErrEmptyID {
		t.Errorf("Register() error = %v, want %v", err, ErrEmptyID)
	}
}

func TestRegistry_Register_PathConflict(t *testing.T) {
	reg := NewRegistry()

	// Register first campaign
	if err := reg.Register("id-1", "campaign-1", "/path/to/test", CampaignTypeProduct); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	// Try to register different ID with same path
	err := reg.Register("id-2", "campaign-2", "/path/to/test", CampaignTypeResearch)
	if err == nil {
		t.Error("Register() expected error for path conflict, got nil")
	}
	if err != nil && !errors.Is(err, ErrPathConflict) {
		t.Errorf("Register() error = %v, want ErrPathConflict", err)
	}
}

func TestRegistry_Register_UpdatePath(t *testing.T) {
	reg := NewRegistry()

	// Register campaign
	if err := reg.Register("id-1", "campaign", "/old/path", CampaignTypeProduct); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	// Update same ID with new path (campaign moved)
	if err := reg.Register("id-1", "campaign", "/new/path", CampaignTypeProduct); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	// Verify path is updated
	c, ok := reg.GetByID("id-1")
	if !ok {
		t.Fatal("campaign not found after path update")
	}
	if c.Path != "/new/path" {
		t.Errorf("Path = %q, want %q", c.Path, "/new/path")
	}

	// Verify old path is no longer in index
	_, found := reg.FindByPath("/old/path")
	if found {
		t.Error("old path should not be found after update")
	}

	// Verify new path is in index
	_, found = reg.FindByPath("/new/path")
	if !found {
		t.Error("new path should be found after update")
	}
}

// createTestCampaign creates a minimal campaign directory with campaign.yaml
func createTestCampaign(t *testing.T, root, id, name string, campaignType CampaignType) {
	t.Helper()
	campaignDir := filepath.Join(root, CampaignDir)
	if err := os.MkdirAll(campaignDir, 0755); err != nil {
		t.Fatalf("failed to create campaign dir: %v", err)
	}

	// Create minimal campaign.yaml
	content := "id: " + id + "\nname: " + name + "\ntype: " + string(campaignType) + "\n"
	configPath := filepath.Join(campaignDir, CampaignConfigFile)
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write campaign.yaml: %v", err)
	}
}

func TestRegistry_VerifyAndRepair_EmptyRegistry(t *testing.T) {
	reg := NewRegistry()
	ctx := context.Background()

	report, err := reg.VerifyAndRepair(ctx)
	if err != nil {
		t.Fatalf("VerifyAndRepair() error = %v", err)
	}

	if report.HasChanges() {
		t.Error("empty registry should have no changes")
	}
	if report.TotalVerified != 0 {
		t.Errorf("TotalVerified = %d, want 0", report.TotalVerified)
	}
}

func TestRegistry_VerifyAndRepair_ContextCancelled(t *testing.T) {
	reg := NewRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := reg.VerifyAndRepair(ctx)
	if err != context.Canceled {
		t.Errorf("VerifyAndRepair() error = %v, want %v", err, context.Canceled)
	}
}

func TestRegistry_VerifyAndRepair_RemoveMissingPath(t *testing.T) {
	reg := NewRegistry()
	// Register campaign with nonexistent path
	if err := reg.Register("test-id", "test", "/nonexistent/path", CampaignTypeProduct); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	ctx := context.Background()
	report, err := reg.VerifyAndRepair(ctx)
	if err != nil {
		t.Fatalf("VerifyAndRepair() error = %v", err)
	}

	if !report.HasChanges() {
		t.Error("expected changes in report")
	}
	if len(report.Removed) != 1 {
		t.Errorf("len(Removed) = %d, want 1", len(report.Removed))
	}
	if report.Removed[0].Reason != "path does not exist" {
		t.Errorf("Reason = %q, want 'path does not exist'", report.Removed[0].Reason)
	}

	// Verify entry was removed
	if _, ok := reg.GetByID("test-id"); ok {
		t.Error("campaign should be removed from registry")
	}
}

func TestRegistry_VerifyAndRepair_RemoveNoCampaignYaml(t *testing.T) {
	dir := t.TempDir()

	reg := NewRegistry()
	// Register campaign pointing to directory without .campaign/campaign.yaml
	if err := reg.Register("test-id", "test", dir, CampaignTypeProduct); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	ctx := context.Background()
	report, err := reg.VerifyAndRepair(ctx)
	if err != nil {
		t.Fatalf("VerifyAndRepair() error = %v", err)
	}

	if len(report.Removed) != 1 {
		t.Errorf("len(Removed) = %d, want 1", len(report.Removed))
	}
	if report.Removed[0].Reason != "no campaign.yaml (not a campaign)" {
		t.Errorf("Reason = %q, want 'no campaign.yaml (not a campaign)'", report.Removed[0].Reason)
	}
}

func TestRegistry_VerifyAndRepair_IDMismatch(t *testing.T) {
	dir := t.TempDir()
	// Create campaign with actual ID
	actualID := "actual-campaign-id-12345"
	createTestCampaign(t, dir, actualID, "test-campaign", CampaignTypeProduct)

	reg := NewRegistry()
	// Register with wrong ID
	wrongID := "wrong-id-in-registry"
	if err := reg.Register(wrongID, "test-campaign", dir, CampaignTypeProduct); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	ctx := context.Background()
	report, err := reg.VerifyAndRepair(ctx)
	if err != nil {
		t.Fatalf("VerifyAndRepair() error = %v", err)
	}

	// Wrong ID should be removed
	if len(report.Removed) != 1 {
		t.Errorf("len(Removed) = %d, want 1", len(report.Removed))
	}
	if _, ok := reg.GetByID(wrongID); ok {
		t.Error("wrong ID should be removed from registry")
	}

	// Correct ID should be added
	if len(report.Added) != 1 {
		t.Errorf("len(Added) = %d, want 1", len(report.Added))
	}
	c, ok := reg.GetByID(actualID)
	if !ok {
		t.Error("correct ID should be added to registry")
	}
	if c.Path != dir {
		t.Errorf("Path = %q, want %q", c.Path, dir)
	}
}

func TestRegistry_VerifyAndRepair_UpdateNameAndType(t *testing.T) {
	dir := t.TempDir()
	id := "test-campaign-id"
	// Create campaign with updated name and type
	createTestCampaign(t, dir, id, "new-name", CampaignTypeResearch)

	reg := NewRegistry()
	// Register with old name and type
	if err := reg.Register(id, "old-name", dir, CampaignTypeProduct); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	ctx := context.Background()
	report, err := reg.VerifyAndRepair(ctx)
	if err != nil {
		t.Fatalf("VerifyAndRepair() error = %v", err)
	}

	// Should have updates, not removals
	if len(report.Removed) != 0 {
		t.Errorf("len(Removed) = %d, want 0", len(report.Removed))
	}
	if len(report.Updated) != 1 {
		t.Errorf("len(Updated) = %d, want 1", len(report.Updated))
	}
	if len(report.Updated[0].Changes) != 2 {
		t.Errorf("len(Changes) = %d, want 2", len(report.Updated[0].Changes))
	}

	// Verify entry was updated
	c, _ := reg.GetByID(id)
	if c.Name != "new-name" {
		t.Errorf("Name = %q, want 'new-name'", c.Name)
	}
	if c.Type != CampaignTypeResearch {
		t.Errorf("Type = %q, want %q", c.Type, CampaignTypeResearch)
	}
}

func TestRegistry_VerifyAndRepair_ValidCampaign(t *testing.T) {
	dir := t.TempDir()
	id := "test-campaign-id"
	createTestCampaign(t, dir, id, "test-campaign", CampaignTypeProduct)

	reg := NewRegistry()
	if err := reg.Register(id, "test-campaign", dir, CampaignTypeProduct); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	ctx := context.Background()
	report, err := reg.VerifyAndRepair(ctx)
	if err != nil {
		t.Fatalf("VerifyAndRepair() error = %v", err)
	}

	// No changes for valid campaign
	if report.HasChanges() {
		t.Error("valid campaign should have no changes")
	}
	if report.TotalVerified != 1 {
		t.Errorf("TotalVerified = %d, want 1", report.TotalVerified)
	}
}

func TestVerificationReport_HasChanges(t *testing.T) {
	tests := []struct {
		name     string
		report   VerificationReport
		expected bool
	}{
		{
			name:     "empty report",
			report:   VerificationReport{},
			expected: false,
		},
		{
			name: "has removed",
			report: VerificationReport{
				Removed: []RemovedEntry{{ID: "test"}},
			},
			expected: true,
		},
		{
			name: "has added",
			report: VerificationReport{
				Added: []AddedEntry{{ID: "test"}},
			},
			expected: true,
		},
		{
			name: "has updated",
			report: VerificationReport{
				Updated: []UpdatedEntry{{ID: "test"}},
			},
			expected: true,
		},
		{
			name: "has all",
			report: VerificationReport{
				Removed: []RemovedEntry{{ID: "r"}},
				Added:   []AddedEntry{{ID: "a"}},
				Updated: []UpdatedEntry{{ID: "u"}},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.report.HasChanges(); got != tt.expected {
				t.Errorf("HasChanges() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestRegistry_RoundTrip catches silent field drops between RegisteredCampaign
// and the on-disk registryfile.Campaign schema. SaveRegistry marshals the full
// Registry struct, but LoadRegistry reads through registryfile.File and copies
// fields manually. If a new JSON-tagged field is added to RegisteredCampaign
// without updating the load path, this test fails.
func TestRegistry_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	ctx := context.Background()

	original := NewRegistry()
	if err := original.Register(
		"550e8400-e29b-41d4-a716-446655440000",
		"my-campaign",
		"/tmp/my-campaign",
		CampaignTypeProduct,
	); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := original.Register(
		"a1b2c3d4-e5f6-7890-abcd-ef1234567890",
		"other-campaign",
		"/tmp/other-campaign",
		CampaignTypeResearch,
	); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if err := SaveRegistry(ctx, original); err != nil {
		t.Fatalf("SaveRegistry() error = %v", err)
	}

	loaded, err := LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("LoadRegistry() error = %v", err)
	}

	if loaded.Version != original.Version {
		t.Errorf("Version: loaded=%d, original=%d", loaded.Version, original.Version)
	}
	if len(loaded.Campaigns) != len(original.Campaigns) {
		t.Fatalf("Campaigns count: loaded=%d, original=%d", len(loaded.Campaigns), len(original.Campaigns))
	}

	for id, want := range original.Campaigns {
		got, ok := loaded.Campaigns[id]
		if !ok {
			t.Errorf("campaign %s missing after load", id)
			continue
		}
		// LastAccess timestamp serializes through time.Time JSON marshal which
		// loses sub-microsecond precision; compare via Equal instead of ==.
		if !got.LastAccess.Equal(want.LastAccess) {
			t.Errorf("campaign %s: LastAccess loaded=%v, original=%v", id, got.LastAccess, want.LastAccess)
		}
		got.LastAccess = want.LastAccess
		if !reflect.DeepEqual(got, want) {
			t.Errorf("campaign %s: round-trip differs\n  loaded   = %+v\n  original = %+v", id, got, want)
		}
	}
}

// TestRegisteredCampaign_AllJSONFieldsPersist enumerates every JSON-serialized
// field on RegisteredCampaign and verifies it survives a Save → Load round-trip.
// If a new JSON-tagged field is added without updating the load path in
// LoadRegistry, this test catches the silent drop.
func TestRegisteredCampaign_AllJSONFieldsPersist(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	// Build a campaign with every JSON-serialized field set to a non-zero value.
	full := RegisteredCampaign{
		ID:         "id-not-serialized", // ID uses json:"-" so excluded
		Name:       "test-name",
		Path:       "/tmp/test-path",
		Type:       CampaignTypeResearch,
		LastAccess: time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC),
	}

	reg := NewRegistry()
	reg.Version = RegistryVersion
	reg.Campaigns["test-id"] = full

	ctx := context.Background()
	if err := SaveRegistry(ctx, reg); err != nil {
		t.Fatalf("SaveRegistry() error = %v", err)
	}

	loaded, err := LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("LoadRegistry() error = %v", err)
	}

	loadedEntry, ok := loaded.Campaigns["test-id"]
	if !ok {
		t.Fatal("test-id missing after load")
	}

	// Iterate every exported field with a JSON tag (not "-") and assert the
	// loaded value is non-zero. A silently-dropped field would zero out here.
	rt := reflect.TypeOf(full)
	loadedV := reflect.ValueOf(loadedEntry)
	originalV := reflect.ValueOf(full)
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		jsonTag := field.Tag.Get("json")
		if jsonTag == "" || jsonTag == "-" {
			continue
		}
		// strip ",omitempty" suffix
		name := jsonTag
		for j := 0; j < len(jsonTag); j++ {
			if jsonTag[j] == ',' {
				name = jsonTag[:j]
				break
			}
		}
		gotField := loadedV.Field(i)
		wantField := originalV.Field(i)
		if gotField.IsZero() && !wantField.IsZero() {
			t.Errorf("field %q (%s) was dropped on load: loaded=%v, original=%v", field.Name, name, gotField.Interface(), wantField.Interface())
		}
	}
}

// TestRegistryFile_Schema_StaysAlignedWithRegisteredCampaign documents the
// invariant that on-disk JSON fields must match between the two types. If the
// JSON tags drift, future RegisteredCampaign fields won't roundtrip.
func TestRegistryFile_Schema_StaysAlignedWithRegisteredCampaign(t *testing.T) {
	// Marshal a RegisteredCampaign and re-unmarshal as a generic map to extract
	// the actual JSON keys written to disk by SaveRegistry.
	rc := RegisteredCampaign{
		Name:       "x",
		Path:       "/x",
		Type:       CampaignTypeProduct,
		LastAccess: time.Now(),
	}
	rcData, err := json.Marshal(rc)
	if err != nil {
		t.Fatalf("marshal RegisteredCampaign: %v", err)
	}
	var rcKeys map[string]any
	if err := json.Unmarshal(rcData, &rcKeys); err != nil {
		t.Fatalf("unmarshal RegisteredCampaign: %v", err)
	}

	// Write the same value through registryfile.Campaign by going through a
	// full Save → Load cycle and inspecting the file shape.
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	reg := NewRegistry()
	reg.Campaigns["k"] = rc
	if err := SaveRegistry(context.Background(), reg); err != nil {
		t.Fatalf("SaveRegistry: %v", err)
	}

	raw, err := os.ReadFile(RegistryPath())
	if err != nil {
		t.Fatalf("read registry file: %v", err)
	}
	var fileShape struct {
		Campaigns map[string]map[string]any `json:"campaigns"`
	}
	if err := json.Unmarshal(raw, &fileShape); err != nil {
		t.Fatalf("unmarshal file: %v", err)
	}

	persisted := fileShape.Campaigns["k"]
	for key := range rcKeys {
		if _, ok := persisted[key]; !ok {
			t.Errorf("RegisteredCampaign field %q is written but registryfile.Campaign cannot read it back; update internal/config/registryfile/registryfile.go", key)
		}
	}
}
