package fresh

import (
	"os"
	"path/filepath"
	"testing"

	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
)

func TestCanonicalPrunedBranch(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"feat/foo", "feat/foo"},
		{"origin/feat/foo", "feat/foo"},
		{"refs/heads/feat/foo", "feat/foo"},
		{"  origin/fix/bar  ", "fix/bar"},
		{"", ""},
	}
	for _, tc := range tests {
		if got := canonicalPrunedBranch(tc.in); got != tc.want {
			t.Errorf("canonicalPrunedBranch(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestWorktreeScopePath(t *testing.T) {
	root := "/campaigns/demo"
	got := worktreeScopePath(root, root+"/projects/worktrees/camp/ledger")
	if got != "projects/worktrees/camp/ledger" {
		t.Errorf("worktreeScopePath = %q, want campaign-relative slash path", got)
	}
}

func TestMatchWorktreeLinkBranch_PrefixedBranchUsesGitMap(t *testing.T) {
	active := []wkitem.WorkItem{
		{Key: "design:workflow/design/ledger", StableID: "design-ledger-01"},
	}
	linkList := []links.Link{
		{WorkitemID: "design-ledger-01", Scope: links.LinkScope{Kind: links.ScopeWorktree, Path: "projects/worktrees/camp/campaign-ledger-emission"}},
	}
	branchPaths := map[string]string{
		"feat/campaign-ledger-emission": "projects/worktrees/camp/campaign-ledger-emission",
	}

	wi, scope, ok := matchWorktreeLinkBranch(linkList, active, "feat/campaign-ledger-emission", branchPaths, false)
	if !ok {
		t.Fatal("prefixed branch must match via git worktree path, not directory basename")
	}
	if wi.Key != active[0].Key {
		t.Errorf("matched workitem %q, want %q", wi.Key, active[0].Key)
	}
	if scope != "projects/worktrees/camp/campaign-ledger-emission" {
		t.Errorf("ScopePath = %q, want the worktree link path", scope)
	}
}

func TestMatchWorktreeLinkBranch_PrefixedBranchMissesWithoutMap(t *testing.T) {
	active := []wkitem.WorkItem{
		{Key: "design:workflow/design/ledger", StableID: "design-ledger-01"},
	}
	linkList := []links.Link{
		{WorkitemID: "design-ledger-01", Scope: links.LinkScope{Kind: links.ScopeWorktree, Path: "projects/worktrees/camp/campaign-ledger-emission"}},
	}

	if _, _, ok := matchWorktreeLinkBranch(linkList, active, "feat/campaign-ledger-emission", nil, false); ok {
		t.Fatal("must not match feat/branch to a different directory basename (no prefix-strip heuristic)")
	}
}

func TestMatchWorktreeLinkBranch_GitPathWithNoLinkDoesNotBasenameGuess(t *testing.T) {
	active := []wkitem.WorkItem{
		{Key: "design:foo", StableID: "design-foo-01"},
	}
	linkList := []links.Link{
		{WorkitemID: "design-foo-01", Scope: links.LinkScope{Kind: links.ScopeWorktree, Path: "projects/worktrees/camp/foo"}},
	}
	branchPaths := map[string]string{
		"foo": "projects/worktrees/camp/other-dir",
	}
	if _, _, ok := matchWorktreeLinkBranch(linkList, active, "foo", branchPaths, false); ok {
		t.Fatal("when git recorded a path, do not fall back to a different basename")
	}
}

func TestCollectWorktreeLinkMatches_PrefixedAndOriginDeduped(t *testing.T) {
	active := []wkitem.WorkItem{
		{Key: "design:ledger", StableID: "design-ledger-01"},
	}
	linkList := []links.Link{
		{WorkitemID: "design-ledger-01", Scope: links.LinkScope{Kind: links.ScopeWorktree, Path: "projects/worktrees/camp/ledger"}},
	}
	branchPaths := map[string]string{
		"feat/ledger": "projects/worktrees/camp/ledger",
	}
	pruned := []string{"origin/feat/ledger", "feat/ledger"}

	matches, unmatched, _ := collectWorktreeLinkMatches(linkList, active, pruned, branchPaths, false)
	if len(matches) != 1 {
		t.Fatalf("expected 1 match after origin/ local dedupe, got %d: %+v", len(matches), matches)
	}
	if matches[0].Branch != "feat/ledger" {
		t.Errorf("branch = %q, want canonical feat/ledger", matches[0].Branch)
	}
	if len(unmatched) != 0 {
		t.Errorf("unmatched = %v, want none", unmatched)
	}
}

func TestCommitTagScopePath_UsesOwnMergedWorktree(t *testing.T) {
	wi := wkitem.WorkItem{Key: "design:foo", StableID: "design-foo-01"}
	all := []links.Link{
		{WorkitemID: "design-foo-01", Scope: links.LinkScope{Kind: links.ScopeProject, Path: "projects/camp"}},
		{WorkitemID: "design-foo-01", Scope: links.LinkScope{Kind: links.ScopeWorktree, Path: "projects/worktrees/camp/foo"}},
	}
	branchPaths := map[string]string{"feat/foo": "projects/worktrees/camp/foo"}
	got := commitTagScopePath(wi, all, "projects/camp", []string{"feat/foo"}, branchPaths)
	if got != "projects/worktrees/camp/foo" {
		t.Errorf("commit-tag ScopePath = %q, want the just-merged worktree", got)
	}
}

func TestCommitTagScopePath_NoWorktreeUsesProject(t *testing.T) {
	wi := wkitem.WorkItem{Key: "design:foo", StableID: "design-foo-01"}
	all := []links.Link{
		{WorkitemID: "design-foo-01", Scope: links.LinkScope{Kind: links.ScopeProject, Path: "projects/camp"}},
	}
	got := commitTagScopePath(wi, all, "projects/camp", []string{"feat/foo"}, nil)
	if got != "projects/camp" {
		t.Errorf("commit-tag ScopePath = %q, want project path", got)
	}
}

// TestHasOpenWork_CommitTagDoesNotCountOwnWorktree is Bug B: a commit-tag
// match used to set ScopePath to the project, so the item's own leftover
// worktree counted as other open work and the match was dropped.
func TestHasOpenWork_CommitTagDoesNotCountOwnWorktree(t *testing.T) {
	wi := wkitem.WorkItem{Key: "design:foo", StableID: "design-foo-01"}
	root := t.TempDir()
	wtRel := filepath.ToSlash(filepath.Join("projects", "worktrees", "camp", "foo"))
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(wtRel)), 0o755); err != nil {
		t.Fatal(err)
	}
	reg := &links.Links{Links: []links.Link{
		{WorkitemID: "design-foo-01", Scope: links.LinkScope{Kind: links.ScopeProject, Path: "projects/camp"}},
		{WorkitemID: "design-foo-01", Scope: links.LinkScope{Kind: links.ScopeWorktree, Path: wtRel}},
	}}
	branchPaths := map[string]string{"feat/foo": wtRel}
	scope := commitTagScopePath(wi, reg.Links, "projects/camp", []string{"feat/foo"}, branchPaths)

	if !HasOpenWork(root, reg, wi, "projects/camp", "projects/camp") {
		t.Fatal("precondition: project ScopePath must still treat the leftover worktree as open (the bug)")
	}
	if HasOpenWork(root, reg, wi, scope, "projects/camp") {
		t.Errorf("commit-tag ScopePath %q must not count the item's own worktree as other open work", scope)
	}
}
