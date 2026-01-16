package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadGlobalConfig_Defaults(t *testing.T) {
	// Use temp XDG_CONFIG_HOME
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	ctx := context.Background()
	cfg, err := LoadGlobalConfig(ctx)
	if err != nil {
		t.Fatalf("LoadGlobalConfig() error = %v", err)
	}

	if cfg.DefaultType != CampaignTypeProduct {
		t.Errorf("DefaultType = %q, want %q", cfg.DefaultType, CampaignTypeProduct)
	}
}

func TestLoadGlobalConfig_FromFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	// Create config file
	configDir := filepath.Join(dir, AppName)
	os.MkdirAll(configDir, 0755)

	configContent := `
default_type: research
editor: nvim
no_color: true
`
	configPath := filepath.Join(configDir, "config.yaml")
	os.WriteFile(configPath, []byte(configContent), 0644)

	ctx := context.Background()
	cfg, err := LoadGlobalConfig(ctx)
	if err != nil {
		t.Fatalf("LoadGlobalConfig() error = %v", err)
	}

	if cfg.DefaultType != CampaignTypeResearch {
		t.Errorf("DefaultType = %q, want %q", cfg.DefaultType, CampaignTypeResearch)
	}
	if cfg.Editor != "nvim" {
		t.Errorf("Editor = %q, want %q", cfg.Editor, "nvim")
	}
	if !cfg.NoColor {
		t.Error("NoColor = false, want true")
	}
}

func TestLoadGlobalConfig_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	configDir := filepath.Join(dir, AppName)
	os.MkdirAll(configDir, 0755)

	configPath := filepath.Join(configDir, "config.yaml")
	os.WriteFile(configPath, []byte("invalid: [yaml"), 0644)

	ctx := context.Background()
	_, err := LoadGlobalConfig(ctx)
	if err == nil {
		t.Error("LoadGlobalConfig() expected error for invalid YAML")
	}
}

func TestLoadGlobalConfig_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := LoadGlobalConfig(ctx)
	if err != context.Canceled {
		t.Errorf("LoadGlobalConfig() error = %v, want %v", err, context.Canceled)
	}
}

func TestSaveGlobalConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg := &GlobalConfig{
		DefaultType: CampaignTypeTools,
		Editor:      "code",
		NoColor:     false,
	}

	ctx := context.Background()
	err := SaveGlobalConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("SaveGlobalConfig() error = %v", err)
	}

	// Verify file was created
	path := GlobalConfigPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("config file was not created")
	}

	// Load and verify
	loaded, err := LoadGlobalConfig(ctx)
	if err != nil {
		t.Fatalf("LoadGlobalConfig() error = %v", err)
	}

	if loaded.DefaultType != cfg.DefaultType {
		t.Errorf("loaded DefaultType = %q, want %q", loaded.DefaultType, cfg.DefaultType)
	}
	if loaded.Editor != cfg.Editor {
		t.Errorf("loaded Editor = %q, want %q", loaded.Editor, cfg.Editor)
	}
}

func TestSaveGlobalConfig_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cfg := &GlobalConfig{}
	err := SaveGlobalConfig(ctx, cfg)
	if err != context.Canceled {
		t.Errorf("SaveGlobalConfig() error = %v, want %v", err, context.Canceled)
	}
}

func TestInitGlobalConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	ctx := context.Background()

	// Init should create config
	err := InitGlobalConfig(ctx)
	if err != nil {
		t.Fatalf("InitGlobalConfig() error = %v", err)
	}

	// File should exist
	path := GlobalConfigPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("config file was not created")
	}

	// Second init should be a no-op
	err = InitGlobalConfig(ctx)
	if err != nil {
		t.Fatalf("InitGlobalConfig() second call error = %v", err)
	}
}

func TestInitGlobalConfig_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := InitGlobalConfig(ctx)
	if err != context.Canceled {
		t.Errorf("InitGlobalConfig() error = %v, want %v", err, context.Canceled)
	}
}

func TestConfigDir_XDG(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	got := ConfigDir()
	want := filepath.Join(dir, AppName)
	if got != want {
		t.Errorf("ConfigDir() = %q, want %q", got, want)
	}
}

func TestConfigDir_Default(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")

	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".config", AppName)

	got := ConfigDir()
	if got != want {
		t.Errorf("ConfigDir() = %q, want %q", got, want)
	}
}

func TestGlobalConfigPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	got := GlobalConfigPath()
	want := filepath.Join(dir, AppName, "config.yaml")
	if got != want {
		t.Errorf("GlobalConfigPath() = %q, want %q", got, want)
	}
}

func TestLoadGlobalConfig_Timeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(1 * time.Millisecond)

	_, err := LoadGlobalConfig(ctx)
	if err != context.DeadlineExceeded {
		t.Errorf("LoadGlobalConfig() error = %v, want %v", err, context.DeadlineExceeded)
	}
}
