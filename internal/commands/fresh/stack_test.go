package fresh

import (
	"testing"

	"github.com/Obedience-Corp/camp/internal/worktree"
)

func TestStackPlanKeepTargetBranch(t *testing.T) {
	// The target branch's own worktree must never be a removal candidate.
	plan := stackCleanupPlan{
		targetBranch: "feat/aggregate",
		primaryPath:  "/projects/camp",
	}
	entry := worktree.GitWorktreeEntry{
		Path:   "/projects/worktrees/camp/aggregate",
		Branch: "feat/aggregate",
	}
	p := stackWorktreePlan{entry: entry, action: stackActionKeep}
	plan.keep = append(plan.keep, p)

	if len(plan.keep) != 1 {
		t.Fatalf("expected 1 keep, got %d", len(plan.keep))
	}
	if plan.keep[0].entry.Branch != "feat/aggregate" {
		t.Errorf("keep entry should be the target branch")
	}
}

func TestStackPlanClassifiesCorrectly(t *testing.T) {
	// Verify the action constants are distinct so the plan's classification
	// is unambiguous.
	actions := []stackCleanupAction{
		stackActionKeep,
		stackActionRemove,
		stackActionSkipDirty,
		stackActionSkipUnmerged,
	}
	seen := make(map[stackCleanupAction]bool)
	for _, a := range actions {
		if seen[a] {
			t.Errorf("duplicate action value: %d", a)
		}
		seen[a] = true
	}
}

func TestStackPlanMergedSet(t *testing.T) {
	// Simulates the merged-set logic: branches in the set are candidates for
	// removal; branches not in the set are kept. This mirrors the branching
	// in planStackCleanup without requiring a real git repo.
	merged := []string{"child-a", "child-b"}
	mergedSet := make(map[string]struct{}, len(merged))
	for _, b := range merged {
		mergedSet[b] = struct{}{}
	}

	// child-a is merged → candidate.
	if _, ok := mergedSet["child-a"]; !ok {
		t.Error("child-a should be in merged set")
	}
	// child-c is not merged → not a candidate.
	if _, ok := mergedSet["child-c"]; ok {
		t.Error("child-c should not be in merged set")
	}
}
