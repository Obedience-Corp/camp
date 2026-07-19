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
		refs := mergeProjectRefs(nil, registry)
		assert.NotNil(t, refs, "must be [] not nil so JSON encodes [] not null")
		assert.Len(t, refs, 0)
	})

	t.Run("primary and non-primary annotated independently in order", func(t *testing.T) {
		refs := mergeProjectRefs([]string{"projects/camp", "projects/fest", "projects/other"}, registry)
		assert.Equal(t, []wkitem.ProjectRef{
			{Path: "projects/camp", Primary: true},
			{Path: "projects/fest", Primary: false},
			{Path: "projects/other", Primary: false},
		}, refs)
	})

	t.Run("nil registry yields all non-primary", func(t *testing.T) {
		refs := mergeProjectRefs([]string{"projects/camp"}, nil)
		assert.Equal(t, []wkitem.ProjectRef{{Path: "projects/camp", Primary: false}}, refs)
	})
}
