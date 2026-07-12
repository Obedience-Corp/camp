package workitem

import (
	"context"
	"testing"

	"github.com/Obedience-Corp/camp/internal/workitem/links"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
)

func TestSanitizeWorktreeName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"camp-settings-tui", "camp-settings-tui"},
		{"Fest List Watch", "fest-list-watch"},
		{"  spaced  ", "spaced"},
		{"weird/slashes:and.dots", "weird-slashes-and-dots"},
		{"--leading-and-trailing--", "leading-and-trailing"},
		{"multiple___under", "multiple___under"},
		{"UPPER_case-42", "upper_case-42"},
		{"", ""},
		{"///", ""},
	}
	for _, tc := range cases {
		if got := sanitizeWorktreeName(tc.in); got != tc.want {
			t.Errorf("sanitizeWorktreeName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDeriveWorktreeName(t *testing.T) {
	cases := []struct {
		name string
		wi   *wkitem.WorkItem
		want string
	}{
		{"from relative path", &wkitem.WorkItem{RelativePath: "workflow/design/camp-settings-tui"}, "camp-settings-tui"},
		{"festival path", &wkitem.WorkItem{RelativePath: "festivals/active/fest-list-watch"}, "fest-list-watch"},
		{"falls back to key", &wkitem.WorkItem{Key: "design:grok-fix"}, "design-grok-fix"},
		{"falls back to title", &wkitem.WorkItem{Title: "My Big Feature"}, "my-big-feature"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := deriveWorktreeName(tc.wi); got != tc.want {
				t.Errorf("deriveWorktreeName() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLinkedProjects(t *testing.T) {
	wi := &wkitem.WorkItem{StableID: "wi-1", Key: "design:foo"}
	registry := &links.Links{Links: []links.Link{
		{WorkitemID: "wi-1", Scope: links.LinkScope{Kind: links.ScopeProject, Path: "projects/camp"}},
		{WorkitemID: "wi-1", Scope: links.LinkScope{Kind: links.ScopeProject, Path: "projects/camp"}},
		{WorkitemID: "wi-1", Scope: links.LinkScope{Kind: links.ScopeWorktree, Path: "projects/worktrees/camp/x"}},
		{WorkitemID: "other", Scope: links.LinkScope{Kind: links.ScopeProject, Path: "projects/fest"}},
	}}
	got := linkedProjects(registry, wi)
	if len(got) != 1 || got[0] != "camp" {
		t.Fatalf("linkedProjects = %v, want [camp]", got)
	}
}

func TestExistingWorktreeLink(t *testing.T) {
	wi := &wkitem.WorkItem{StableID: "wi-1", Key: "design:foo"}
	registry := &links.Links{Links: []links.Link{
		{WorkitemKey: "design:foo", Role: links.RolePrimary, Scope: links.LinkScope{Kind: links.ScopeWorktree, Path: "projects/worktrees/camp/foo"}},
	}}
	path, ok := existingWorktreeLink(registry, wi)
	if !ok || path != "projects/worktrees/camp/foo" {
		t.Fatalf("existingWorktreeLink = %q, %v; want the primary worktree path", path, ok)
	}

	none := &links.Links{Links: []links.Link{
		{WorkitemID: "wi-1", Role: links.RoleRelated, Scope: links.LinkScope{Kind: links.ScopeWorktree, Path: "x"}},
	}}
	if _, ok := existingWorktreeLink(none, wi); ok {
		t.Fatal("related (non-primary) worktree link must not count as existing")
	}
}

func TestResolveWorktreeProject(t *testing.T) {
	wi := &wkitem.WorkItem{StableID: "wi-1"}
	ctx := context.Background()

	if got, err := resolveWorktreeProject(ctx, "", &links.Links{}, wi, "explicit"); err != nil || got != "explicit" {
		t.Fatalf("flag should win: got %q, err %v", got, err)
	}

	single := &links.Links{Links: []links.Link{
		{WorkitemID: "wi-1", Scope: links.LinkScope{Kind: links.ScopeProject, Path: "projects/fest"}},
	}}
	if got, err := resolveWorktreeProject(ctx, "", single, wi, ""); err != nil || got != "fest" {
		t.Fatalf("single linked project: got %q, err %v", got, err)
	}

	if _, err := resolveWorktreeProject(ctx, "", &links.Links{}, wi, ""); err == nil {
		t.Fatal("no linked project must error asking for --project")
	}

	multi := &links.Links{Links: []links.Link{
		{WorkitemID: "wi-1", Scope: links.LinkScope{Kind: links.ScopeProject, Path: "projects/fest"}},
		{WorkitemID: "wi-1", Scope: links.LinkScope{Kind: links.ScopeProject, Path: "projects/camp"}},
	}}
	if _, err := resolveWorktreeProject(ctx, "", multi, wi, ""); err == nil {
		t.Fatal("multiple linked projects must error asking for --project")
	}
}
