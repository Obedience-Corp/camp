package project

import (
	"context"
	"path/filepath"

	"github.com/Obedience-Corp/camp/internal/config"
)

func campaignWorktreesRoot(ctx context.Context, campaignRoot string) string {
	jumps, err := config.LoadJumpsConfig(ctx, campaignRoot)
	if err == nil && jumps != nil {
		return filepath.Join(campaignRoot, jumps.Paths.Worktrees)
	}

	return filepath.Join(campaignRoot, config.DefaultCampaignPaths().Worktrees)
}

func campaignProjectWorktreePath(ctx context.Context, campaignRoot, name string) string {
	return filepath.Join(campaignWorktreesRoot(ctx, campaignRoot), name)
}
