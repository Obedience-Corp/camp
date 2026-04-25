package config

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

	// Verify TUI defaults are applied
	if cfg.TUI.Theme != "adaptive" {
		t.Errorf("TUI.Theme = %q, want %q", cfg.TUI.Theme, "adaptive")
	}
}

func TestLoadGlobalConfig_AutoCreate(t *testing.T) {
	// Use temp XDG_CONFIG_HOME
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	ctx := context.Background()
	_, err := LoadGlobalConfig(ctx)
	if err != nil {
		t.Fatalf("LoadGlobalConfig() error = %v", err)
	}

	// Verify config file was auto-created
	path := GlobalConfigPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("config file was not auto-created on first load")
	}

	// Verify it's valid JSON
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read auto-created config: %v", err)
	}

	var cfg GlobalConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Errorf("auto-created config is not valid JSON: %v", err)
	}
}

func TestLoadGlobalConfig_FromFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	// Create config file
	configDir := filepath.Join(dir, OrgName, AppName)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	configContent := `{
  "editor": "nvim",
  "no_color": true
}`
	configPath := filepath.Join(configDir, "config.json")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	ctx := context.Background()
	cfg, err := LoadGlobalConfig(ctx)
	if err != nil {
		t.Fatalf("LoadGlobalConfig() error = %v", err)
	}

	if cfg.Editor != "nvim" {
		t.Errorf("Editor = %q, want %q", cfg.Editor, "nvim")
	}
	if !cfg.NoColor {
		t.Error("NoColor = false, want true")
	}
}

func TestGlobalConfig_CampaignsDirOmittedWhenEmpty(t *testing.T) {
	legacyConfig := []byte(`{"editor":"nvim","no_color":true}`)

	var cfg GlobalConfig
	if err := json.Unmarshal(legacyConfig, &cfg); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if cfg.CampaignsDir != "" {
		t.Fatalf("CampaignsDir = %q, want empty for legacy config", cfg.CampaignsDir)
	}

	data, err := json.Marshal(&cfg)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if strings.Contains(string(data), "campaigns_dir") {
		t.Fatalf("empty CampaignsDir should be omitted when saved, got JSON: %s", data)
	}
}

func TestLoadGlobalConfig_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	configDir := filepath.Join(dir, OrgName, AppName)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	configPath := filepath.Join(configDir, "config.json")
	if err := os.WriteFile(configPath, []byte("{invalid json"), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	ctx := context.Background()
	_, err := LoadGlobalConfig(ctx)
	if err == nil {
		t.Error("LoadGlobalConfig() expected error for invalid JSON")
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
		Editor:  "code",
		NoColor: false,
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
	want := filepath.Join(dir, OrgName, AppName)
	if got != want {
		t.Errorf("ConfigDir() = %q, want %q", got, want)
	}
}

func TestConfigDir_Default(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")

	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".obey", AppName)

	got := ConfigDir()
	if got != want {
		t.Errorf("ConfigDir() = %q, want %q", got, want)
	}
}

func TestGlobalConfigPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	got := GlobalConfigPath()
	want := filepath.Join(dir, OrgName, AppName, "config.json")
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

// TestGlobalConfig_ResolvedCampaignsDir_DefaultEmpty verifies that an empty
// CampaignsDir expands to $HOME/campaigns.
func TestGlobalConfig_ResolvedCampaignsDir_DefaultEmpty(t *testing.T) {
	fakeHome := "/fake/home"
	t.Setenv("HOME", fakeHome)

	cfg := &GlobalConfig{}
	got, err := cfg.ResolvedCampaignsDir()
	if err != nil {
		t.Fatalf("ResolvedCampaignsDir() unexpected error: %v", err)
	}
	want := filepath.Join(fakeHome, "campaigns")
	if got != want {
		t.Errorf("ResolvedCampaignsDir() = %q, want %q", got, want)
	}
}

// TestGlobalConfig_ResolvedCampaignsDir_TildePrefix verifies that a
// tilde-prefixed path is expanded against $HOME.
func TestGlobalConfig_ResolvedCampaignsDir_TildePrefix(t *testing.T) {
	fakeHome := "/fake/home"
	t.Setenv("HOME", fakeHome)

	cfg := &GlobalConfig{CampaignsDir: "~/foo"}
	got, err := cfg.ResolvedCampaignsDir()
	if err != nil {
		t.Fatalf("ResolvedCampaignsDir() unexpected error: %v", err)
	}
	want := filepath.Join(fakeHome, "foo")
	if got != want {
		t.Errorf("ResolvedCampaignsDir() = %q, want %q", got, want)
	}
}

// TestGlobalConfig_ResolvedCampaignsDir_AbsolutePath verifies that an
// absolute path passes through unchanged (after Clean).
func TestGlobalConfig_ResolvedCampaignsDir_AbsolutePath(t *testing.T) {
	cfg := &GlobalConfig{CampaignsDir: "/tmp/x"}
	got, err := cfg.ResolvedCampaignsDir()
	if err != nil {
		t.Fatalf("ResolvedCampaignsDir() unexpected error: %v", err)
	}
	want := "/tmp/x"
	if got != want {
		t.Errorf("ResolvedCampaignsDir() = %q, want %q", got, want)
	}
}

// TestGlobalConfig_ResolvedCampaignsDir_RelativeResolvesAgainstHome verifies
// that a bare relative path is joined with $HOME.
func TestGlobalConfig_ResolvedCampaignsDir_RelativeResolvesAgainstHome(t *testing.T) {
	fakeHome := "/fake/home"
	t.Setenv("HOME", fakeHome)

	cfg := &GlobalConfig{CampaignsDir: "my-campaigns"}
	got, err := cfg.ResolvedCampaignsDir()
	if err != nil {
		t.Fatalf("ResolvedCampaignsDir() unexpected error: %v", err)
	}
	want := filepath.Join(fakeHome, "my-campaigns")
	if got != want {
		t.Errorf("ResolvedCampaignsDir() = %q, want %q", got, want)
	}
}

// TestGlobalConfig_ResolvedCampaignsDir_InvalidHome verifies that clearing
// HOME causes ResolvedCampaignsDir to return a wrapped error.
func TestGlobalConfig_ResolvedCampaignsDir_InvalidHome(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "") // Windows fallback

	cfg := &GlobalConfig{}
	_, err := cfg.ResolvedCampaignsDir()
	if err == nil {
		t.Fatal("ResolvedCampaignsDir() expected error when HOME is unset, got nil")
	}
	if !strings.Contains(err.Error(), "campaigns_dir") {
		t.Errorf("ResolvedCampaignsDir() error = %q, want it to mention campaigns_dir", err.Error())
	}
}
