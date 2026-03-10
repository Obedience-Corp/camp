package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFreshConfig(t *testing.T) {
	tmpDir := t.TempDir()
	settingsDir := filepath.Join(tmpDir, CampaignDir, SettingsDir)
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatalf("failed to create settings dir: %v", err)
	}

	configPath := filepath.Join(settingsDir, FreshConfigFile)
	content := `branch: develop
push_upstream: false
prune: true
prune_remote: false
projects:
  camp:
    branch: feat/camp
    push_upstream: true
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write fresh config: %v", err)
	}

	cfg, err := LoadFreshConfig(context.Background(), tmpDir)
	if err != nil {
		t.Fatalf("LoadFreshConfig() error = %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadFreshConfig() returned nil")
	}

	if got := cfg.ResolveFreshBranch("", false, "camp"); got != "feat/camp" {
		t.Fatalf("ResolveFreshBranch(project override) = %q, want %q", got, "feat/camp")
	}
	if got := cfg.ResolveFreshBranch("", false, "fest"); got != "develop" {
		t.Fatalf("ResolveFreshBranch(global default) = %q, want %q", got, "develop")
	}
	if got := cfg.ResolveFreshBranch("feat/override", false, "camp"); got != "feat/override" {
		t.Fatalf("ResolveFreshBranch(flag override) = %q, want %q", got, "feat/override")
	}
	if got := cfg.ResolveFreshBranch("", true, "camp"); got != "" {
		t.Fatalf("ResolveFreshBranch(noBranch) = %q, want empty", got)
	}
	if !cfg.ResolveFreshPushUpstream("camp") {
		t.Fatal("ResolveFreshPushUpstream(project override) = false, want true")
	}
	if cfg.ResolveFreshPushUpstream("fest") {
		t.Fatal("ResolveFreshPushUpstream(global default) = true, want false")
	}
	if !cfg.ResolveFreshPrune() {
		t.Fatal("ResolveFreshPrune() = false, want true")
	}
	if cfg.ResolveFreshPruneRemote() {
		t.Fatal("ResolveFreshPruneRemote() = true, want false")
	}
}

func TestLoadFreshConfig_NotExists(t *testing.T) {
	cfg, err := LoadFreshConfig(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("LoadFreshConfig() error = %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadFreshConfig() returned nil")
	}

	if got := cfg.ResolveFreshBranch("", false, "camp"); got != "" {
		t.Fatalf("ResolveFreshBranch() = %q, want empty", got)
	}
	if !cfg.ResolveFreshPushUpstream("camp") {
		t.Fatal("ResolveFreshPushUpstream() = false, want true default")
	}
	if !cfg.ResolveFreshPrune() {
		t.Fatal("ResolveFreshPrune() = false, want true default")
	}
	if !cfg.ResolveFreshPruneRemote() {
		t.Fatal("ResolveFreshPruneRemote() = false, want true default")
	}
}

func TestLoadFreshConfig_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	settingsDir := filepath.Join(tmpDir, CampaignDir, SettingsDir)
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatalf("failed to create settings dir: %v", err)
	}

	configPath := filepath.Join(settingsDir, FreshConfigFile)
	if err := os.WriteFile(configPath, []byte("branch: ["), 0o644); err != nil {
		t.Fatalf("failed to write fresh config: %v", err)
	}

	if _, err := LoadFreshConfig(context.Background(), tmpDir); err == nil {
		t.Fatal("LoadFreshConfig() expected error for invalid YAML")
	}
}

func TestLoadFreshConfig_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := LoadFreshConfig(ctx, t.TempDir()); err == nil {
		t.Fatal("LoadFreshConfig() expected context error")
	}
}
