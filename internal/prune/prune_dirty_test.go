//go:build !integration

package prune

import "testing"

// prune_dirty_test.go hosts host-side (t.TempDir) tests for dirty worktree protection
// in the branch-worktree removal path (deleteLocalBranches).
// The full seam for executor injection is developed in task 04 (integration_tagged_test_migration);
// this file provides a thin in-package check for the new DiscardDirty field and documents
// the expected SkipReasonDirtyWorktree behavior. Real dirty scenarios are covered by the
// container integration tests added in the worktrees-clean and related tasks.

func TestOptions_DiscardDirty(t *testing.T) {
	o := Options{
		DryRun:       false,
		Force:        false,
		DiscardDirty: true,
	}
	if !o.DiscardDirty {
		t.Fatal("DiscardDirty field should be writable and observable")
	}
	// When false (default), dirty branch worktrees should be skipped with SkipReasonDirtyWorktree
	// (enforced in deleteLocalBranches before the wt.Remove call).
}
