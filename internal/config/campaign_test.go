package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadCampaignConfig(t *testing.T) {
	// Create temp campaign
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	campaignDir := filepath.Join(tmpDir, CampaignDir)
	if err := os.MkdirAll(campaignDir, 0755); err != nil {
		t.Fatalf("failed to create campaign dir: %v", err)
	}

	// Legacy config with paths: block (should trigger migration)
	configContent := `
name: test-campaign
type: product
description: A test campaign
paths:
  projects: src/
`
	configPath := filepath.Join(campaignDir, CampaignConfigFile)
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	ctx := context.Background()
	cfg, err := LoadCampaignConfig(ctx, tmpDir)
	if err != nil {
		t.Fatalf("LoadCampaignConfig() error = %v", err)
	}

	if cfg.Name != "test-campaign" {
		t.Errorf("Name = %q, want %q", cfg.Name, "test-campaign")
	}
	if cfg.Type != CampaignTypeProduct {
		t.Errorf("Type = %q, want %q", cfg.Type, CampaignTypeProduct)
	}
	if cfg.Description != "A test campaign" {
		t.Errorf("Description = %q, want %q", cfg.Description, "A test campaign")
	}
	// Custom path should be migrated to jumps.yaml and accessible via Paths()
	if cfg.Paths().Projects != "src/" {
		t.Errorf("Paths().Projects = %q, want %q", cfg.Paths().Projects, "src/")
	}
	// Default should be applied for missing path
	if cfg.Paths().Worktrees != "projects/worktrees/" {
		t.Errorf("Paths().Worktrees = %q, want %q (default)", cfg.Paths().Worktrees, "projects/worktrees/")
	}

	// Verify jumps.yaml was created (migration)
	if !JumpsConfigExists(tmpDir) {
		t.Error("jumps.yaml should have been created during migration")
	}
}

func TestLoadCampaignConfig_DefaultsApplied(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	campaignDir := filepath.Join(tmpDir, CampaignDir)
	os.MkdirAll(campaignDir, 0755)

	// Minimal config - just name, no type
	configContent := `name: minimal`
	configPath := filepath.Join(campaignDir, CampaignConfigFile)
	os.WriteFile(configPath, []byte(configContent), 0644)

	ctx := context.Background()
	cfg, err := LoadCampaignConfig(ctx, tmpDir)
	if err != nil {
		t.Fatalf("LoadCampaignConfig() error = %v", err)
	}

	// Type should default to product
	if cfg.Type != CampaignTypeProduct {
		t.Errorf("Type = %q, want %q (default)", cfg.Type, CampaignTypeProduct)
	}

	// All paths should have defaults (from jumps.yaml which is auto-created)
	defaults := DefaultCampaignPaths()
	if cfg.Paths().Projects != defaults.Projects {
		t.Errorf("Paths().Projects = %q, want %q", cfg.Paths().Projects, defaults.Projects)
	}
}

func TestLoadCampaignConfig_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	_, err := LoadCampaignConfig(ctx, tmpDir)
	if err == nil {
		t.Error("LoadCampaignConfig() expected error for missing config")
	}
}

func TestLoadCampaignConfig_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	campaignDir := filepath.Join(tmpDir, CampaignDir)
	os.MkdirAll(campaignDir, 0755)

	// Invalid YAML
	configPath := filepath.Join(campaignDir, CampaignConfigFile)
	os.WriteFile(configPath, []byte("name: [invalid yaml"), 0644)

	ctx := context.Background()
	_, err := LoadCampaignConfig(ctx, tmpDir)
	if err == nil {
		t.Error("LoadCampaignConfig() expected error for invalid YAML")
	}
}

func TestLoadCampaignConfig_MissingName(t *testing.T) {
	tmpDir := t.TempDir()
	campaignDir := filepath.Join(tmpDir, CampaignDir)
	os.MkdirAll(campaignDir, 0755)

	// Missing required name field
	configContent := `type: product`
	configPath := filepath.Join(campaignDir, CampaignConfigFile)
	os.WriteFile(configPath, []byte(configContent), 0644)

	ctx := context.Background()
	_, err := LoadCampaignConfig(ctx, tmpDir)
	if err == nil {
		t.Error("LoadCampaignConfig() expected error for missing name")
	}
}

