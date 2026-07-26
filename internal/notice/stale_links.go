package notice

import (
	"context"
	"strconv"

	"github.com/Obedience-Corp/camp/internal/workitem/links"
)

// StaleLinksID identifies the stale-workitem-links signal.
const StaleLinksID = "workitem-links-stale"

// StaleLinks reports a notice when links.yaml has rows whose scope target is
// authoritatively gone -- a deleted design directory, a removed festival.
//
// Writers prune these as they go and say so, so the notice is the backstop for
// a campaign that has not written a link since the rot appeared, not the
// primary surface.
//
// It deliberately ignores machine-local scopes. Worktrees are gitignored by
// camp's own scaffold and submodules may simply be uncloned, so on a campaign
// synced across two machines every row the other machine owns would be counted
// here. That notice would be permanent, unactionable, and would train the user
// to ignore the next one. See links.MachineLocal.
//
// It stays stat-level as the package requires: one read of links.yaml plus one
// stat per row. Broken workitem_id references need the on-disk workitem set,
// which is a tree scan, so they are left to `camp workitem doctor` -- the same
// command this notice names, which reports both.
func StaleLinks(ctx context.Context, campaignRoot string) (*Notice, error) {
	registry, err := links.Load(ctx, campaignRoot)
	if err != nil {
		return nil, err
	}

	stale := 0
	for _, link := range registry.Links {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if link.Scope.Path == "" || links.MachineLocal(campaignRoot, link.Scope) {
			continue
		}
		if !links.ScopeTargetExists(campaignRoot, link.Scope) {
			stale++
		}
	}
	if stale == 0 {
		return nil, nil
	}

	subject := "workitem links point"
	if stale == 1 {
		subject = "workitem link points"
	}
	return &Notice{
		ID:      StaleLinksID,
		Message: strconv.Itoa(stale) + " " + subject + " at a path that no longer exists",
		Command: "camp workitem doctor --fix",
	}, nil
}
