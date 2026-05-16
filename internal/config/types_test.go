package config

import (
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestCampaignConfigYAML(t *testing.T) {
	// Test loading campaign.yaml format
	yamlData := `
name: test-campaign
type: product
description: A test campaign
mission: Build amazing things
created_at: 2026-01-14T10:00:00Z
projects:
  - name: project-a
    path: projects/project-a
    url: https://github.com/example/project-a
hooks:
  commit_message:
    command: ob commit
`
	var cfg CampaignConfig
	err := yaml.Unmarshal([]byte(yamlData), &cfg)
	if err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
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
	if cfg.Mission != "Build amazing things" {
		t.Errorf("Mission = %q, want %q", cfg.Mission, "Build amazing things")
	}
	// Paths() returns defaults when Jumps is nil
	if cfg.Paths().Projects != "projects/" {
		t.Errorf("Paths().Projects = %q, want %q", cfg.Paths().Projects, "projects/")
	}
	if len(cfg.Projects) != 1 {
		t.Fatalf("len(Projects) = %d, want 1", len(cfg.Projects))
	}
	if cfg.Projects[0].Name != "project-a" {
		t.Errorf("Projects[0].Name = %q, want %q", cfg.Projects[0].Name, "project-a")
	}
	if cfg.Hooks.CommitMessage.Command != "ob commit" {
		t.Errorf("Hooks.CommitMessage.Command = %q, want %q", cfg.Hooks.CommitMessage.Command, "ob commit")
	}
}

func TestCampaignConfigYAML_MissionOptional(t *testing.T) {
	// Test that mission field is optional (for backwards compatibility)
	yamlData := `
name: legacy-campaign
type: product
description: A legacy campaign without mission
created_at: 2026-01-14T10:00:00Z
`
	var cfg CampaignConfig
	err := yaml.Unmarshal([]byte(yamlData), &cfg)
	if err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	if cfg.Name != "legacy-campaign" {
		t.Errorf("Name = %q, want %q", cfg.Name, "legacy-campaign")
	}
	if cfg.Mission != "" {
		t.Errorf("Mission = %q, want empty string for legacy config", cfg.Mission)
	}
}

func TestGlobalConfigYAML(t *testing.T) {
	yamlData := `
editor: vim
no_color: true
verbose: false
tui:
  theme: dark
`
	var cfg GlobalConfig
	err := yaml.Unmarshal([]byte(yamlData), &cfg)
	if err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	if cfg.Editor != "vim" {
		t.Errorf("Editor = %q, want %q", cfg.Editor, "vim")
	}
	if !cfg.NoColor {
		t.Error("NoColor = false, want true")
	}
	if cfg.TUI.Theme != "dark" {
		t.Errorf("TUI.Theme = %q, want %q", cfg.TUI.Theme, "dark")
	}
}

func TestRegistryYAML(t *testing.T) {
	yamlData := `
campaigns:
  my-campaign:
    path: /home/user/my-campaign
    type: product
    last_access: 2026-01-14T10:00:00Z
  other-campaign:
    path: /home/user/other
    type: research
`
	var reg Registry
	err := yaml.Unmarshal([]byte(yamlData), &reg)
	if err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	if len(reg.Campaigns) != 2 {
		t.Fatalf("len(Campaigns) = %d, want 2", len(reg.Campaigns))
	}

	campaign, ok := reg.Campaigns["my-campaign"]
	if !ok {
		t.Fatal("Campaigns[\"my-campaign\"] not found")
	}
	if campaign.Path != "/home/user/my-campaign" {
		t.Errorf("Path = %q, want %q", campaign.Path, "/home/user/my-campaign")
	}
	if campaign.Type != CampaignTypeProduct {
		t.Errorf("Type = %q, want %q", campaign.Type, CampaignTypeProduct)
	}
}

func TestCampaignTypeValid(t *testing.T) {
	tests := []struct {
		campaignType CampaignType
		want         bool
	}{
		{CampaignTypeProduct, true},
		{CampaignTypeResearch, true},
		{CampaignTypeTools, true},
		{CampaignTypePersonal, true},
		{CampaignType("invalid"), false},
		{CampaignType(""), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.campaignType), func(t *testing.T) {
			if got := tt.campaignType.Valid(); got != tt.want {
				t.Errorf("CampaignType(%q).Valid() = %v, want %v", tt.campaignType, got, tt.want)
			}
		})
	}
}

