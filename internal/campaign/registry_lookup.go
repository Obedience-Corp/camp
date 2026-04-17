package campaign

import (
	"context"
	"path/filepath"

	"github.com/Obedience-Corp/camp/internal/config/registryfile"
)

func lookupRegisteredCampaignRoot(ctx context.Context, campaignID string) (string, bool, error) {
	if campaignID == "" {
		return "", false, nil
	}

	if ctx.Err() != nil {
		return "", false, ctx.Err()
	}

	reg, err := registryfile.Load()
	if err != nil {
		return "", false, err
	}

	entry, ok := reg.Campaigns[campaignID]
	if !ok || entry.Path == "" {
		return "", false, nil
	}

	root, err := filepath.Abs(entry.Path)
	if err != nil {
		return "", false, err
	}
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}

	return root, true, nil
}
