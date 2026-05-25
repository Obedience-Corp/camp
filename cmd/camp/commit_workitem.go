package main

import (
	"context"

	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/resolver"
)

// resolveCommitContext runs the workitem resolver against the campaign root
// and returns the captured quest id and ref for inclusion in the commit
// tag. Resolution failures are non-fatal: empty strings are returned so the
// caller can still produce a quest- and workitem-free tag.
//
// explicit, when non-empty, is the user-supplied --workitem selector.
func resolveCommitContext(ctx context.Context, campaignRoot, explicit string) (questID, workitemRef string) {
	res, err := resolver.Resolve(ctx, campaignRoot, resolver.Options{
		Explicit: explicit,
	})
	if err != nil || res == nil || res.Workitem == nil {
		return "", ""
	}
	return res.QuestID, workitemRefFor(res.Workitem)
}

// workitemRefFor returns the ref carried on the resolved workitem's
// SourceMetadata, or empty if none was captured (e.g. v1alpha5 workitems
// that have not yet been backfilled).
func workitemRefFor(wi *wkitem.WorkItem) string {
	if wi == nil || wi.SourceMetadata == nil {
		return ""
	}
	if v, ok := wi.SourceMetadata["ref"].(string); ok {
		return v
	}
	return ""
}
