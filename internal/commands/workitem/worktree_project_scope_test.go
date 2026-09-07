package workitem

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
)

// The listing is the surface the bug showed up on: a workitem whose only link
// is a worktree used to display no project at all, or the worktree path in a
// project's place. Discovery must hand every reader the owning project instead,
// with the worktree as the detail.
func TestDiscoverWorkitems_WorktreeLinkResolvesToItsProject(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	// The worktree directory is deliberately absent: this is what a workitem
	// looks like after its branch merged and the worktree was removed.
	seedLink(t, root, links.ScopeWorktree, "projects/worktrees/demo/merged-branch")

	state, err := discoverWorkitems(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	item, ok := findWorkitem(state.items, "design-example-2026-05-24")
	if !ok {
		t.Fatalf("seeded workitem not discovered; got %d items", len(state.items))
	}
	want := []wkitem.ProjectLink{{
		Path:     "projects/demo",
		Worktree: "projects/worktrees/demo/merged-branch",
		Primary:  true,
	}}
	if len(item.ProjectLinks) != 1 || item.ProjectLinks[0] != want[0] {
		t.Fatalf("ProjectLinks = %v, want %v", item.ProjectLinks, want)
	}

	refs := mergeProjectRefs(state.campaignRoot, item, state.registry)
	if len(refs) != 1 {
		t.Fatalf("projects = %v, want one entry naming the project", refs)
	}
	if refs[0].Path != "projects/demo" {
		t.Errorf("projects[0].path = %q, want projects/demo", refs[0].Path)
	}
	if refs[0].Worktree != "projects/worktrees/demo/merged-branch" {
		t.Errorf("projects[0].worktree = %q, want the worktree as the detail", refs[0].Worktree)
	}
	if !refs[0].WorktreeMissing {
		t.Error("projects[0].worktree_missing must report a worktree that is gone")
	}

	// The project the workitem reaches through a worktree is the project it
	// belongs to, so filtering by that project must find it.
	filtered := wkitem.FilterAdvanced(state.items, wkitem.FilterOptions{
		Projects: []string{"projects/demo"}, ShowParked: true,
	})
	if !containsWorkitem(filtered, "design-example-2026-05-24") {
		t.Fatal("--project projects/demo must match a workitem linked through a worktree of it")
	}

	other := wkitem.FilterAdvanced(state.items, wkitem.FilterOptions{
		Projects: []string{"projects/unrelated"}, ShowParked: true,
	})
	if containsWorkitem(other, "design-example-2026-05-24") {
		t.Fatal("--project must not match a project the workitem has no link to")
	}
}

func findWorkitem(items []wkitem.WorkItem, id string) (wkitem.WorkItem, bool) {
	for _, item := range items {
		if item.StableID == id {
			return item, true
		}
	}
	return wkitem.WorkItem{}, false
}

func containsWorkitem(items []wkitem.WorkItem, id string) bool {
	_, ok := findWorkitem(items, id)
	return ok
}

// `camp workitem links` is where a worktree row was most visibly shown as if it
// were a project. The project belongs in its own column, with the scope path
// beside it.
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

// Every worktree writer must record the project, not only worktree creation.
// A link made by hand goes through the same normalization.
func TestLink_WorktreeScopeRecordsItsProject(t *testing.T) {
	cases := []struct {
		name        string
		opts        linkOptions
		wantKind    links.ScopeKind
		wantPath    string
		wantProject string
		why         string
	}{
		{
			name:        "short worktree form",
			opts:        linkOptions{Selector: "design-example-2026-05-24", Worktree: "demo/feature-x", AllowMissing: true},
			wantKind:    links.ScopeWorktree,
			wantPath:    "projects/worktrees/demo/feature-x",
			wantProject: "projects/demo",
			why:         "the project is derivable at write time, so it is recorded",
		},
		{
			name:        "full worktree path",
			opts:        linkOptions{Selector: "design-example-2026-05-24", ExplicitPath: "projects/worktrees/demo/feature-y", AllowMissing: true},
			wantKind:    links.ScopeWorktree,
			wantPath:    "projects/worktrees/demo/feature-y",
			wantProject: "projects/demo",
		},
		{
			name:     "project scope carries no project field",
			opts:     linkOptions{Selector: "design-example-2026-05-24", Project: "demo"},
			wantKind: links.ScopeProject,
			wantPath: "projects/demo",
			why:      "a project scope already is its project",
		},
		{
			name:     "festival scope carries no project field",
			opts:     linkOptions{Selector: "design-example-2026-05-24", Festival: "demo-DE0001", AllowMissing: true},
			wantKind: links.ScopeFestival,
			wantPath: "festivals/active/demo-DE0001",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := linkTestCampaign(t)
			restore := chdir(t, root)
			defer restore()

			if err := runLink(context.Background(), newCmd(), tc.opts); err != nil {
				t.Fatalf("runLink: %v", err)
			}
			registry, err := links.Load(context.Background(), root)
			if err != nil {
				t.Fatal(err)
			}
			if len(registry.Links) != 1 {
				t.Fatalf("expected 1 link, got %d", len(registry.Links))
			}
			scope := registry.Links[0].Scope
			if scope.Kind != tc.wantKind || scope.Path != tc.wantPath {
				t.Fatalf("scope = %#v, want kind %s path %s", scope, tc.wantKind, tc.wantPath)
			}
			if scope.Project != tc.wantProject {
				t.Fatalf("scope.project = %q, want %q (%s)", scope.Project, tc.wantProject, tc.why)
			}
		})
	}
}
