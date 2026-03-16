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

	configContent := `
name: test-campaign
type: product
description: A test campaign
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
	// Paths should have defaults from jumps.yaml
	if cfg.Paths().Projects != "projects/" {
		t.Errorf("Paths().Projects = %q, want %q", cfg.Paths().Projects, "projects/")
	}
	if cfg.Paths().Worktrees != "projects/worktrees/" {
		t.Errorf("Paths().Worktrees = %q, want %q", cfg.Paths().Worktrees, "projects/worktrees/")
	}

	// Verify jumps.yaml was created
	if !JumpsConfigExists(tmpDir) {
		t.Error("jumps.yaml should have been created")
	}
}

func TestLoadCampaignConfig_DefaultsApplied(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	campaignDir := filepath.Join(tmpDir, CampaignDir)
	if err := os.MkdirAll(campaignDir, 0755); err != nil {
		t.Fatalf("failed to create campaign dir: %v", err)
	}

	// Minimal config - just name, no type
	configContent := `name: minimal`
	configPath := filepath.Join(campaignDir, CampaignConfigFile)
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write campaign config: %v", err)
	}

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

func TestLoadCampaignConfig_PreservesLegacyDungeonConceptList(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	campaignDir := filepath.Join(tmpDir, CampaignDir)
	if err := os.MkdirAll(campaignDir, 0755); err != nil {
		t.Fatalf("failed to create campaign dir: %v", err)
	}

	configContent := `
name: legacy-campaign
type: product
concepts:
  - name: design
    path: workflow/design/
    description: Design docs
  - name: dungeon
    path: dungeon/
    description: Legacy dungeon concept
`
	configPath := filepath.Join(campaignDir, CampaignConfigFile)
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write campaign config: %v", err)
	}

	ctx := context.Background()
	cfg, err := LoadCampaignConfig(ctx, tmpDir)
	if err != nil {
		t.Fatalf("LoadCampaignConfig() error = %v", err)
	}

	if len(cfg.ConceptList) != 2 {
		t.Fatalf("len(ConceptList) = %d, want 2", len(cfg.ConceptList))
	}

	var hasDungeon bool
	for _, c := range cfg.ConceptList {
		if c.Name == "dungeon" && c.Path == "dungeon/" {
			hasDungeon = true
			break
		}
	}
	if !hasDungeon {
		t.Fatal("legacy dungeon concept entry should be preserved when explicitly configured")
	}
}

func TestLoadCampaignConfig_PreservesLegacyShortcutsWithoutExplore(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	campaignDir := filepath.Join(tmpDir, CampaignDir)
	settingsDir := filepath.Join(campaignDir, SettingsDir)
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatalf("failed to create settings dir: %v", err)
	}

	configContent := `
name: legacy-shortcuts
type: product
`
	if err := os.WriteFile(filepath.Join(campaignDir, CampaignConfigFile), []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write campaign config: %v", err)
	}

	// Legacy jumps config intentionally omits the new "ex" shortcut.
	jumpsContent := `
paths:
  workflow: workflow/
  design: workflow/design/
  dungeon: dungeon/
shortcuts:
  de:
    path: workflow/design/
    description: Jump to design
    source: auto
  du:
    path: dungeon/
    description: Jump to dungeon directory
    source: auto
`
	if err := os.WriteFile(filepath.Join(settingsDir, JumpsConfigFile), []byte(jumpsContent), 0644); err != nil {
		t.Fatalf("failed to write jumps config: %v", err)
	}

	ctx := context.Background()
	cfg, err := LoadCampaignConfig(ctx, tmpDir)
	if err != nil {
		t.Fatalf("LoadCampaignConfig() error = %v", err)
	}

	shortcuts := cfg.Shortcuts()
	if _, ok := shortcuts["de"]; !ok {
		t.Fatal("legacy design shortcut de should be preserved")
	}
	if _, ok := shortcuts["du"]; !ok {
		t.Fatal("legacy dungeon shortcut du should be preserved")
	}
	if _, ok := shortcuts["ex"]; ok {
		t.Fatal("legacy shortcuts should not be silently rewritten with new ex shortcut")
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
	if err := os.MkdirAll(campaignDir, 0755); err != nil {
		t.Fatalf("failed to create campaign dir: %v", err)
	}

	// Invalid YAML
	configPath := filepath.Join(campaignDir, CampaignConfigFile)
	if err := os.WriteFile(configPath, []byte("name: [invalid yaml"), 0644); err != nil {
		t.Fatalf("failed to write campaign config: %v", err)
	}

	ctx := context.Background()
	_, err := LoadCampaignConfig(ctx, tmpDir)
	if err == nil {
		t.Error("LoadCampaignConfig() expected error for invalid YAML")
	}
}

func TestLoadCampaignConfig_MissingName(t *testing.T) {
	tmpDir := t.TempDir()
	campaignDir := filepath.Join(tmpDir, CampaignDir)
	if err := os.MkdirAll(campaignDir, 0755); err != nil {
		t.Fatalf("failed to create campaign dir: %v", err)
	}

	// Missing required name field
	configContent := `type: product`
	configPath := filepath.Join(campaignDir, CampaignConfigFile)
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write campaign config: %v", err)
	}

	ctx := context.Background()
	_, err := LoadCampaignConfig(ctx, tmpDir)
	if err == nil {
		t.Error("LoadCampaignConfig() expected error for missing name")
	}
}

func TestLoadCampaignConfig_InvalidType(t *testing.T) {
	tmpDir := t.TempDir()
	campaignDir := filepath.Join(tmpDir, CampaignDir)
	if err := os.MkdirAll(campaignDir, 0755); err != nil {
		t.Fatalf("failed to create campaign dir: %v", err)
	}

	configContent := `
name: test
	type: invalid-type
`
	configPath := filepath.Join(campaignDir, CampaignConfigFile)
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write campaign config: %v", err)
	}

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

	if err := os.MkdirAll(campaignDir, 0755); err != nil {
		t.Fatalf("failed to create campaign dir: %v", err)
	}
	if err := os.MkdirAll(nestedDir, 0755); err != nil {
		t.Fatalf("failed to create nested dir: %v", err)
	}

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
