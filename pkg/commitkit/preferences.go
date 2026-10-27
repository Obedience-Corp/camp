package commitkit

import (
	"context"

	"github.com/Obedience-Corp/camp/internal/config"
)

// CommitTagsEnabled resolves Camp's commit-tag preference for a consumer such
// as fest. A camp-local commit block replaces the machine-global block; an
// empty campaignRoot uses only the global preference. Tags default to enabled.
// Callers must handle configuration errors before staging or committing rather
// than silently choosing a different tag policy.
func CommitTagsEnabled(ctx context.Context, campaignRoot string) (bool, error) {
	prefs, err := config.EffectiveCommitPrefs(ctx, campaignRoot)
	if err != nil {
		return false, err
	}
	return prefs.TagCommits(), nil
}
