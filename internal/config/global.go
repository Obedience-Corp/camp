package config

import (
	"context"
	"encoding/json"
	"os"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
)

// LoadGlobalConfig loads the global configuration from ~/.obey/campaign/config.json.
// Returns default configuration if the file doesn't exist, and auto-creates the file.
func LoadGlobalConfig(ctx context.Context) (*GlobalConfig, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	path := GlobalConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Return defaults and auto-create config file for discoverability
			cfg := DefaultGlobalConfig()
			_ = SaveGlobalConfig(ctx, &cfg) // Ignore error - may lack permissions
			return &cfg, nil
		}
		return nil, camperrors.Wrapf(err, "failed to read global config %s", path)
	}

	var cfg GlobalConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, camperrors.Wrapf(err, "failed to parse global config %s", path)
	}

	// Apply defaults for missing fields
	cfg.ApplyDefaults()

	// Validate
	if err := ValidateGlobalConfig(&cfg); err != nil {
		return nil, camperrors.Wrapf(err, "invalid global config %s", path)
	}

	return &cfg, nil
}

// SaveGlobalConfig saves the global configuration to ~/.obey/campaign/config.json.
func SaveGlobalConfig(ctx context.Context, cfg *GlobalConfig) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Ensure config directory exists
	if err := EnsureConfigDir(); err != nil {
		return camperrors.Wrap(err, "failed to create config directory")
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return camperrors.Wrap(err, "failed to marshal global config")
	}

	path := GlobalConfigPath()

	if err := fsutil.WriteFileAtomically(path, data, 0o644); err != nil {
		return camperrors.Wrap(err, "failed to write global config")
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
