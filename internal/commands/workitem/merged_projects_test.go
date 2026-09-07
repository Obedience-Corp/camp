package workitem

import (
	"testing"

	"github.com/stretchr/testify/assert"

	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
)

func TestMergeProjectRefs(t *testing.T) {
	registry := &links.Links{Links: []links.Link{
		{ID: "lnk_20260719_000001", Role: links.RolePrimary, Scope: links.LinkScope{Kind: links.ScopeProject, Path: "projects/camp"}},
		// A related (non-primary) link to fest must not count as primary, and a
		// primary link on a different scope kind must not match a project path.
		{ID: "lnk_20260719_000002", Role: links.RoleRelated, Scope: links.LinkScope{Kind: links.ScopeProject, Path: "projects/fest"}},
	}}

	t.Run("empty projects returns non-nil empty slice", func(t *testing.T) {
		refs := mergeProjectRefs("", wkitem.WorkItem{}, registry)
		assert.NotNil(t, refs, "must be [] not nil so JSON encodes [] not null")
		assert.Len(t, refs, 0)
	})

	t.Run("primary and non-primary annotated independently in order", func(t *testing.T) {
		item := wkitem.WorkItem{Projects: []string{"projects/camp", "projects/fest", "projects/other"}}
		refs := mergeProjectRefs("", item, registry)
		assert.Equal(t, []wkitem.ProjectRef{
			{Path: "projects/camp", Primary: true},
			{Path: "projects/fest", Primary: false},
			{Path: "projects/other", Primary: false},
		}, refs)
	})

	t.Run("nil registry yields all non-primary", func(t *testing.T) {
		item := wkitem.WorkItem{Projects: []string{"projects/camp"}}
		refs := mergeProjectRefs("", item, nil)
		assert.Equal(t, []wkitem.ProjectRef{{Path: "projects/camp", Primary: false}}, refs)
	})

	// A worktree is a checkout of a project, so a workitem that only holds a
	// worktree link still belongs to that project. Before this, such a workitem
	// showed no project at all.
	t.Run("link-derived project is appended with its worktree detail", func(t *testing.T) {
		item := wkitem.WorkItem{ProjectLinks: []wkitem.ProjectLink{
			{Path: "projects/camp", Worktree: "projects/worktrees/camp/feature-x", Primary: true},
		}}
		refs := mergeProjectRefs("", item, registry)
		assert.Equal(t, []wkitem.ProjectRef{{
			Path:            "projects/camp",
			Primary:         true,
			Worktree:        "projects/worktrees/camp/feature-x",
			WorktreeMissing: true,
		}}, refs)
	})

	// The project must not be listed twice when the workitem names it in
	// projects: and also reaches it through a worktree link.
	t.Run("worktree detail attaches to an already-listed project", func(t *testing.T) {
		item := wkitem.WorkItem{
			Projects: []string{"projects/camp"},
			ProjectLinks: []wkitem.ProjectLink{
				{Path: "projects/camp", Worktree: "projects/worktrees/camp/feature-x", Primary: true},
			},
		}
		refs := mergeProjectRefs("", item, registry)
		assert.Equal(t, []wkitem.ProjectRef{{
			Path:            "projects/camp",
			Primary:         true,
			Worktree:        "projects/worktrees/camp/feature-x",
			WorktreeMissing: true,
		}}, refs)
	})
}

func TestProjectLinksFor(t *testing.T) {
	layout := links.ScopeLayout{}
	const wiID = "design-example-2026-05-24"
	wi := &wkitem.WorkItem{StableID: wiID}

	cases := []struct {
		name string
		rows []links.Link
		want []wkitem.ProjectLink
		why  string
	}{
		{
			name: "unrelated rows are ignored",
			rows: []links.Link{{
				ID: "lnk_20260907_000001", WorkitemID: "other-workitem", Role: links.RolePrimary,
				Scope: links.LinkScope{Kind: links.ScopeWorktree, Path: "projects/worktrees/camp/x"},
			}},
			why: "a link belonging to another workitem must not lend it a project",
		},
		{
			name: "worktree path with no project segment yields nothing",
			rows: []links.Link{{
				ID: "lnk_20260907_000002", WorkitemID: wiID, Role: links.RolePrimary,
				Scope: links.LinkScope{Kind: links.ScopeWorktree, Path: "projects/worktrees/orphan"},
			}},
			why: "a path camp cannot read must not invent a project",
		},
		{
			name: "festival scope names no project",
			rows: []links.Link{{
				ID: "lnk_20260907_000003", WorkitemID: wiID, Role: links.RolePrimary,
				Scope: links.LinkScope{Kind: links.ScopeFestival, Path: "festivals/active/camp-CC0001"},
			}},
			why: "a festival is not a project",
		},
		{
			name: "worktree scope resolves to its owning project",
			rows: []links.Link{{
				ID: "lnk_20260907_000004", WorkitemID: wiID, Role: links.RolePrimary,
				Scope: links.LinkScope{Kind: links.ScopeWorktree, Path: "projects/worktrees/camp/feature-x"},
			}},
			want: []wkitem.ProjectLink{
				{Path: "projects/camp", Worktree: "projects/worktrees/camp/feature-x", Primary: true},
			},
			why: "the project is the relationship, the worktree is the detail",
		},
		{
			name: "recorded project wins over the path derivation",
			rows: []links.Link{{
				ID: "lnk_20260907_000005", WorkitemID: wiID, Role: links.RolePrimary,
				Scope: links.LinkScope{
					Kind: links.ScopeWorktree, Path: "projects/worktrees/camp/feature-x",
					Project: "projects/camp-timeline",
				},
			}},
			want: []wkitem.ProjectLink{
				{Path: "projects/camp-timeline", Worktree: "projects/worktrees/camp/feature-x", Primary: true},
			},
			why: "a moved or renamed holder makes the recorded project the durable answer",
		},
		{
			name: "the primary row supplies the detail for a repeated project",
			rows: []links.Link{
				{
					ID: "lnk_20260907_000006", WorkitemID: wiID, Role: links.RoleRelated,
					Scope: links.LinkScope{Kind: links.ScopeWorktree, Path: "projects/worktrees/camp/old"},
				},
				{
					ID: "lnk_20260907_000007", WorkitemID: wiID, Role: links.RolePrimary,
					Scope: links.LinkScope{Kind: links.ScopeWorktree, Path: "projects/worktrees/camp/current"},
				},
			},
			want: []wkitem.ProjectLink{
				{Path: "projects/camp", Worktree: "projects/worktrees/camp/current", Primary: true},
			},
			why: "the detail shown must be the worktree commit routing would use",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := projectLinksFor(layout, &links.Links{Links: tc.rows}, wi)
			assert.Equal(t, tc.want, got, tc.why)
		})
	}
}
