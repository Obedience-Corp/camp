package main

import (
	"context"
	"os"

	workitemcmd "github.com/Obedience-Corp/camp/internal/commands/workitem"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/resolver"
	"github.com/Obedience-Corp/camp/pkg/commitkit"
)

// resolveCommitContext runs the workitem resolver against the campaign root
// and returns the captured quest id and ref for inclusion in the commit
// tag. Resolution failures are non-fatal: empty strings are returned so the
// caller can still produce a quest- and workitem-free tag.
//
// explicit, when non-empty, is the user-supplied --workitem selector.
//
// When the resolved workitem is a directory-kind item with no `ref` field
// (pre-v1alpha6), the ref is auto-backfilled into the .workitem marker on
// disk so future commits inherit it. A stderr warning notifies the user.
func resolveCommitContext(ctx context.Context, campaignRoot, explicit string) (questID, workitemRef string) {
	res, err := resolver.Resolve(ctx, campaignRoot, resolver.Options{
		Explicit: explicit,
	})
	if err != nil || res == nil || res.Workitem == nil {
		return "", ""
	}
	ref, ensureErr := workitemcmd.EnsureRefForCommit(ctx, campaignRoot, res.Workitem, os.Stderr)
	if ensureErr != nil {
		return res.QuestID, workitemRefFor(res.Workitem)
	}
	if ref != "" {
		return res.QuestID, ref
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

// workitemEnvForCommit resolves the active workitem and returns the
// CAMP_WORKITEM_* env vars for the auto-write hook. Returns nil when no
// workitem context resolves so the hook sees no leaked vars.
func workitemEnvForCommit(ctx context.Context, campaignRoot, explicit string) []string {
	res, err := resolver.Resolve(ctx, campaignRoot, resolver.Options{
		Explicit: explicit,
	})
	if err != nil || res == nil || res.Workitem == nil {
		return nil
	}
	return commitkit.WorkitemEnv(res.Workitem, campaignRoot)
}
