package fresh

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestSameWorktreePath(t *testing.T) {
	if !sameWorktreePath("/a/b", "/a/b") {
		t.Error("identical paths should match")
	}
	if !sameWorktreePath("/a/b/", "/a/b") {
		t.Error("trailing slash should not matter")
	}
	if sameWorktreePath("/a/b", "/a/c") {
		t.Error("different paths should not match")
	}
	if sameWorktreePath("", "/a") || sameWorktreePath("/a", "") {
		t.Error("empty path should not match")
	}
}

func TestEmptyBranchLabel(t *testing.T) {
	if got := emptyBranchLabel(""); got != "detached HEAD" {
		t.Errorf("empty = %q", got)
	}
	if got := emptyBranchLabel("main"); got != "main" {
		t.Errorf("main = %q", got)
	}
}

func TestIsBranchInUseByWorktree(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("fatal: 'main' is already used by worktree at '/tmp/x'"), true},
		{errors.New("'main' is already checked out at '/tmp/x'"), true},
		{errors.New("conflict: path is unmerged"), false},
	}
	for _, tc := range cases {
		if got := isBranchInUseByWorktree(tc.err); got != tc.want {
			t.Errorf("isBranchInUseByWorktree(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

func TestFreshSyncWorktreeNote(t *testing.T) {
	if got := freshSyncWorktreeNote(freshSyncState{}); got != "" {
		t.Errorf("empty = %q", got)
	}
	got := freshSyncWorktreeNote(freshSyncState{
		defaultBranch: "main",
		worktreePath:  "/campaign/projects/worktrees/camp/old-feature",
	})
	want := "(main still in " + filepath.Base("/campaign/projects/worktrees/camp/old-feature") + ")"
	if got != want {
		t.Errorf("note = %q, want %q", got, want)
	}
}
