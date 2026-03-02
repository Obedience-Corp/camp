package config

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// AllowlistConfigFile is the name of the allowlist configuration file.
const AllowlistConfigFile = "allowlist.json"

// AllowlistVersion is the current allowlist format version.
const AllowlistVersion = 1

// AllowlistConfig represents .campaign/settings/allowlist.json configuration.
// It defines which commands are allowed to execute through the daemon for this campaign.
type AllowlistConfig struct {
	// Version is the allowlist format version.
	Version int `json:"version"`
	// Commands maps command names to their configuration.
	Commands map[string]CommandConfig `json:"commands"`
	// InheritDefaults controls whether to merge with daemon defaults.
	// If true, campaign allowlist extends defaults; if false, only campaign commands are allowed.
	InheritDefaults bool `json:"inherit_defaults"`
}

// CommandConfig holds configuration for a single command.
type CommandConfig struct {
	// Allowed indicates whether the command is permitted.
	Allowed bool `json:"allowed"`
	// Description provides human-readable documentation for the command.
	Description string `json:"description,omitempty"`
}

// AllowlistConfigPath returns the path to allowlist.json for a given campaign root.
func AllowlistConfigPath(campaignRoot string) string {
	return filepath.Join(campaignRoot, CampaignDir, SettingsDir, AllowlistConfigFile)
}

// LoadAllowlistConfig loads .campaign/settings/allowlist.json from the campaign root.
// Returns nil if the file doesn't exist (caller should use defaults).
func LoadAllowlistConfig(ctx context.Context, campaignRoot string) (*AllowlistConfig, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	configPath := AllowlistConfigPath(campaignRoot)
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // File doesn't exist, caller should use defaults
		}
		return nil, camperrors.Wrapf(err, "failed to read allowlist config %s", configPath)
	}

	var cfg AllowlistConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, camperrors.Wrapf(err, "failed to parse allowlist config %s", configPath)
	}

	return &cfg, nil
}

// SaveAllowlistConfig saves the allowlist configuration to .campaign/settings/allowlist.json.
func SaveAllowlistConfig(ctx context.Context, campaignRoot string, cfg *AllowlistConfig) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	settingsDir := SettingsDirPath(campaignRoot)
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		return camperrors.Wrap(err, "failed to create settings directory")
	}

	configPath := AllowlistConfigPath(campaignRoot)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return camperrors.Wrap(err, "failed to marshal allowlist config")
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return camperrors.Wrap(err, "failed to write allowlist config")
	}

	return nil
}

// DefaultAllowlistConfig returns the default allowlist configuration.
// This matches the daemon's DefaultCommandAllowlist.
func DefaultAllowlistConfig() *AllowlistConfig {
	return &AllowlistConfig{
		Version: AllowlistVersion,
		Commands: map[string]CommandConfig{
			"fest": {Allowed: true, Description: "Festival CLI"},
			"camp": {Allowed: true, Description: "Campaign CLI"},
			"just": {Allowed: true, Description: "Task runner"},
			"git":  {Allowed: true, Description: "Version control"},
		},
		InheritDefaults: true,
	}
}

// IsAllowed checks if a command is allowed by this configuration.
// Returns the allowed status and whether the command was found in the config.
func (a *AllowlistConfig) IsAllowed(cmd string) (allowed bool, found bool) {
	if a == nil || a.Commands == nil {
		return false, false
	}
	entry, found := a.Commands[cmd]
	if !found {
		return false, false
	}
	return entry.Allowed, true
}

// AllowlistConfigExists checks if allowlist.json exists for the given campaign root.
func AllowlistConfigExists(campaignRoot string) bool {
	configPath := AllowlistConfigPath(campaignRoot)
	_, err := os.Stat(configPath)
	return err == nil
}
