package project

import (
	"context"
	"os"

	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/resolver"
	"github.com/Obedience-Corp/camp/pkg/commitkit"
)

// resolveProjectCommitContext runs the workitem resolver against the
// campaign root and returns the captured quest id, festival ref, and workitem
// ref for inclusion in the project-commit tag. Resolution failures are
// non-fatal: empty strings are returned so callers can still produce a quest-,
// festival-, and workitem-free tag.
//
// A worktree whose primary link resolves to a festival (the state left by
// `camp workitem promote --target festival`) carries the festival ref (FE-),
// not a workitem ref (WI-), since a festival has no WI- ref of its own.
//
// cwd is the resolved project path used for cwd/link-based resolution.
// explicit, when non-empty, is the user-supplied --workitem selector and
// short-circuits cwd-based resolution.
func resolveProjectCommitContext(ctx context.Context, campaignRoot, cwd, explicit string) (questID, festivalRef, workitemRef string) {
	res, err := resolver.Resolve(ctx, campaignRoot, resolver.Options{
		Explicit: explicit,
		Cwd:      cwd,
	})
	if err != nil || res == nil || res.Workitem == nil {
		return "", "", ""
	}
	festivalRef = wkitem.FestivalRef(res.Workitem)
	ref, ensureErr := wkitem.EnsureRefForCommit(ctx, campaignRoot, res.Workitem, os.Stderr)
	if ensureErr != nil || ref == "" {
		ref = wkitem.RefOf(res.Workitem)
	}
	return res.QuestID, festivalRef, ref
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
