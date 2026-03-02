package config

import (
	"context"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// SettingsDir is the name of the settings subdirectory within .campaign/.
const SettingsDir = "settings"

// JumpsConfigFile is the name of the jumps configuration file.
const JumpsConfigFile = "jumps.yaml"

// PinsConfigFile is the name of the pins configuration file.
const PinsConfigFile = "pins.json"

// JumpsConfig represents .campaign/settings/jumps.yaml configuration.
// It contains navigation paths and shortcuts for quick campaign navigation.
type JumpsConfig struct {
	// Paths defines the campaign directory structure.
	Paths CampaignPaths `yaml:"paths,omitempty"`
	// Shortcuts defines custom navigation and command shortcuts.
	Shortcuts map[string]ShortcutConfig `yaml:"shortcuts,omitempty"`
}

// JumpsConfigPath returns the path to jumps.yaml for a given campaign root.
func JumpsConfigPath(campaignRoot string) string {
	return filepath.Join(campaignRoot, CampaignDir, SettingsDir, JumpsConfigFile)
}

// PinsConfigPath returns the path to pins.json for a given campaign root.
func PinsConfigPath(campaignRoot string) string {
	return filepath.Join(campaignRoot, CampaignDir, SettingsDir, PinsConfigFile)
}

// SettingsDirPath returns the path to the settings directory for a given campaign root.
func SettingsDirPath(campaignRoot string) string {
	return filepath.Join(campaignRoot, CampaignDir, SettingsDir)
}

// LoadJumpsConfig loads .campaign/settings/jumps.yaml from the campaign root.
// Returns nil if the file doesn't exist (caller should use defaults).
func LoadJumpsConfig(ctx context.Context, campaignRoot string) (*JumpsConfig, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	configPath := JumpsConfigPath(campaignRoot)
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // File doesn't exist, caller should use defaults
		}
		return nil, camperrors.Wrapf(err, "failed to read jumps config %s", configPath)
	}

	var cfg JumpsConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, camperrors.Wrapf(err, "failed to parse jumps config %s", configPath)
	}

	return &cfg, nil
}

// SaveJumpsConfig saves the jumps configuration to .campaign/settings/jumps.yaml.
func SaveJumpsConfig(ctx context.Context, campaignRoot string, cfg *JumpsConfig) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	settingsDir := SettingsDirPath(campaignRoot)
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		return camperrors.Wrap(err, "failed to create settings directory")
	}

	configPath := JumpsConfigPath(campaignRoot)
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return camperrors.Wrap(err, "failed to marshal jumps config")
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return camperrors.Wrap(err, "failed to write jumps config")
	}

	return nil
}

// DefaultJumpsConfig returns the default jumps configuration.
func DefaultJumpsConfig() JumpsConfig {
	return JumpsConfig{
		Paths:     DefaultCampaignPaths(),
		Shortcuts: DefaultNavigationShortcuts(),
	}
}

// ApplyDefaults fills in missing fields with default values.
func (j *JumpsConfig) ApplyDefaults() {
	defaults := DefaultJumpsConfig()

	// Apply path defaults
	if j.Paths.Projects == "" {
		j.Paths.Projects = defaults.Paths.Projects
	}
	if j.Paths.Worktrees == "" {
		j.Paths.Worktrees = defaults.Paths.Worktrees
	}
	if j.Paths.AIDocs == "" {
		j.Paths.AIDocs = defaults.Paths.AIDocs
	}
	if j.Paths.Docs == "" {
		j.Paths.Docs = defaults.Paths.Docs
	}
	if j.Paths.Festivals == "" {
		j.Paths.Festivals = defaults.Paths.Festivals
	}
	if j.Paths.Workflow == "" {
		j.Paths.Workflow = defaults.Paths.Workflow
	}
	if j.Paths.Intents == "" {
		j.Paths.Intents = defaults.Paths.Intents
	}
	if j.Paths.CodeReviews == "" {
		j.Paths.CodeReviews = defaults.Paths.CodeReviews
	}
	if j.Paths.Pipelines == "" {
		j.Paths.Pipelines = defaults.Paths.Pipelines
	}
	if j.Paths.Design == "" {
		j.Paths.Design = defaults.Paths.Design
	}
	if j.Paths.Dungeon == "" {
		j.Paths.Dungeon = defaults.Paths.Dungeon
	}
}

// JumpsConfigExists checks if jumps.yaml exists for the given campaign root.
func JumpsConfigExists(campaignRoot string) bool {
	configPath := JumpsConfigPath(campaignRoot)
	_, err := os.Stat(configPath)
	return err == nil
}
