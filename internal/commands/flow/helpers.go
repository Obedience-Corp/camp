package flow

import (
	"context"
	"os"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/dungeon/spelling"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// getCwd returns the current working directory.
func getCwd() (string, error) {
	return os.Getwd()
}

// campaignDungeonSpelling resolves the dungeon spelling a new dungeon under a
// workflow should adopt: whatever the owning campaign already uses. Outside a
// campaign there is nothing to stay consistent with, so the dungeon_hidden
// setting decides.
func campaignDungeonSpelling(ctx context.Context) (string, error) {
	globalCfg, err := config.LoadGlobalConfig(ctx)
	if err != nil {
		return "", camperrors.Wrap(err, "loading global config")
	}
	campaignRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return spelling.NameFor(globalCfg.ResolveDungeonHidden()), nil
	}
	return spelling.CampaignName(ctx, campaignRoot, globalCfg.ResolveDungeonHidden())
}
