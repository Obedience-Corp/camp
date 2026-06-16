package worktrees

import (
	"context"
	"os"

	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/resolver"
	"github.com/Obedience-Corp/camp/pkg/commitkit"
)

func resolveWorktreeCommitContext(ctx context.Context, campaignRoot, cwd, explicit string) (questID, workitemRef string) {
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
