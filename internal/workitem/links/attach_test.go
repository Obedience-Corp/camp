package links

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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
// A dead row elsewhere in links.yaml must not stop it, and the row it drops on
// the way through has to be reported.
func TestAttachPrimary_PrunesDeadRowsAndReports(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".campaign", "workitems"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Seed a row against a tracked path, then delete the path so its absence
	// is authoritative.
	deadPath := "workflow/design/deleted"
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(deadPath)), 0o755); err != nil {
		t.Fatal(err)
	}
	dead, err := AttachPrimary(ctx, root, AttachOptions{
		WorkitemID: "design-deleted-2026-07-01",
		Scope:      LinkScope{Kind: ScopeCampaignPath, Path: deadPath},
		CreatedBy:  "test",
	})
	if err != nil {
		t.Fatalf("seed dead link: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(root, filepath.FromSlash(deadPath))); err != nil {
		t.Fatal(err)
	}

	livePath := "projects/worktrees/fest/live"
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(livePath)), 0o755); err != nil {
		t.Fatal(err)
	}
	var report strings.Builder
	if _, err := AttachPrimary(ctx, root, AttachOptions{
		WorkitemID: "design-live-2026-07-26",
		Scope:      LinkScope{Kind: ScopeWorktree, Path: livePath},
		CreatedBy:  "test",
		Report:     &report,
	}); err != nil {
		t.Fatalf("dead row blocked a valid attach: %v", err)
	}

	if !strings.Contains(report.String(), dead.ID) {
		t.Fatalf("pruning must be reported, got %q", report.String())
	}
	reg, err := Load(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.FindByID(dead.ID); ok {
		t.Fatal("dead row should have been pruned")
	}
}

// A worktree that is not on this machine is absent, not deleted. links.yaml is
// tracked, so pruning it here would delete it on the machine that owns it.
func TestAttachPrimary_PreservesMachineLocalRows(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".campaign", "workitems"), 0o755); err != nil {
		t.Fatal(err)
	}

	elsewhere := "projects/worktrees/fest/on-another-machine"
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(elsewhere)), 0o755); err != nil {
		t.Fatal(err)
	}
	seeded, err := AttachPrimary(ctx, root, AttachOptions{
		WorkitemID: "design-elsewhere-2026-07-01",
		Scope:      LinkScope{Kind: ScopeWorktree, Path: elsewhere},
		CreatedBy:  "test",
	})
	if err != nil {
		t.Fatalf("seed worktree link: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(root, filepath.FromSlash(elsewhere))); err != nil {
		t.Fatal(err)
	}

	livePath := "projects/worktrees/fest/live"
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(livePath)), 0o755); err != nil {
		t.Fatal(err)
	}
	var report strings.Builder
	if _, err := AttachPrimary(ctx, root, AttachOptions{
		WorkitemID: "design-live-2026-07-26",
		Scope:      LinkScope{Kind: ScopeWorktree, Path: livePath},
		CreatedBy:  "test",
		Report:     &report,
	}); err != nil {
		t.Fatalf("attach failed: %v", err)
	}

	if report.String() != "" {
		t.Fatalf("nothing should have been pruned, got %q", report.String())
	}
	reg, err := Load(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.FindByID(seeded.ID); !ok {
		t.Fatal("a worktree link was deleted because the worktree is on another machine")
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

// Reporting is the price of removing a row the user did not ask about, so a
// caller with nowhere to report to must not prune at all. Otherwise the one
// outcome the design forbids -- a silent removal -- is a nil field away.
func TestAttachPrimary_DoesNotPruneWithoutAReportWriter(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".campaign", "workitems"), 0o755); err != nil {
		t.Fatal(err)
	}

	deadPath := "workflow/design/deleted"
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(deadPath)), 0o755); err != nil {
		t.Fatal(err)
	}
	dead, err := AttachPrimary(ctx, root, AttachOptions{
		WorkitemID: "design-deleted-2026-07-01",
		Scope:      LinkScope{Kind: ScopeCampaignPath, Path: deadPath},
		CreatedBy:  "test",
	})
	if err != nil {
		t.Fatalf("seed dead link: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(root, filepath.FromSlash(deadPath))); err != nil {
		t.Fatal(err)
	}

	livePath := "projects/worktrees/fest/live"
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(livePath)), 0o755); err != nil {
		t.Fatal(err)
	}
	// No Report writer.
	if _, err := AttachPrimary(ctx, root, AttachOptions{
		WorkitemID: "design-live-2026-07-26",
		Scope:      LinkScope{Kind: ScopeWorktree, Path: livePath},
		CreatedBy:  "test",
	}); err != nil {
		t.Fatalf("attach failed: %v", err)
	}

	reg, err := Load(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.FindByID(dead.ID); !ok {
		t.Fatal("pruned silently with no Report writer; the row should have been left for a reporting caller or doctor")
	}
}

func TestReportPruned_UndoSaysItRevertsTheWholeWrite(t *testing.T) {
	var out strings.Builder
	ReportPruned(&out, []Pruned{{
		Link:   Link{ID: "lnk_20260726_eeeeee", Scope: LinkScope{Kind: ScopeCampaignPath, Path: "workflow/design/gone"}},
		Reason: "campaign_path workflow/design/gone no longer exists",
	}})
	// The prune shares a save with the caller's write, so an undo that looks
	// surgical would cost the user the link they just created.
	if !strings.Contains(out.String(), "reverts this whole write") {
		t.Fatalf("undo must say it is not surgical, got %q", out.String())
	}
}
