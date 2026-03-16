package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// CampaignConfigFile is the name of the campaign configuration file.
const CampaignConfigFile = "campaign.yaml"

// CampaignDir is the name of the campaign marker directory.
const CampaignDir = ".campaign"

// LoadCampaignConfig loads .campaign/campaign.yaml from the campaign root.
// Returns the configuration with defaults applied and validated.
// Also loads .campaign/settings/jumps.yaml for navigation configuration.
func LoadCampaignConfig(ctx context.Context, campaignRoot string) (*CampaignConfig, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	configPath := CampaignConfigPath(campaignRoot)
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("campaign config not found: %s", configPath)
		}
		return nil, camperrors.Wrapf(err, "failed to read campaign config %s", configPath)
	}

	var cfg CampaignConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, camperrors.Wrapf(err, "failed to parse campaign config %s", configPath)
	}

	// Apply defaults for missing optional fields
	cfg.ApplyDefaults()

	// Generate ID for campaigns that don't have one
	if cfg.ID == "" {
		cfg.ID = uuid.New().String()
		// Best-effort persistence: keep loading even if saving the generated ID fails.
		_ = SaveCampaignConfig(ctx, campaignRoot, &cfg)
	}

	// Load jumps.yaml for navigation configuration
	if err := loadJumps(ctx, campaignRoot, &cfg); err != nil {
		return nil, err
	}

	// Validate required fields
	if err := ValidateCampaignConfig(&cfg); err != nil {
		return nil, camperrors.Wrapf(err, "invalid campaign config %s", configPath)
	}

	return &cfg, nil
}

// loadJumps loads jumps.yaml or creates defaults if it doesn't exist.
func loadJumps(ctx context.Context, campaignRoot string, cfg *CampaignConfig) error {
	// Try to load existing jumps.yaml
	jumps, err := LoadJumpsConfig(ctx, campaignRoot)
	if err != nil {
		return err
	}

	if jumps != nil {
		// jumps.yaml exists, use it
		jumps.ApplyDefaults()
		cfg.Jumps = jumps
		return nil
	}

	// No jumps.yaml exists - create defaults
	defaultJumps := DefaultJumpsConfig()
	if err := SaveJumpsConfig(ctx, campaignRoot, &defaultJumps); err != nil {
		// Don't fail if we can't save defaults, just use them in memory
		cfg.Jumps = &defaultJumps
		return nil
	}

	cfg.Jumps = &defaultJumps
	return nil
}

// LoadCampaignConfigFromCwd loads campaign config by first detecting the campaign root
// from the current working directory.
func LoadCampaignConfigFromCwd(ctx context.Context) (*CampaignConfig, string, error) {
	if ctx.Err() != nil {
		return nil, "", ctx.Err()
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil, "", camperrors.Wrap(err, "failed to get working directory")
	}

	// Find campaign root by walking up
	root, err := findCampaignRoot(ctx, cwd)
	if err != nil {
		return nil, "", err
	}

	cfg, err := LoadCampaignConfig(ctx, root)
	if err != nil {
		return nil, "", err
	}

	return cfg, root, nil
}

// findCampaignRoot walks up from startDir looking for .campaign/ directory.
func findCampaignRoot(ctx context.Context, startDir string) (string, error) {
	dir := startDir

	// Resolve symlinks
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", camperrors.Wrap(err, "failed to resolve path")
	}

	dir, err = filepath.Abs(dir)
	if err != nil {
		return "", camperrors.Wrap(err, "failed to get absolute path")
	}

	for {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}

		campaignPath := filepath.Join(dir, CampaignDir)
		info, err := os.Stat(campaignPath)
		if err == nil && info.IsDir() {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("not inside a campaign directory\n" +
				"Hint: Run 'camp init' to create a campaign, or navigate to an existing one")
		}
		dir = parent
	}
}

// CampaignConfigPath returns the path to campaign.yaml for a given root.
func CampaignConfigPath(root string) string {
	return filepath.Join(root, CampaignDir, CampaignConfigFile)
}

// SaveCampaignConfig saves the campaign configuration to the campaign root.
func SaveCampaignConfig(ctx context.Context, campaignRoot string, cfg *CampaignConfig) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	configPath := CampaignConfigPath(campaignRoot)

	// Ensure the .campaign directory exists
	campaignDir := filepath.Join(campaignRoot, CampaignDir)
	if err := os.MkdirAll(campaignDir, 0755); err != nil {
		return camperrors.Wrap(err, "failed to create campaign directory")
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return camperrors.Wrap(err, "failed to marshal campaign config")
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return camperrors.Wrap(err, "failed to write campaign config")
	}

	return nil
}
