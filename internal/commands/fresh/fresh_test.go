package fresh

import (
	"testing"

	"github.com/Obedience-Corp/camp/internal/prune"
)

func TestPruneSkippedWorktreeNames_UsesTypedSkipReason(t *testing.T) {
	results := []prune.Result{
		{
			Branch:     "stable-v0.1.2",
			Status:     prune.StatusSkipped,
			Detail:     "kept stable worktree mounted elsewhere",
			SkipReason: prune.SkipReasonActiveWorktree,
		},
		{
			Branch: "cancelled",
			Status: prune.StatusSkipped,
			Detail: "active worktree: /tmp/path-that-should-not-match",
		},
		{
			Branch:     "deleted",
			Status:     prune.StatusDeleted,
			SkipReason: prune.SkipReasonActiveWorktree,
		},
	}

	got := pruneSkippedWorktreeNames(results)
	if len(got) != 1 || got[0] != "stable-v0.1.2" {
		t.Fatalf("pruneSkippedWorktreeNames() = %v, want [stable-v0.1.2]", got)
	}
}
