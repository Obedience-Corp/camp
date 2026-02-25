// Package config provides configuration loading and management for camp.
//
// Camp uses a two-level configuration system:
//   - Campaign config: .campaign/campaign.yaml in the campaign root
//   - Global config: ~/.obey/campaign/config.yaml for user preferences
//
// Additionally, a registry at ~/.obey/campaign/registry.yaml tracks
// all known campaigns for quick navigation.
package config

// Config is an alias for CampaignConfig for backward compatibility.
// Deprecated: Use CampaignConfig directly.
type Config = CampaignConfig

// Load loads configuration from the specified file path.
// If path is empty, it searches for campaign.yaml in the current directory
// and parent directories.
func Load(path string) (*CampaignConfig, error) {
	// Placeholder - will be implemented in 02_campaign_config task
	return &CampaignConfig{}, nil
}

// Save saves the configuration to the specified file path.
func (c *CampaignConfig) Save(path string) error {
	// Placeholder - will be implemented in 02_campaign_config task
	return nil
}
