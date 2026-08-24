package fresh

import (
	"context"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/worktree"
)

func TestStackCleanupActionsAreDistinct(t *testing.T) {
	actions := []stackCleanupAction{
		stackActionKeep,
		stackActionRemove,
		stackActionSkipDirty,
		stackActionSkipUnmerged,
		stackActionSkipOffStack,
	}
	seen := make(map[stackCleanupAction]bool)
	for _, a := range actions {
		if seen[a] {
			t.Errorf("duplicate action value: %d", a)
		}
		seen[a] = true
	}
}

func TestIsProtectedDefaultBranch(t *testing.T) {
	cases := []struct {
		target, def string
		want        bool
	}{
		{"main", "main", true},
		{"master", "develop", true},
		{"develop", "develop", true},
		{"feat/aggregate", "main", false},
		{"feat/aggregate", "", false},
		{"", "main", false},
		{"  main  ", "main", true},
	}
	for _, tc := range cases {
		got := isProtectedDefaultBranch(tc.target, tc.def)
		if got != tc.want {
			t.Errorf("isProtectedDefaultBranch(%q, %q) = %v, want %v", tc.target, tc.def, got, tc.want)
		}
	}
}

func TestShouldScopeToStackChildren(t *testing.T) {
	if !shouldScopeToStackChildren("feat/aggregate", "main", false) {
		t.Error("aggregate target should stay scoped")
	}
	if !shouldScopeToStackChildren("feat/aggregate", "main", true) {
		t.Error("allow-default-target must not disable scoping for a non-default target")
	}
	if shouldScopeToStackChildren("main", "main", true) {
		t.Error("explicit default-branch override should disable off-stack filtering")
	}
	if !shouldScopeToStackChildren("main", "main", false) {
		t.Error("without the override, default targets stay scoped (and are refused separately)")
	}
}

func TestValidateStackCleanupTarget_EmptyBranch(t *testing.T) {
	err := validateStackCleanupTarget(context.Background(), "/nope", "", false)
	if err == nil {
		t.Fatal("expected error for empty target branch")
	}
	if !strings.Contains(err.Error(), "--cleanup-stack requires --branch") {
		t.Errorf("error = %q", err)
	}
}

func TestValidateStackCleanupTarget_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := validateStackCleanupTarget(ctx, "/nope", "feat/aggregate", false)
	if err == nil {
		t.Fatal("expected cancelled context error")
	}
}

func TestPlanStackCleanup_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := planStackCleanup(ctx, "/nope", "feat/aggregate", false)
	if err == nil {
		t.Fatal("expected cancelled context error")
	}
}

func TestExecuteStackCleanup_EmptyRemove(t *testing.T) {
	removed, errs := executeStackCleanup(context.Background(), stackCleanupPlan{projectPath: "/nope"})
	if removed != 0 || len(errs) != 0 {
		t.Fatalf("empty plan: removed=%d errs=%v", removed, errs)
	}
}

func TestExecuteStackCleanup_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, errs := executeStackCleanup(ctx, stackCleanupPlan{
		projectPath: "/nope",
		remove: []stackWorktreePlan{{
			entry: worktree.GitWorktreeEntry{Path: "/x", Branch: "y"},
		}},
	})
	if len(errs) == 0 {
		t.Fatal("expected cancellation error")
	}
}
