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

func TestApplyReclaimDecision(t *testing.T) {
	// Starting state mirrors resolveFreshSyncState when another worktree holds main.
	state := freshSyncState{
		defaultBranch: "main",
		baseRef:       "origin/main",
		displayRef:    "origin/main (detached)",
		detached:      true,
		worktreePath:  "/campaign/projects/worktrees/camp/stuck-main",
	}

	applyReclaimDecision(&state)

	if !state.reclaimed {
		t.Error("reclaimed should be true")
	}
	if state.detached {
		t.Error("detached should be false after reclaim")
	}
	if state.baseRef != "main" {
		t.Errorf("baseRef = %q, want main", state.baseRef)
	}
	if state.displayRef != "main" {
		t.Errorf("displayRef = %q, want main", state.displayRef)
	}
	// Occupying path is retained for messaging until the step finishes.
	if state.worktreePath != "/campaign/projects/worktrees/camp/stuck-main" {
		t.Errorf("worktreePath should be preserved, got %q", state.worktreePath)
	}

	// Nil must not panic.
	applyReclaimDecision(nil)
}

func TestReclaimStepDetail(t *testing.T) {
	const branch = "main"
	const path = "/campaign/projects/worktrees/camp/stuck-main"
	base := filepath.Base(path)

	cases := []struct {
		name      string
		reclaimed bool
		dryRun    bool
		wantLabel string
		wantDet   string
	}{
		{
			name:      "real reclaim success",
			reclaimed: true,
			dryRun:    false,
			wantLabel: "Free main                        ",
			wantDet:   "(detached " + base + " so main is free here)",
		},
		{
			name:      "real reclaim dirty skip",
			reclaimed: false,
			dryRun:    false,
			wantLabel: "Free main                        ",
			wantDet:   "skipped · " + base + " has uncommitted changes; syncing detached",
		},
		{
			name:      "dry-run would reclaim",
			reclaimed: true,
			dryRun:    true,
			wantLabel: "Would free main                   ",
			wantDet:   "(detach clean worktree " + base + ")",
		},
		{
			name:      "dry-run dirty skip",
			reclaimed: false,
			dryRun:    true,
			wantLabel: "Would free main                   ",
			wantDet:   "skipped · " + base + " has uncommitted changes; would sync detached",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			label, detail := reclaimStepDetail(branch, path, tc.reclaimed, tc.dryRun)
			if label != tc.wantLabel {
				t.Errorf("label = %q, want %q", label, tc.wantLabel)
			}
			if detail != tc.wantDet {
				t.Errorf("detail = %q, want %q", detail, tc.wantDet)
			}
		})
	}
}

// TestReclaimDecisionMatrix locks the resolve → reclaim state transitions that
// executeFresh relies on, without git.
func TestReclaimDecisionMatrix(t *testing.T) {
	occupied := func() freshSyncState {
		return freshSyncState{
			defaultBranch: "main",
			baseRef:       "origin/main",
			displayRef:    "origin/main (detached)",
			detached:      true,
			worktreePath:  "/tmp/other",
		}
	}

	t.Run("clean reclaim enables normal checkout", func(t *testing.T) {
		state := occupied()
		// Simulate maybeReclaimDefaultBranch after can/reclaim returned true.
		applyReclaimDecision(&state)
		if state.detached || !state.reclaimed {
			t.Fatalf("want reclaimed normal checkout, got detached=%v reclaimed=%v", state.detached, state.reclaimed)
		}
		if state.baseRef != "main" || state.displayRef != "main" {
			t.Fatalf("refs = %q / %q, want main", state.baseRef, state.displayRef)
		}
	})

	t.Run("dirty skip keeps detached fallback", func(t *testing.T) {
		state := occupied()
		// maybeReclaimDefaultBranch does not call applyReclaimDecision when dirty.
		if !state.detached || state.reclaimed {
			t.Fatalf("precondition failed: detached=%v reclaimed=%v", state.detached, state.reclaimed)
		}
		if state.baseRef != "origin/main" {
			t.Fatalf("baseRef = %q, want origin/main", state.baseRef)
		}
		note := freshSyncWorktreeNote(state)
		if note == "" {
			t.Fatal("expected worktree note for dirty fallback messaging")
		}
	})

	t.Run("no occupying worktree is a no-op", func(t *testing.T) {
		state := freshSyncState{
			defaultBranch: "main",
			baseRef:       "main",
			displayRef:    "main",
		}
		// maybeReclaimDefaultBranch returns immediately when worktreePath is empty.
		if state.worktreePath != "" || state.detached || state.reclaimed {
			t.Fatalf("unexpected occupied state: %+v", state)
		}
	})
}