func TestLoadCampaignConfig_InvalidType(t *testing.T) {
	tmpDir := t.TempDir()
	campaignDir := filepath.Join(tmpDir, CampaignDir)
	os.MkdirAll(campaignDir, 0755)

	configContent := `
name: test
type: invalid-type
`
	configPath := filepath.Join(campaignDir, CampaignConfigFile)
	os.WriteFile(configPath, []byte(configContent), 0644)

	ctx := context.Background()
	_, err := LoadCampaignConfig(ctx, tmpDir)
	if err == nil {
		t.Error("LoadCampaignConfig() expected error for invalid type")
	}
}

func TestLoadCampaignConfig_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := LoadCampaignConfig(ctx, "/some/path")
	if err != context.Canceled {
		t.Errorf("LoadCampaignConfig() error = %v, want %v", err, context.Canceled)
	}
}

func TestLoadCampaignConfig_ContextTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(1 * time.Millisecond)

	_, err := LoadCampaignConfig(ctx, "/some/path")
	if err != context.DeadlineExceeded {
		t.Errorf("LoadCampaignConfig() error = %v, want %v", err, context.DeadlineExceeded)
	}
}

func TestSaveCampaignConfig(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	cfg := &CampaignConfig{
		Name:        "saved-campaign",
		Type:        CampaignTypeResearch,
		Description: "A saved campaign",
	}

	ctx := context.Background()
	err := SaveCampaignConfig(ctx, tmpDir, cfg)
	if err != nil {
		t.Fatalf("SaveCampaignConfig() error = %v", err)
	}

	// Verify the file was created
	configPath := CampaignConfigPath(tmpDir)
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("config file was not created")
	}

	// Load it back and verify (this will auto-create jumps.yaml with defaults)
	loaded, err := LoadCampaignConfig(ctx, tmpDir)
	if err != nil {
		t.Fatalf("LoadCampaignConfig() error = %v", err)
	}

	if loaded.Name != cfg.Name {
		t.Errorf("loaded Name = %q, want %q", loaded.Name, cfg.Name)
	}
	if loaded.Type != cfg.Type {
		t.Errorf("loaded Type = %q, want %q", loaded.Type, cfg.Type)
	}
}

func TestSaveCampaignConfig_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cfg := &CampaignConfig{Name: "test", Type: CampaignTypeProduct}
	err := SaveCampaignConfig(ctx, "/some/path", cfg)
	if err != context.Canceled {
		t.Errorf("SaveCampaignConfig() error = %v, want %v", err, context.Canceled)
	}
}

func TestCampaignConfigPath(t *testing.T) {
	got := CampaignConfigPath("/foo/bar")
	want := filepath.Join("/foo/bar", CampaignDir, CampaignConfigFile)
	if got != want {
		t.Errorf("CampaignConfigPath() = %q, want %q", got, want)
	}
}

func TestFindCampaignRoot(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	// Create campaign structure
	campaignRoot := filepath.Join(tmpDir, "my-campaign")
	campaignDir := filepath.Join(campaignRoot, CampaignDir)
	nestedDir := filepath.Join(campaignRoot, "projects", "subproject")

	os.MkdirAll(campaignDir, 0755)
	os.MkdirAll(nestedDir, 0755)

	ctx := context.Background()

	// Test from nested directory
	got, err := findCampaignRoot(ctx, nestedDir)
	if err != nil {
		t.Fatalf("findCampaignRoot() error = %v", err)
	}
	if got != campaignRoot {
		t.Errorf("findCampaignRoot() = %q, want %q", got, campaignRoot)
	}
}

func TestFindCampaignRoot_NotInCampaign(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	_, err := findCampaignRoot(ctx, tmpDir)
	if err == nil {
		t.Error("findCampaignRoot() expected error when not in campaign")
	}
}
