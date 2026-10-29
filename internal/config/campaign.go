package config

import (
	"context"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	"github.com/Obedience-Corp/camp/internal/campaign"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
)

// CampaignConfigFile is the name of the campaign configuration file.
const CampaignConfigFile = "campaign.yaml"

// CampaignDir is the name of the campaign marker directory.
const CampaignDir = ".campaign"

// LoadCampaignConfig loads .campaign/campaign.yaml from the campaign root.
// Returns the configuration with defaults applied and validated.
// Also loads .campaign/settings/jumps.yaml for navigation configuration.
func LoadCampaignConfig(ctx context.Context, campaignRoot string) (*CampaignConfig, error) {
	cfg, err := readCampaignFile(ctx, campaignRoot)
	if err != nil {
		return nil, err
	}

	// Generate ID for campaigns that don't have one
	if cfg.ID == "" {
		cfg.ID = uuid.New().String()
		// Best-effort persistence: keep loading even if saving the generated ID fails.
		_ = SaveCampaignConfig(ctx, campaignRoot, cfg)
	}

	// Load jumps.yaml for navigation configuration
	if err := loadJumps(ctx, campaignRoot, cfg, true); err != nil {
		return nil, err
	}

	if err := validateCampaignFile(campaignRoot, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// readCampaignFile reads campaign.yaml and applies defaults. It does not write.
func readCampaignFile(ctx context.Context, campaignRoot string) (*CampaignConfig, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	configPath := CampaignConfigPath(campaignRoot)
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, camperrors.NewNotFound("camp config", configPath, nil)
		}
		return nil, camperrors.Wrapf(err, "failed to read camp config %s", configPath)
	}

	var cfg CampaignConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, camperrors.Wrapf(err, "failed to parse camp config %s", configPath)
	}

	// Apply defaults for missing optional fields
	cfg.ApplyDefaults()
	return &cfg, nil
}

func validateCampaignFile(campaignRoot string, cfg *CampaignConfig) error {
	if err := ValidateCampaignConfig(cfg); err != nil {
		return camperrors.Wrapf(err, "invalid camp config %s", CampaignConfigPath(campaignRoot))
	}
	return nil
}

// loadJumps loads jumps.yaml. When persist is set and the file is missing,
// the defaults are written; otherwise they are kept in memory.
func loadJumps(ctx context.Context, campaignRoot string, cfg *CampaignConfig, persist bool) error {
	// Try to load existing jumps.yaml
	jumps, err := LoadJumpsConfig(ctx, campaignRoot)
	if err != nil {
		return err
	}

	if jumps != nil {
		// jumps.yaml exists, use it
		cfg.Jumps = jumps
		return nil
	}

	defaultJumps := DefaultJumpsConfig()
	if !persist {
		cfg.Jumps = &defaultJumps
		return nil
	}

	// No jumps.yaml exists - create defaults
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

	root, err := campaign.DetectFromCwd(ctx)
	if err != nil {
		return nil, "", err
	}

	cfg, err := LoadCampaignConfig(ctx, root)
	if err != nil {
		return nil, "", err
	}

	return cfg, root, nil
}

// ReadCampaignConfig reads campaign.yaml and jumps.yaml without writing.
// A missing campaign ID stays empty, and a missing jumps.yaml is not created.
func ReadCampaignConfig(ctx context.Context, campaignRoot string) (*CampaignConfig, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	cfg, err := readCampaignFile(ctx, campaignRoot)
	if err != nil {
		return nil, err
	}
	if err := loadJumps(ctx, campaignRoot, cfg, false); err != nil {
		return nil, err
	}
	if err := validateCampaignFile(campaignRoot, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// ReadCampaignConfigFromCwd detects the camp root and reads campaign.yaml.
// A missing campaign ID stays empty, and a missing jumps.yaml is not created.
func ReadCampaignConfigFromCwd(ctx context.Context) (*CampaignConfig, string, error) {
	if ctx.Err() != nil {
		return nil, "", ctx.Err()
	}

	root, err := campaign.DetectFromCwd(ctx)
	if err != nil {
		return nil, "", err
	}

	cfg, err := ReadCampaignConfig(ctx, root)
	if err != nil {
		return nil, "", err
	}
	return cfg, root, nil
}

// findCampaignRoot is kept as a thin wrapper for tests and older callers.
func findCampaignRoot(ctx context.Context, startDir string) (string, error) {
	return campaign.Detect(ctx, startDir)
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
		return camperrors.Wrap(err, "failed to create camp directory")
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return camperrors.Wrap(err, "failed to marshal camp config")
	}

	if (cfg.Hooks == HooksConfig{}) {
		data = append(data, commentedHooksPlaceholder()...)
	}

	if err := fsutil.WriteFileAtomically(configPath, data, 0o644); err != nil {
		return camperrors.Wrap(err, "failed to write camp config")
	}

	return nil
}

// commentedHooksPlaceholder keeps first-use setup discoverable without a manual in YAML.
func commentedHooksPlaceholder() []byte {
	return []byte(`
# Automatic commit messages: camp settings commit-writer
# Runs in the target repo; stdout becomes the message.
# hooks:
#   commit_message:
#     command: ob commit --print-session-id
#     timeout: 12m
#     retry_window: 1h
`)
}
