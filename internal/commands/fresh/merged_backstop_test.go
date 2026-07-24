package fresh

import (
	"os"
	"path/filepath"
	"testing"

	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
	"github.com/Obedience-Corp/camp/pkg/commitkit"
)

func taggedSubject(ref, msg string) string {
	return commitkit.PrependContextTagsFullNamed("obey-campaign", "8deed8b4", "", "", ref, msg)
}

func TestWorkitemRefsFromSubjects(t *testing.T) {
	subjects := []string{
		taggedSubject("WI-abc123", "fix a thing"),
		"chore: no tag here",
		"",
		taggedSubject("WI-def456", "another fix"),
		taggedSubject("WI-abc123", "same ref again"),
	}
	got := workitemRefsFromSubjects(subjects)
	if len(got) != 2 {
		t.Fatalf("expected 2 distinct refs, got %d: %v", len(got), got)
	}
	if !got["WI-abc123"] || !got["WI-def456"] {
		t.Errorf("unexpected ref set: %v", got)
	}
}

func TestMatchWorktreeLinkBranch(t *testing.T) {
	active := []wkitem.WorkItem{
		{Key: "design:workflow/design/foo", StableID: "design-foo-01", RelativePath: "workflow/design/foo"},
	}
	linkList := []links.Link{
		// Worktree link whose dir basename matches the branch name.
		{WorkitemID: "design-foo-01", Scope: links.LinkScope{Kind: links.ScopeWorktree, Path: "projects/worktrees/camp/foo"}},
		// Repo-scope link with the same basename must be ignored (no branch identity).
		{WorkitemID: "design-foo-01", Scope: links.LinkScope{Kind: links.ScopeRepo, Path: "projects/other/foo"}},
	}

	wi, _, ok := matchWorktreeLinkBranch(linkList, active, "foo")
	if !ok || wi.Key != "design:workflow/design/foo" {
		t.Fatalf("expected worktree-link match for branch foo, got ok=%v wi=%+v", ok, wi)
	}

	if _, _, ok := matchWorktreeLinkBranch(linkList, active, "unrelated"); ok {
		t.Errorf("expected no match for a branch with no worktree link")
	}

	// Repo-scope only (no worktree link) must not match by basename.
	repoOnly := []links.Link{{WorkitemID: "design-foo-01", Scope: links.LinkScope{Kind: links.ScopeRepo, Path: "projects/other/foo"}}}
	if _, _, ok := matchWorktreeLinkBranch(repoOnly, active, "foo"); ok {
		t.Errorf("repo-scope link must not match a branch by basename")
	}
}

func TestRefMatchesActiveItem(t *testing.T) {
	item := wkitem.WorkItem{
		Key:            "design:workflow/design/foo",
		StableID:       "design-foo-01",
		RelativePath:   "workflow/design/foo",
		SourceMetadata: map[string]any{"ref": "WI-abc123"},
	}
	if !refMatchesActiveItem(map[string]bool{"WI-abc123": true}, item) {
		t.Errorf("expected match by WI- ref in SourceMetadata")
	}
	if !refMatchesActiveItem(map[string]bool{"design-foo-01": true}, item) {
		t.Errorf("expected match by StableID alias")
	}
	if refMatchesActiveItem(map[string]bool{"WI-999999": true}, item) {
		t.Errorf("expected no match for an unrelated ref")
	}
}

func TestActiveBackstopItems_ExcludesFestivalsAndIntents(t *testing.T) {
	items := []wkitem.WorkItem{
		{Key: "design:a", WorkflowType: wkitem.WorkflowTypeDesign},
		{Key: "festival:b", WorkflowType: wkitem.WorkflowTypeFestival},
		{Key: "intent:c", WorkflowType: wkitem.WorkflowTypeIntent},
		{Key: "explore:d", WorkflowType: wkitem.WorkflowTypeExplore},
	}
	got := activeBackstopItems(items)
	if len(got) != 2 {
		t.Fatalf("expected 2 items (design, explore), got %d", len(got))
	}
	for _, wi := range got {
		if wi.WorkflowType == wkitem.WorkflowTypeFestival || wi.WorkflowType == wkitem.WorkflowTypeIntent {
			t.Errorf("festival/intent leaked into backstop set: %+v", wi)
		}
	}
}

func TestHasOpenWork(t *testing.T) {
	wi := wkitem.WorkItem{Key: "design:foo", StableID: "design-foo-01"}
	const mergedPath = "projects/camp"

	t.Run("single scope, only the merged project link, is not open", func(t *testing.T) {
		reg := &links.Links{Links: []links.Link{
			{WorkitemID: "design-foo-01", Scope: links.LinkScope{Kind: links.ScopeProject, Path: mergedPath}},
		}}
		if HasOpenWork("/root", reg, wi, mergedPath) {
			t.Errorf("single-scope workitem should not be suppressed")
		}
	})

	t.Run("two distinct project links (WI-ca06e1 shape) is open", func(t *testing.T) {
		reg := &links.Links{Links: []links.Link{
			{WorkitemID: "design-foo-01", Scope: links.LinkScope{Kind: links.ScopeProject, Path: mergedPath}},
			{WorkitemID: "design-foo-01", Scope: links.LinkScope{Kind: links.ScopeProject, Path: "projects/festival-app"}},
		}}
		if !HasOpenWork("/root", reg, wi, mergedPath) {
			t.Errorf("workitem linked to a second project must be suppressed (open work elsewhere)")
		}
	})

	t.Run("stale worktree link with a missing dir does not count as open", func(t *testing.T) {
		root := t.TempDir()
		reg := &links.Links{Links: []links.Link{
			{WorkitemID: "design-foo-01", Scope: links.LinkScope{Kind: links.ScopeProject, Path: mergedPath}},
			{WorkitemID: "design-foo-01", Scope: links.LinkScope{Kind: links.ScopeWorktree, Path: "projects/worktrees/camp/gone"}},
		}}
		if HasOpenWork(root, reg, wi, mergedPath) {
			t.Errorf("a worktree link whose directory no longer exists must not count as open")
		}
	})

	t.Run("existing other worktree is open", func(t *testing.T) {
		root := t.TempDir()
		wtRel := filepath.Join("projects", "worktrees", "camp", "live")
		if err := os.MkdirAll(filepath.Join(root, wtRel), 0o755); err != nil {
			t.Fatal(err)
		}
		reg := &links.Links{Links: []links.Link{
			{WorkitemID: "design-foo-01", Scope: links.LinkScope{Kind: links.ScopeProject, Path: mergedPath}},
			{WorkitemID: "design-foo-01", Scope: links.LinkScope{Kind: links.ScopeWorktree, Path: filepath.ToSlash(wtRel)}},
		}}
		if !HasOpenWork(root, reg, wi, mergedPath) {
			t.Errorf("an existing other worktree must count as open work")
		}
	})
}
