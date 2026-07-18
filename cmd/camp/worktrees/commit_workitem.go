package worktrees

import (
	"context"
	"os"

	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/resolver"
	"github.com/Obedience-Corp/camp/pkg/commitkit"
)

func resolveWorktreeCommitContext(ctx context.Context, campaignRoot, cwd, explicit string) (questID, festivalRef, workitemRef string) {
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

func workitemEnvForWorktreeCommit(ctx context.Context, campaignRoot, cwd, explicit string) []string {
	res, err := resolver.Resolve(ctx, campaignRoot, resolver.Options{
		Explicit: explicit,
		Cwd:      cwd,
	})
	if err != nil || res == nil || res.Workitem == nil {
		return nil
	}
	return commitkit.WorkitemEnv(res.Workitem, campaignRoot)
}
