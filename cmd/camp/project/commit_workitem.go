package project

import (
	"context"

	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/resolver"
	"github.com/Obedience-Corp/camp/pkg/commitkit"
)

// resolveProjectCommitContext runs the workitem resolver against the
// campaign root and returns the captured quest id and ref for inclusion in
// the project-commit tag. Resolution failures are non-fatal: empty strings
// are returned so callers can still produce a quest- and workitem-free tag.
//
// explicit, when non-empty, is the user-supplied --workitem selector and
// short-circuits the cwd-based resolution.
func resolveProjectCommitContext(ctx context.Context, campaignRoot, explicit string) (questID, workitemRef string) {
	res, err := resolver.Resolve(ctx, campaignRoot, resolver.Options{
		Explicit: explicit,
	})
	if err != nil || res == nil || res.Workitem == nil {
		return "", ""
	}
	return res.QuestID, workitemRefFor(res.Workitem)
}

func workitemRefFor(wi *wkitem.WorkItem) string {
	if wi == nil || wi.SourceMetadata == nil {
		return ""
	}
	if v, ok := wi.SourceMetadata["ref"].(string); ok {
		return v
	}
	return ""
}

// workitemEnvForProjectCommit resolves the active workitem and returns the
// CAMP_WORKITEM_* env vars for the auto-write hook. Returns nil when no
// workitem context resolves.
func workitemEnvForProjectCommit(ctx context.Context, campaignRoot, explicit string) []string {
	res, err := resolver.Resolve(ctx, campaignRoot, resolver.Options{
		Explicit: explicit,
	})
	if err != nil || res == nil || res.Workitem == nil {
		return nil
	}
	return commitkit.WorkitemEnv(res.Workitem, campaignRoot)
}
