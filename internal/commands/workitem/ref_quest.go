package workitem

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/quest"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
)

// deriveUniqueRef enumerates existing workitem refs and returns a fresh ref
// for id that does not collide with any of them.
func deriveUniqueRef(ctx context.Context, campaignRoot string, cfg *config.CampaignConfig, id string) (string, error) {
	resolver := paths.NewResolverFromConfig(campaignRoot, cfg)
	items, err := wkitem.Discover(ctx, campaignRoot, resolver)
	if err != nil {
		return "", camperrors.Wrap(err, "scan existing refs")
	}
	existing := wkitem.RefsFromWorkitems(items)
	return wkitem.DeriveUnique(ctx, id, existing)
}

// resolveQuestIDForCreate calls quest.ResolveContext to capture the active
// quest's id at workitem-create time. Quest resolution failures are
// non-fatal: the function returns "" and emits a one-line warning on
// stderr. This keeps the quest-agnostic behavior intact for users who do
// not run quests.
func resolveQuestIDForCreate(ctx context.Context, cmd *cobra.Command, campaignRoot, explicit string) string {
	q, err := quest.ResolveContext(ctx, campaignRoot, explicit)
	if err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: quest resolution failed: %v\n", err)
		return ""
	}
	if q == nil {
		return ""
	}
	return q.ID
}
