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

// AttachPrimary is the writer behind `camp project worktree add --workitem`.
// A stale row elsewhere in links.yaml must not stop it.
func TestAttachPrimary_SucceedsAlongsideStaleRow(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".campaign", "workitems"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Seed a row whose scope path does not exist, the way a deleted worktree
	// leaves one behind.
	stalePath := "projects/worktrees/fest/deleted"
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(stalePath)), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := AttachPrimary(ctx, root, AttachOptions{
		WorkitemID: "design-deleted-2026-07-01",
		Scope:      LinkScope{Kind: ScopeWorktree, Path: stalePath},
		CreatedBy:  "test",
	}); err != nil {
		t.Fatalf("seed stale link: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(root, filepath.FromSlash(stalePath))); err != nil {
		t.Fatal(err)
	}

	livePath := "projects/worktrees/fest/live"
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(livePath)), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := AttachPrimary(ctx, root, AttachOptions{
		WorkitemID: "design-live-2026-07-26",
		Scope:      LinkScope{Kind: ScopeWorktree, Path: livePath},
		CreatedBy:  "test",
	}); err != nil {
		t.Fatalf("stale row blocked a valid attach: %v", err)
	}

	// The stale row is preserved, not silently dropped: doctor owns removal.
	reg, err := Load(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.PrimaryForScope(ScopeWorktree, stalePath); !ok {
		t.Fatal("stale row was dropped by the write path; doctor --fix owns that decision")
	}
}

// The new link's own scope path is still checked, so the fix does not turn into
// "writes never validate".
func TestAttachPrimary_StillRejectsMissingScopePath(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".campaign", "workitems"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := AttachPrimary(context.Background(), root, AttachOptions{
		WorkitemID: "design-nowhere-2026-07-26",
		Scope:      LinkScope{Kind: ScopeWorktree, Path: "projects/worktrees/fest/nope"},
		CreatedBy:  "test",
	})
	if err == nil {
		t.Fatal("expected a missing-scope-path rejection for the link being written")
	}
}

func TestWorktreeScopePath(t *testing.T) {
	if got := WorktreeScopePath("fest", "list-watch"); got != "projects/worktrees/fest/list-watch" {
		t.Fatalf("WorktreeScopePath = %q", got)
	}
}
