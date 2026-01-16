// Package config provides configuration loading and management for camp.
package config

// Config holds the campaign configuration loaded from campaign.yaml.
type Config struct {
	// Name is the campaign name
	Name string `yaml:"name"`

	// Description is a brief description of the campaign
	Description string `yaml:"description,omitempty"`

	// Projects contains the list of project configurations
	Projects []ProjectConfig `yaml:"projects,omitempty"`
}

// ProjectConfig holds configuration for a single project in the campaign.
type ProjectConfig struct {
	// Name is the project name (directory name)
	Name string `yaml:"name"`

	// Path is the relative path to the project
	Path string `yaml:"path"`

	// URL is the git remote URL (for submodules)
	URL string `yaml:"url,omitempty"`
}

// Load loads configuration from the specified file path.
// If path is empty, it searches for campaign.yaml in the current directory
// and parent directories.
func Load(path string) (*Config, error) {
	// Placeholder - will be implemented in 03_config_loading sequence
	return &Config{}, nil
}

// Save saves the configuration to the specified file path.
func (c *Config) Save(path string) error {
	// Placeholder - will be implemented in 03_config_loading sequence
	return nil
}
