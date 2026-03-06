package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/dungeon"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// dungeonCommandContext captures shared path/config values for dungeon commands.
type dungeonCommandContext struct {
	Config       *config.CampaignConfig
	CampaignRoot string
	WorkingDir   string
	Dungeon      dungeon.Context
}

func resolveDungeonCommandContext(ctx context.Context) (*dungeonCommandContext, error) {
	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return nil, camperrors.Wrap(err, "not in a campaign directory")
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil, camperrors.Wrap(err, "getting current directory")
	}

	dungeonCtx, err := dungeon.ResolveContext(ctx, campaignRoot, cwd)
	if err != nil {
		if errors.Is(err, dungeon.ErrDungeonContextNotFound) {
			return nil, fmt.Errorf(
				"no dungeon context found from %s to campaign root; run 'camp dungeon add' in the target directory",
				cwd,
			)
		}
		return nil, camperrors.Wrap(err, "resolving dungeon context")
	}

	return &dungeonCommandContext{
		Config:       cfg,
		CampaignRoot: campaignRoot,
		WorkingDir:   cwd,
		Dungeon:      dungeonCtx,
	}, nil
}
