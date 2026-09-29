package fresh

import "testing"

func TestPruneChecklistStatusCountsAndListsNames(t *testing.T) {
	names := []string{"festival-starter-camp", "fix-leverage-worktrees"}
	status, listed := pruneChecklistStatus(names, 2, false)
	if status != "deleted 2 branches, removed 2 worktrees" {
		t.Fatalf("status = %q", status)
	}
	if len(listed) != 2 || listed[0] != names[0] {
		t.Fatalf("names = %#v", listed)
	}

	status, listed = pruneChecklistStatus(nil, 1, true)
	if status != "would remove 1 worktree" {
		t.Fatalf("worktree-only status = %q", status)
	}
	if len(listed) != 0 {
		t.Fatalf("worktree-only names = %#v", listed)
	}

	status, listed = pruneChecklistStatus([]string{"one"}, 0, false)
	if status != "deleted 1 branch" || len(listed) != 1 {
		t.Fatalf("branch-only = %q %#v", status, listed)
	}
}
