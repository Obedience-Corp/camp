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

// FreshConfigFile is the name of the fresh configuration file.
const FreshConfigFile = "fresh.yaml"

// FreshConfig represents .campaign/settings/fresh.yaml configuration.
// It defines the post-merge branch cycling behavior for camp fresh.
type FreshConfig struct {
	// Branch to create after syncing. Empty means no branch creation.
	Branch string `yaml:"branch,omitempty"`
	// PushUpstream controls whether to push with --set-upstream after branch creation.
	PushUpstream *bool `yaml:"push_upstream,omitempty"`
	// Prune controls whether to prune merged branches.
	Prune *bool `yaml:"prune,omitempty"`
	// PruneRemote controls whether to prune stale remote tracking refs.
	PruneRemote *bool `yaml:"prune_remote,omitempty"`
	// Projects holds per-project overrides keyed by project name.
	Projects map[string]FreshProjectConfig `yaml:"projects,omitempty"`
}

// FreshProjectConfig holds per-project overrides for fresh behavior.
// Pointer types distinguish "not set" from "set to false/empty".
type FreshProjectConfig struct {
	Branch       *string `yaml:"branch,omitempty"`
	PushUpstream *bool   `yaml:"push_upstream,omitempty"`
}

// FreshConfigPath returns the path to fresh.yaml for a given campaign root.
func FreshConfigPath(campaignRoot string) string {
	return filepath.Join(campaignRoot, CampaignDir, SettingsDir, FreshConfigFile)
}

// LoadFreshConfig loads .campaign/settings/fresh.yaml from the campaign root.
// Returns an empty config with defaults if the file doesn't exist.
func LoadFreshConfig(ctx context.Context, campaignRoot string) (*FreshConfig, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	configPath := FreshConfigPath(campaignRoot)
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &FreshConfig{}, nil
		}
		return nil, camperrors.Wrapf(err, "failed to read fresh config %s", configPath)
	}

	var cfg FreshConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, camperrors.Wrapf(err, "failed to parse fresh config %s", configPath)
	}

	return &cfg, nil
}

// ResolveFreshBranch resolves the branch name using the priority chain:
// flag > project override > global config > default ("").
func (c *FreshConfig) ResolveFreshBranch(flagBranch string, noBranch bool, projectName string) string {
	if noBranch {
		return ""
	}
	if flagBranch != "" {
		return flagBranch
	}
	if pc, ok := c.Projects[projectName]; ok && pc.Branch != nil {
		return *pc.Branch
	}
	return c.Branch
}

// ResolveFreshPushUpstream resolves push_upstream using the priority chain:
// project override > global config > default (true).
func (c *FreshConfig) ResolveFreshPushUpstream(projectName string) bool {
	if pc, ok := c.Projects[projectName]; ok && pc.PushUpstream != nil {
		return *pc.PushUpstream
	}
	if c.PushUpstream != nil {
		return *c.PushUpstream
	}
	return true
}

// ResolveFreshPrune resolves prune using the global config or default (true).
func (c *FreshConfig) ResolveFreshPrune() bool {
	if c.Prune != nil {
		return *c.Prune
	}
	return true
}

// ResolveFreshPruneRemote resolves prune_remote using the global config or default (true).
func (c *FreshConfig) ResolveFreshPruneRemote() bool {
	if c.PruneRemote != nil {
		return *c.PruneRemote
	}
	return true
}
