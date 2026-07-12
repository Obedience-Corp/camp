package links

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestAttachPrimaryWorktree(t *testing.T) {
	root := t.TempDir()
	// Worktree path must exist for ValidateLinkPath.
	wtRel := "projects/worktrees/fest/demo"
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(wtRel)), 0o755); err != nil {
		t.Fatal(err)
	}
	// Seed empty registry.
	if err := os.MkdirAll(filepath.Join(root, ".campaign", "workitems"), 0o755); err != nil {
		t.Fatal(err)
	}

	link, err := AttachPrimary(context.Background(), root, AttachOptions{
		WorkitemID:  "design-fest-list-watch-2026-07-12",
		WorkitemKey: "design:workflow/design/fest-list-watch",
		Scope:       LinkScope{Kind: ScopeWorktree, Path: wtRel},
		CreatedBy:   "test",
		// Workitem may not exist on disk in this unit test.
		AllowMissing: true,
	})
	if err != nil {
		t.Fatalf("AttachPrimary: %v", err)
	}
	if link.ID == "" || link.Role != RolePrimary {
		t.Fatalf("unexpected link: %+v", link)
	}
	if link.Scope.Path != wtRel {
		t.Fatalf("scope path = %q, want %q", link.Scope.Path, wtRel)
	}

	// Reload and confirm primary covers the worktree path.
	reg, err := Load(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	primary, ok := reg.PrimaryForScope(ScopeWorktree, wtRel)
	if !ok || primary.WorkitemID != "design-fest-list-watch-2026-07-12" {
		t.Fatalf("primary after reload = %+v ok=%v", primary, ok)
	}
}

func TestWorktreeScopePath(t *testing.T) {
	if got := WorktreeScopePath("fest", "list-watch"); got != "projects/worktrees/fest/list-watch" {
		t.Fatalf("WorktreeScopePath = %q", got)
	}
}