func TestCampaignTypeString(t *testing.T) {
	tests := []struct {
		campaignType CampaignType
		want         string
	}{
		{CampaignTypeProduct, "product"},
		{CampaignTypeResearch, "research"},
		{CampaignTypeTools, "tools"},
		{CampaignTypePersonal, "personal"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.campaignType.String(); got != tt.want {
				t.Errorf("CampaignType.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDefaultCampaignPaths(t *testing.T) {
	paths := DefaultCampaignPaths()

	if paths.Projects != "projects/" {
		t.Errorf("Projects = %q, want %q", paths.Projects, "projects/")
	}
	if paths.Worktrees != "projects/worktrees/" {
		t.Errorf("Worktrees = %q, want %q", paths.Worktrees, "projects/worktrees/")
	}
	if paths.AIDocs != "ai_docs/" {
		t.Errorf("AIDocs = %q, want %q", paths.AIDocs, "ai_docs/")
	}
	if paths.Docs != "docs/" {
		t.Errorf("Docs = %q, want %q", paths.Docs, "docs/")
	}
	if paths.Festivals != "festivals/" {
		t.Errorf("Festivals = %q, want %q", paths.Festivals, "festivals/")
	}
	if paths.Workflow != "workflow/" {
		t.Errorf("Workflow = %q, want %q", paths.Workflow, "workflow/")
	}
	if paths.Intents != ".campaign/intents/" {
		t.Errorf("Intents = %q, want %q", paths.Intents, ".campaign/intents/")
	}
	if paths.CodeReviews != "workflow/code_reviews/" {
		t.Errorf("CodeReviews = %q, want %q", paths.CodeReviews, "workflow/code_reviews/")
	}
	if paths.Pipelines != "workflow/pipelines/" {
		t.Errorf("Pipelines = %q, want %q", paths.Pipelines, "workflow/pipelines/")
	}
	if paths.Design != "workflow/design/" {
		t.Errorf("Design = %q, want %q", paths.Design, "workflow/design/")
	}
	if paths.Dungeon != "dungeon/" {
		t.Errorf("Dungeon = %q, want %q", paths.Dungeon, "dungeon/")
	}
}

func TestDefaultGlobalConfig(t *testing.T) {
	cfg := DefaultGlobalConfig()

	if cfg.Editor != "" {
		t.Errorf("Editor = %q, want empty string", cfg.Editor)
	}
	if cfg.NoColor {
		t.Error("NoColor = true, want false")
	}
	if cfg.Verbose {
		t.Error("Verbose = true, want false")
	}
	if cfg.TUI.Theme != "adaptive" {
		t.Errorf("TUI.Theme = %q, want %q", cfg.TUI.Theme, "adaptive")
	}
}

func TestDefaultCampaignConfig(t *testing.T) {
	cfg := DefaultCampaignConfig("my-campaign")

	if cfg.Name != "my-campaign" {
		t.Errorf("Name = %q, want %q", cfg.Name, "my-campaign")
	}
	if cfg.Type != CampaignTypeProduct {
		t.Errorf("Type = %q, want %q", cfg.Type, CampaignTypeProduct)
	}
	if cfg.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero, want non-zero")
	}
	// Paths are now accessed via Jumps (set by DefaultCampaignConfig)
	if cfg.Paths().Projects != "projects/" {
		t.Errorf("Paths().Projects = %q, want %q", cfg.Paths().Projects, "projects/")
	}
}

func TestNewRegistry(t *testing.T) {
	reg := NewRegistry()

	if reg == nil {
		t.Fatal("NewRegistry() returned nil")
	}
	if reg.Campaigns == nil {
		t.Error("Campaigns is nil, want initialized map")
	}
	if len(reg.Campaigns) != 0 {
		t.Errorf("len(Campaigns) = %d, want 0", len(reg.Campaigns))
	}
}

func TestCampaignConfigApplyDefaults(t *testing.T) {
	cfg := CampaignConfig{
		Name: "test",
	}
	cfg.ApplyDefaults()

	if cfg.Type != CampaignTypeProduct {
		t.Errorf("Type = %q, want %q", cfg.Type, CampaignTypeProduct)
	}
	if cfg.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero, want non-zero")
	}
	// Note: CampaignConfig.ApplyDefaults no longer sets paths directly
	// Paths are managed via JumpsConfig and loaded separately
	// Paths() method returns defaults when Jumps is nil
	if cfg.Paths().Projects != "projects/" {
		t.Errorf("Paths().Projects = %q, want %q", cfg.Paths().Projects, "projects/")
	}
}

func TestCampaignConfigApplyDefaults_PreservesExisting(t *testing.T) {
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cfg := CampaignConfig{
		Name:      "test",
		Type:      CampaignTypeResearch,
		CreatedAt: created,
	}
	cfg.ApplyDefaults()

	if cfg.Type != CampaignTypeResearch {
		t.Errorf("Type = %q, want %q (should preserve existing)", cfg.Type, CampaignTypeResearch)
	}
	if !cfg.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt = %v, want %v (should preserve existing)", cfg.CreatedAt, created)
	}
	// Paths() returns defaults when Jumps is nil
	if cfg.Paths().Projects != "projects/" {
		t.Errorf("Paths().Projects = %q, want %q (should return default)", cfg.Paths().Projects, "projects/")
	}
}

func TestGlobalConfigApplyDefaults(t *testing.T) {
	cfg := GlobalConfig{}
	cfg.ApplyDefaults()

	if cfg.TUI.Theme != "adaptive" {
		t.Errorf("TUI.Theme = %q, want %q", cfg.TUI.Theme, "adaptive")
	}
}

func TestCampaignConfigMarshalYAML(t *testing.T) {
	// New campaign.yaml format: no paths or shortcuts (those go in jumps.yaml)
	cfg := CampaignConfig{
		Name:        "test-campaign",
		Type:        CampaignTypeProduct,
		Description: "A test campaign",
		CreatedAt:   time.Date(2026, 1, 14, 10, 0, 0, 0, time.UTC),
		Projects: []ProjectConfig{
			{Name: "project-a", Path: "projects/project-a"},
		},
	}

	data, err := yaml.Marshal(&cfg)
	if err != nil {
		t.Fatalf("yaml.Marshal() error = %v", err)
	}

	// Unmarshal back and verify round-trip
	var cfg2 CampaignConfig
	if err := yaml.Unmarshal(data, &cfg2); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	if cfg2.Name != cfg.Name {
		t.Errorf("Round-trip Name = %q, want %q", cfg2.Name, cfg.Name)
	}
	if cfg2.Type != cfg.Type {
		t.Errorf("Round-trip Type = %q, want %q", cfg2.Type, cfg.Type)
	}
	if len(cfg2.Projects) != len(cfg.Projects) {
		t.Errorf("Round-trip len(Projects) = %d, want %d", len(cfg2.Projects), len(cfg.Projects))
	}
}
