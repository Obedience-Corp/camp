package config

import (
	"context"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// LoadGlobalConfig loads the global configuration from ~/.config/campaign/config.yaml.
// Returns default configuration if the file doesn't exist.
func LoadGlobalConfig(ctx context.Context) (*GlobalConfig, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	path := GlobalConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Return defaults if no config file
			cfg := DefaultGlobalConfig()
			return &cfg, nil
		}
		return nil, fmt.Errorf("failed to read global config %s: %w", path, err)
	}

	var cfg GlobalConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse global config %s: %w", path, err)
	}

	// Apply defaults for missing fields
	cfg.ApplyDefaults()

	// Validate
	if err := ValidateGlobalConfig(&cfg); err != nil {
		return nil, fmt.Errorf("invalid global config %s: %w", path, err)
	}

	return &cfg, nil
}

// SaveGlobalConfig saves the global configuration to ~/.config/campaign/config.yaml.
func SaveGlobalConfig(ctx context.Context, cfg *GlobalConfig) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Ensure config directory exists
	if err := EnsureConfigDir(); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal global config: %w", err)
	}

	path := GlobalConfigPath()

	// Atomic write via temp file
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("failed to write global config: %w", err)
	}

	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp) // Clean up temp file on rename failure
		return fmt.Errorf("failed to save global config: %w", err)
	}

	return nil
}

// InitGlobalConfig creates a default global configuration file if it doesn't exist.
func InitGlobalConfig(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	path := GlobalConfigPath()
	if _, err := os.Stat(path); err == nil {
		// Config already exists
		return nil
	}

	cfg := DefaultGlobalConfig()
	return SaveGlobalConfig(ctx, &cfg)
}
