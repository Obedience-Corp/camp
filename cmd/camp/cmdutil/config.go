package cmdutil

import (
	"context"

	"github.com/Obedience-Corp/camp/internal/config"
)

// LoadCampaignConfigSafe loads campaign config without adding command-specific behavior.
func LoadCampaignConfigSafe(ctx context.Context) (*config.CampaignConfig, string, error) {
	cfg, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return nil, "", err
	}
	return cfg, root, nil
}
