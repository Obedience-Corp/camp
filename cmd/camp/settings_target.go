package main

import (
	"context"
	"strings"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/spf13/cobra"
)

type settingsTargetKey struct{}

func addSettingsCampaignFlag(cmd *cobra.Command) {
	cmd.Flags().StringP("campaign", "c", "", "Target camp by exact registered name or ID (independent of cwd)")
}

// Resolve once per invocation. Every local/effective read and write uses this
// context, without changing process cwd or the shared campaign detection cache.
func settingsTargetContext(cmd *cobra.Command) (context.Context, error) {
	ctx := cmd.Context()
	if !cmd.Flags().Changed("campaign") {
		return ctx, nil
	}
	query, err := cmd.Flags().GetString("campaign")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(query) == "" {
		return nil, camperrors.New("--campaign requires a registered camp name or ID")
	}
	reg, err := config.LoadRegistry(ctx)
	if err != nil {
		return nil, camperrors.Wrap(err, "load target camp registry")
	}
	selected, err := selectSettingsCampaign(reg, query)
	if err != nil {
		return nil, err
	}
	// Identity validation must not backfill an ID or create jumps.yaml before
	// accepting the target: a rejected get or set must leave the camp untouched.
	cfg, err := config.ReadCampaignConfig(ctx, selected.Path)
	if err != nil {
		return nil, camperrors.Wrap(err, "load target camp")
	}
	if cfg.ID != selected.ID {
		return nil, camperrors.New("target camp identity no longer matches its registry entry; register it again")
	}
	return context.WithValue(ctx, settingsTargetKey{}, selected.Path), nil
}

// Mutation targets must be exact and unique; a typo must never select a fuzzy
// match and silently write a different camp.
func selectSettingsCampaign(reg *config.Registry, query string) (config.RegisteredCampaign, error) {
	if selected, ok := reg.GetByID(query); ok {
		return selected, nil
	}
	var matches []config.RegisteredCampaign
	for _, entry := range reg.ListAll() {
		if entry.Name == query {
			matches = append(matches, entry)
		}
	}
	if len(matches) != 1 {
		return config.RegisteredCampaign{}, camperrors.NewValidation("campaign", "target must match one registered camp name or full ID", nil)
	}
	return matches[0], nil
}

func settingsCampaignRoot(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if root, ok := ctx.Value(settingsTargetKey{}).(string); ok {
		return root, nil
	}
	return campaign.DetectCached(ctx)
}
