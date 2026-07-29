package skills

import (
	"testing"
)

// Pure-logic tests only. Filesystem-mutating worktree projection coverage
// lives in tests/integration/skills_worktree_test.go (container harness).

func TestMergeWorktreeProjectionStates(t *testing.T) {
	agents := ProjectionState{TotalSkills: 3, Linked: 3}
	claude := ProjectionState{TotalSkills: 3, Linked: 1, Mismatched: 1}
	grok := ProjectionState{TotalSkills: 3, Linked: 3, Conflicts: 1}

	got := mergeWorktreeProjectionStates(agents, claude, grok)
	if got.Linked != 1 {
		t.Errorf("Linked = %d, want min surface = 1", got.Linked)
	}
	if got.Mismatched != 1 {
		t.Errorf("Mismatched = %d, want 1", got.Mismatched)
	}
	if got.Conflicts != 1 {
		t.Errorf("Conflicts = %d, want 1", got.Conflicts)
	}
	if got.TotalSkills != 3 {
		t.Errorf("TotalSkills = %d, want 3", got.TotalSkills)
	}
}

func TestMergeWorktreeProjectionStatesEmpty(t *testing.T) {
	got := mergeWorktreeProjectionStates()
	if got.TotalSkills != 0 || got.Linked != 0 {
		t.Errorf("empty merge = %+v", got)
	}
}

func TestWorktreeSkillExcludePatterns(t *testing.T) {
	want := map[string]bool{".agents/": true, ".claude/": true, ".grok/": true}
	if len(worktreeSkillExcludePatterns) != len(want) {
		t.Fatalf("patterns = %v, want %d entries", worktreeSkillExcludePatterns, len(want))
	}
	for _, p := range worktreeSkillExcludePatterns {
		if !want[p] {
			t.Errorf("unexpected exclude pattern %q", p)
		}
	}
}
