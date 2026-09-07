package workitem

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/workitem/links"
)

// `camp workitem links` is where a worktree row was most visibly shown as if it
// were a project. The project belongs in its own column, with the scope path
// beside it. Rendering takes a registry value and touches no filesystem, so it
// stays on the host.
func TestEmitLinksHuman_NamesTheProjectForEveryScope(t *testing.T) {
	created := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	list := []links.Link{
		{
			ID: "lnk_20260907_000001", WorkitemID: "design-a", Role: links.RolePrimary, CreatedAt: created,
			Scope: links.LinkScope{Kind: links.ScopeWorktree, Path: "projects/worktrees/camp-timeline/host"},
		},
		{
			ID: "lnk_20260907_000002", WorkitemID: "design-b", Role: links.RolePrimary, CreatedAt: created,
			Scope: links.LinkScope{Kind: links.ScopeFestival, Path: "festivals/active/camp-CC0001"},
		},
	}
	var buf bytes.Buffer
	if err := emitLinksHuman(&buf, links.ScopeLayout{}, list); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "PROJECT") {
		t.Fatalf("listing must carry a project column:\n%s", out)
	}
	worktreeRow := rowContaining(t, out, "lnk_20260907_000001")
	if !strings.Contains(worktreeRow, "projects/camp-timeline") {
		t.Fatalf("worktree row must name its project:\n%s", worktreeRow)
	}
	if !strings.Contains(worktreeRow, "worktree:projects/worktrees/camp-timeline/host") {
		t.Fatalf("worktree row must keep the scope path as the detail:\n%s", worktreeRow)
	}
	festivalRow := rowContaining(t, out, "lnk_20260907_000002")
	if !strings.Contains(festivalRow, "-") {
		t.Fatalf("a scope with no project must render a placeholder:\n%s", festivalRow)
	}
	if strings.Contains(festivalRow, "projects/") {
		t.Fatalf("a festival scope must not be given a project:\n%s", festivalRow)
	}
}

func rowContaining(t *testing.T, out, needle string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	t.Fatalf("no row containing %q in:\n%s", needle, out)
	return ""
}
