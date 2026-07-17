package workitem

import (
	"testing"

	"github.com/Obedience-Corp/camp/internal/workitem/links"
)

func TestRehomePromotedLinks(t *testing.T) {
	const (
		oldID  = "design-x-2026-07-17"
		oldKey = "design:workflow/design/x"
		newID  = "SC0001"
		newKey = "festival:festivals/planning/x-SC0001"
	)

	worktreeScope := links.LinkScope{Kind: links.ScopeWorktree, Path: "projects/worktrees/demo/x"}

	t.Run("re-points a matching primary link by id", func(t *testing.T) {
		reg := &links.Links{Links: []links.Link{
			{ID: "lnk_1", WorkitemID: oldID, WorkitemKey: oldKey, Scope: worktreeScope, Role: links.RolePrimary},
		}}
		if !rehomePromotedLinks(reg, oldID, oldKey, newID, newKey) {
			t.Fatal("expected registry to change")
		}
		got := reg.Links[0]
		if got.WorkitemID != newID || got.WorkitemKey != newKey {
			t.Fatalf("link not re-pointed: id=%q key=%q", got.WorkitemID, got.WorkitemKey)
		}
	})

	t.Run("no match leaves registry unchanged", func(t *testing.T) {
		reg := &links.Links{Links: []links.Link{
			{ID: "lnk_1", WorkitemID: "other-id", WorkitemKey: "design:workflow/design/other", Scope: worktreeScope, Role: links.RolePrimary},
		}}
		if rehomePromotedLinks(reg, oldID, oldKey, newID, newKey) {
			t.Fatal("expected no change for a non-matching link")
		}
	})

	t.Run("dedupes links that collide after re-pointing", func(t *testing.T) {
		// Two links referencing the promoted workitem on the same scope+role
		// (one already on the festival) collapse into a single entry.
		reg := &links.Links{Links: []links.Link{
			{ID: "lnk_1", WorkitemID: oldID, WorkitemKey: oldKey, Scope: worktreeScope, Role: links.RolePrimary},
			{ID: "lnk_2", WorkitemID: newID, WorkitemKey: newKey, Scope: worktreeScope, Role: links.RolePrimary},
		}}
		if !rehomePromotedLinks(reg, oldID, oldKey, newID, newKey) {
			t.Fatal("expected registry to change")
		}
		if len(reg.Links) != 1 {
			t.Fatalf("expected duplicate links to collapse to 1, got %d", len(reg.Links))
		}
	})
}
