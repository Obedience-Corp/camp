package clone

import (
	"context"
	"os"
	"path/filepath"

	"github.com/Obedience-Corp/camp/internal/config"
)

// registerCampaign attempts to register the cloned campaign in the global registry.
// Registration is non-fatal: errors are captured in the result but do not affect
// clone success. Returns nil if no .campaign/campaign.yaml exists (not a campaign).
func (c *Cloner) registerCampaign(ctx context.Context, dir string) *RegistrationResult {
	if ctx.Err() != nil {
		return &RegistrationResult{Error: ctx.Err()}
	}

	// Check if .campaign/campaign.yaml exists
	configPath := filepath.Join(dir, config.CampaignDir, config.CampaignConfigFile)
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil
	}

	// Load campaign config
	cfg, err := config.LoadCampaignConfig(ctx, dir)
	if err != nil {
		return &RegistrationResult{Error: err}
	}

	// Load global registry
	reg, err := config.LoadRegistry(ctx)
	if err != nil {
		return &RegistrationResult{Error: err}
	}

	// Register the campaign
	if err := reg.Register(cfg.ID, cfg.Name, dir, cfg.Type); err != nil {
		return &RegistrationResult{Error: err}
	}

	// Save registry (non-fatal on failure)
	if err := config.SaveRegistry(ctx, reg); err != nil {
		return &RegistrationResult{
			Registered:   true,
			CampaignID:   cfg.ID,
			CampaignName: cfg.Name,
			Error:        err,
		}
	}

	return &RegistrationResult{
		Registered:   true,
		CampaignID:   cfg.ID,
		CampaignName: cfg.Name,
	}
}
