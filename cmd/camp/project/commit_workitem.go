package project

import (
	"context"
	"os"

	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/resolver"
	"github.com/Obedience-Corp/camp/pkg/commitkit"
)

// resolveProjectCommitContext runs the workitem resolver against the
// campaign root and returns the captured quest id and ref for inclusion in
// the project-commit tag. Resolution failures are non-fatal: empty strings
// are returned so callers can still produce a quest- and workitem-free tag.
//
// cwd is the resolved project path used for cwd/link-based resolution.
// explicit, when non-empty, is the user-supplied --workitem selector and
// short-circuits cwd-based resolution.
func resolveProjectCommitContext(ctx context.Context, campaignRoot, cwd, explicit string) (questID, workitemRef string) {
	res, err := resolver.Resolve(ctx, campaignRoot, resolver.Options{
		Explicit: explicit,
		Cwd:      cwd,
	})
	if err != nil || res == nil || res.Workitem == nil {
		return "", ""
	}
	ref, ensureErr := wkitem.EnsureRefForCommit(ctx, campaignRoot, res.Workitem, os.Stderr)
	if ensureErr != nil {
		return res.QuestID, wkitem.RefOf(res.Workitem)
	}
	if ref != "" {
		return res.QuestID, ref
	}
	return res.QuestID, wkitem.RefOf(res.Workitem)
}

// workitemEnvForProjectCommit resolves the active workitem and returns the
// CAMP_WORKITEM_* env vars for the auto-write hook. Returns nil when no
// workitem context resolves.
func workitemEnvForProjectCommit(ctx context.Context, campaignRoot, cwd, explicit string) []string {
	res, err := resolver.Resolve(ctx, campaignRoot, resolver.Options{
		Explicit: explicit,
		Cwd:      cwd,
	})
	if err != nil || res == nil || res.Workitem == nil {
		return nil
	}
	return commitkit.WorkitemEnv(res.Workitem, campaignRoot)
}
