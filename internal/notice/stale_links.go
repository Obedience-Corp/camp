package notice

import (
	"context"
	"os"
	"path/filepath"
	"strconv"

	"github.com/Obedience-Corp/camp/internal/workitem/links"
)

// StaleLinksID identifies the stale-workitem-links signal.
const StaleLinksID = "workitem-links-stale"

// StaleLinks reports a notice when links.yaml has rows whose scope path is gone
// from disk -- a deleted worktree, a renamed design directory.
//
// Writers no longer refuse on stale rows (see links.ValidateOne), so rot that
// used to announce itself by breaking the next `camp workitem link` is now
// silent. This is what makes it visible again, on a command the user already
// runs, pointing at the repair that already exists.
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
		if link.Scope.Path == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(campaignRoot, filepath.FromSlash(link.Scope.Path))); os.IsNotExist(err) {
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
