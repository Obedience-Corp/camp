package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAllowlistConfig(t *testing.T) {
	tmpDir := t.TempDir()

	// Create campaign settings directory
	settingsDir := filepath.Join(tmpDir, CampaignDir, SettingsDir)
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatalf("failed to create settings dir: %v", err)
	}

	// Write test allowlist
	allowlistPath := filepath.Join(settingsDir, AllowlistConfigFile)
	content := `{
		"version": 1,
		"commands": {
			"git": { "allowed": true, "description": "Version control" },
			"make": { "allowed": false, "description": "Build tool - disabled" }
		},
		"inherit_defaults": true
	}`
	if err := os.WriteFile(allowlistPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write allowlist: %v", err)
	}

	// Load and verify
	ctx := context.Background()
	cfg, err := LoadAllowlistConfig(ctx, tmpDir)
	if err != nil {
		t.Fatalf("LoadAllowlistConfig() error = %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadAllowlistConfig() returned nil")
	}

	if cfg.Version != 1 {
		t.Errorf("Version = %d, want 1", cfg.Version)
	}
	if !cfg.InheritDefaults {
		t.Error("InheritDefaults should be true")
	}
	if len(cfg.Commands) != 2 {
		t.Errorf("Commands length = %d, want 2", len(cfg.Commands))
	}

	// Check git entry
	if git, ok := cfg.Commands["git"]; !ok {
		t.Error("git command not found")
	} else {
		if !git.Allowed {
			t.Error("git should be allowed")
		}
		if git.Description != "Version control" {
			t.Errorf("git description = %q, want %q", git.Description, "Version control")
		}
	}

	// Check make entry
	if make, ok := cfg.Commands["make"]; !ok {
		t.Error("make command not found")
	} else {
		if make.Allowed {
			t.Error("make should not be allowed")
		}
	}
}

func TestLoadAllowlistConfig_NotExists(t *testing.T) {
	tmpDir := t.TempDir()

	ctx := context.Background()
	cfg, err := LoadAllowlistConfig(ctx, tmpDir)
	if err != nil {
		t.Fatalf("LoadAllowlistConfig() error = %v, want nil", err)
	}
	if cfg != nil {
		t.Error("LoadAllowlistConfig() should return nil when file doesn't exist")
	}
}

func TestLoadAllowlistConfig_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()

	// Create settings directory
	settingsDir := filepath.Join(tmpDir, CampaignDir, SettingsDir)
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatalf("failed to create settings dir: %v", err)
	}

	// Write invalid JSON
	allowlistPath := filepath.Join(settingsDir, AllowlistConfigFile)
	if err := os.WriteFile(allowlistPath, []byte("{invalid json}"), 0644); err != nil {
		t.Fatalf("failed to write allowlist: %v", err)
	}

	ctx := context.Background()
	_, err := LoadAllowlistConfig(ctx, tmpDir)
	if err == nil {
		t.Error("LoadAllowlistConfig() should return error for invalid JSON")
	}
}

func TestLoadAllowlistConfig_ContextCanceled(t *testing.T) {
	tmpDir := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := LoadAllowlistConfig(ctx, tmpDir)
	if err == nil {
		t.Error("LoadAllowlistConfig() should return error when context is canceled")
	}
}

func TestSaveAllowlistConfig(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &AllowlistConfig{
		Version: 1,
		Commands: map[string]CommandConfig{
			"test": {Allowed: true, Description: "Test command"},
		},
		InheritDefaults: false,
	}

	ctx := context.Background()
	if err := SaveAllowlistConfig(ctx, tmpDir, cfg); err != nil {
		t.Fatalf("SaveAllowlistConfig() error = %v", err)
	}

	// Verify file exists
	allowlistPath := AllowlistConfigPath(tmpDir)
	if _, err := os.Stat(allowlistPath); os.IsNotExist(err) {
		t.Error("allowlist file was not created")
	}

	// Load and verify roundtrip
	loaded, err := LoadAllowlistConfig(ctx, tmpDir)
	if err != nil {
		t.Fatalf("LoadAllowlistConfig() error = %v", err)
	}
	if loaded.Version != cfg.Version {
		t.Errorf("Version = %d, want %d", loaded.Version, cfg.Version)
	}
	if loaded.InheritDefaults != cfg.InheritDefaults {
		t.Errorf("InheritDefaults = %v, want %v", loaded.InheritDefaults, cfg.InheritDefaults)
	}
	if len(loaded.Commands) != len(cfg.Commands) {
		t.Errorf("Commands length = %d, want %d", len(loaded.Commands), len(cfg.Commands))
	}
}

func TestDefaultAllowlistConfig(t *testing.T) {
	cfg := DefaultAllowlistConfig()

	if cfg == nil {
		t.Fatal("DefaultAllowlistConfig() returned nil")
	}
	if cfg.Version != AllowlistVersion {
		t.Errorf("Version = %d, want %d", cfg.Version, AllowlistVersion)
	}
	if !cfg.InheritDefaults {
		t.Error("InheritDefaults should be true by default")
	}

	// Check expected default commands
	expectedCmds := []string{"fest", "camp", "just", "git"}
	for _, cmd := range expectedCmds {
		if entry, ok := cfg.Commands[cmd]; !ok {
			t.Errorf("expected command %q not found in defaults", cmd)
		} else if !entry.Allowed {
			t.Errorf("command %q should be allowed by default", cmd)
		}
	}
}

func TestAllowlistConfig_IsAllowed(t *testing.T) {
	cfg := &AllowlistConfig{
		Commands: map[string]CommandConfig{
			"allowed":     {Allowed: true},
			"not-allowed": {Allowed: false},
		},
	}

	tests := []struct {
		cmd       string
		wantAllow bool
		wantFound bool
	}{
		{"allowed", true, true},
		{"not-allowed", false, true},
		{"unknown", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			allowed, found := cfg.IsAllowed(tt.cmd)
			if allowed != tt.wantAllow {
				t.Errorf("IsAllowed(%q) allowed = %v, want %v", tt.cmd, allowed, tt.wantAllow)
			}
			if found != tt.wantFound {
				t.Errorf("IsAllowed(%q) found = %v, want %v", tt.cmd, found, tt.wantFound)
			}
		})
	}
}

func TestAllowlistConfig_IsAllowed_Nil(t *testing.T) {
	var cfg *AllowlistConfig
	allowed, found := cfg.IsAllowed("anything")
	if allowed || found {
		t.Error("nil config should return false, false")
	}
}

func TestAllowlistConfigExists(t *testing.T) {
	tmpDir := t.TempDir()

	// Should not exist initially
	if AllowlistConfigExists(tmpDir) {
		t.Error("AllowlistConfigExists() should return false when file doesn't exist")
	}

	// Create the file
	settingsDir := filepath.Join(tmpDir, CampaignDir, SettingsDir)
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatalf("failed to create settings dir: %v", err)
	}
	allowlistPath := filepath.Join(settingsDir, AllowlistConfigFile)
	if err := os.WriteFile(allowlistPath, []byte("{}"), 0644); err != nil {
		t.Fatalf("failed to create allowlist file: %v", err)
	}

	// Should exist now
	if !AllowlistConfigExists(tmpDir) {
		t.Error("AllowlistConfigExists() should return true when file exists")
	}
}

func TestAllowlistConfigPath(t *testing.T) {
	path := AllowlistConfigPath("/some/campaign")
	expected := "/some/campaign/.campaign/settings/allowlist.json"
	if path != expected {
		t.Errorf("AllowlistConfigPath() = %q, want %q", path, expected)
	}
}
