package links

import (
	"context"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestScopeLayout_Derivation(t *testing.T) {
	custom := LayoutFor("repos/", "repos/trees/")

	cases := []struct {
		name          string
		layout        ScopeLayout
		path          string
		wantProject   string
		wantWorktree  string
		wantRoot      string
		wantUnderTree bool
		why           string
	}{
		{
			name: "empty path names nothing",
			path: "",
			why:  "an empty scope path must not resolve to a project",
		},
		{
			name: "worktrees root alone names nothing",
			path: "projects/worktrees",
			why:  "the holder root carries neither a project nor a worktree",
		},
		{
			name:          "project holder without a worktree names nothing",
			path:          "projects/worktrees/camp",
			wantUnderTree: true,
			why:           "one segment is a holder directory, not a worktree",
		},
		{
			name: "a project path is not a worktree",
			path: "projects/camp",
			// ProjectRoot answers; SplitWorktreePath does not.
			wantRoot: "projects/camp",
			why:      "projects/ and projects/worktrees/ must not be confused",
		},
		{
			name:          "worktree path splits into project and name",
			path:          "projects/worktrees/camp-timeline/festival-activity-host",
			wantProject:   "projects/camp-timeline",
			wantWorktree:  "festival-activity-host",
			wantUnderTree: true,
			why:           "this is the shape camp writes for every worktree",
		},
		{
			name:          "deeper worktree path still names its project",
			path:          "projects/worktrees/camp/feature-x/internal/pkg",
			wantProject:   "projects/camp",
			wantWorktree:  "feature-x",
			wantUnderTree: true,
			why:           "a path inside a worktree belongs to the same project",
		},
		{
			name:     "deeper project path resolves to the project root",
			path:     "projects/camp/internal/workitem",
			wantRoot: "projects/camp",
			why:      "the project, not the subdirectory, is the relationship",
		},
		{
			name: "a path outside the projects dir names nothing",
			path: "workflow/design/spike",
			why:  "campaign paths have no owning project",
		},
		{
			name:          "a configured layout is honored",
			layout:        custom,
			path:          "repos/trees/camp/feature-x",
			wantProject:   "repos/camp",
			wantWorktree:  "feature-x",
			wantUnderTree: true,
			why:           "the worktrees directory is configurable, not hardcoded",
		},
		{
			name:   "the default layout does not read a configured tree",
			layout: ScopeLayout{},
			path:   "repos/trees/camp/feature-x",
			why:    "a path outside the configured holder must not resolve",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.layout.WorktreeProject(tc.path); got != tc.wantProject {
				t.Errorf("WorktreeProject(%q) = %q, want %q (%s)", tc.path, got, tc.wantProject, tc.why)
			}
			if _, got := tc.layout.SplitWorktreePath(tc.path); got != tc.wantWorktree {
				t.Errorf("SplitWorktreePath(%q) name = %q, want %q (%s)", tc.path, got, tc.wantWorktree, tc.why)
			}
			if got := tc.layout.ProjectRoot(tc.path); got != tc.wantRoot {
				t.Errorf("ProjectRoot(%q) = %q, want %q (%s)", tc.path, got, tc.wantRoot, tc.why)
			}
			if got := tc.layout.UnderWorktrees(tc.path); got != tc.wantUnderTree {
				t.Errorf("UnderWorktrees(%q) = %v, want %v (%s)", tc.path, got, tc.wantUnderTree, tc.why)
			}
		})
	}
}

func TestScopeLayout_ProjectNameRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
		why  string
	}{
		{name: "empty", in: "", want: "", why: "an unnamed project has no path"},
		{name: "dot segment", in: ".", want: "", why: "a relative marker is not a project"},
		{name: "parent segment", in: "..", want: "", why: "escaping the campaign root is not a project"},
		{name: "nested name", in: "a/b", want: "", why: "a project name is a single segment"},
		{name: "plain name", in: "camp", want: "projects/camp"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			layout := ScopeLayout{}
			path := layout.ProjectPath(tc.in)
			if path != tc.want {
				t.Fatalf("ProjectPath(%q) = %q, want %q (%s)", tc.in, path, tc.want, tc.why)
			}
			if path == "" {
				return
			}
			if back := layout.ProjectName(path); back != tc.in {
				t.Fatalf("ProjectName(%q) = %q, want %q", path, back, tc.in)
			}
		})
	}
}

func TestLinkScope_ProjectFor(t *testing.T) {
	cases := []struct {
		name  string
		scope LinkScope
		want  string
		why   string
	}{
		{
			name:  "festival scope has no project",
			scope: LinkScope{Kind: ScopeFestival, Path: "festivals/active/camp-CC0001"},
			why:   "a festival is not a checkout of a project",
		},
		{
			name:  "campaign path has no project",
			scope: LinkScope{Kind: ScopeCampaignPath, Path: "workflow/design/spike"},
			why:   "design docs live in the camp, not in a project",
		},
		{
			name:  "project scope answers with itself",
			scope: LinkScope{Kind: ScopeProject, Path: "projects/camp"},
			want:  "projects/camp",
		},
		{
			name:  "worktree scope derives the project from its path",
			scope: LinkScope{Kind: ScopeWorktree, Path: "projects/worktrees/camp-timeline/host"},
			want:  "projects/camp-timeline",
			why:   "a registry written before the project field must still resolve",
		},
		{
			name: "a recorded project outranks the derivation",
			scope: LinkScope{
				Kind: ScopeWorktree, Path: "projects/worktrees/camp/host",
				Project: "projects/camp-timeline",
			},
			want: "projects/camp-timeline",
			why:  "the recorded project survives a moved or renamed holder",
		},
		{
			name:  "an unreadable worktree path names no project",
			scope: LinkScope{Kind: ScopeWorktree, Path: "projects/worktrees/stray"},
			why:   "camp must not invent a project it cannot derive",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.scope.ProjectFor(ScopeLayout{}); got != tc.want {
				t.Fatalf("ProjectFor = %q, want %q (%s)", got, tc.want, tc.why)
			}
		})
	}
}

func TestNormalizeScope(t *testing.T) {
	t.Run("leaves a caller-supplied project alone", func(t *testing.T) {
		in := LinkScope{Kind: ScopeWorktree, Path: "projects/worktrees/camp/x", Project: "projects/other"}
		if got := NormalizeScope(ScopeLayout{}, in).Project; got != "projects/other" {
			t.Fatalf("Project = %q; the writer knows the project better than the path does", got)
		}
	})
	t.Run("records the project a worktree path names", func(t *testing.T) {
		in := LinkScope{Kind: ScopeWorktree, Path: "projects/worktrees/camp/x"}
		if got := NormalizeScope(ScopeLayout{}, in).Project; got != "projects/camp" {
			t.Fatalf("Project = %q, want projects/camp", got)
		}
	})
	t.Run("adds nothing to a non-worktree scope", func(t *testing.T) {
		in := LinkScope{Kind: ScopeProject, Path: "projects/camp"}
		if got := NormalizeScope(ScopeLayout{}, in).Project; got != "" {
			t.Fatalf("Project = %q; a project scope already is its project", got)
		}
	})
}

func TestBackfillProjects(t *testing.T) {
	registry := &Links{Links: []Link{
		{ID: "lnk_20260907_000001", Scope: LinkScope{Kind: ScopeWorktree, Path: "projects/worktrees/camp/a"}},
		{ID: "lnk_20260907_000002", Scope: LinkScope{Kind: ScopeWorktree, Path: "projects/worktrees/fest/b", Project: "projects/kept"}},
		{ID: "lnk_20260907_000003", Scope: LinkScope{Kind: ScopeWorktree, Path: "projects/worktrees/stray"}},
		{ID: "lnk_20260907_000004", Scope: LinkScope{Kind: ScopeProject, Path: "projects/camp"}},
	}}

	changed := BackfillProjects(ScopeLayout{}, registry)
	if len(changed) != 1 || changed[0] != "lnk_20260907_000001" {
		t.Fatalf("changed = %v, want only the derivable row with no project", changed)
	}
	if got := registry.Links[0].Scope.Project; got != "projects/camp" {
		t.Errorf("row 1 project = %q, want projects/camp", got)
	}
	if got := registry.Links[1].Scope.Project; got != "projects/kept" {
		t.Errorf("row 2 project = %q; backfill must never overwrite a recorded project", got)
	}
	if got := registry.Links[2].Scope.Project; got != "" {
		t.Errorf("row 3 project = %q; an underivable path must stay empty", got)
	}
	if got := registry.Links[3].Scope.Project; got != "" {
		t.Errorf("row 4 project = %q; a project scope needs no project field", got)
	}
	if again := BackfillProjects(ScopeLayout{}, registry); len(again) != 0 {
		t.Fatalf("second pass changed %v; backfill must be idempotent", again)
	}
	if BackfillProjects(ScopeLayout{}, nil) != nil {
		t.Fatal("a nil registry must be a no-op")
	}
}

func TestValidate_ScopeProject(t *testing.T) {
	base := Link{
		ID:         "lnk_20260907_0000aa",
		WorkitemID: "design-example-2026-09-07",
		Role:       RolePrimary,
		CreatedAt:  time.Now().UTC().Add(-time.Hour),
		CreatedBy:  "test",
	}

	cases := []struct {
		name    string
		scope   LinkScope
		wantErr string
		why     string
	}{
		{
			name:  "absent project is valid",
			scope: LinkScope{Kind: ScopeWorktree, Path: "projects/worktrees/camp/x"},
			why:   "registries written before the field must keep loading",
		},
		{
			name:  "a project directory is valid",
			scope: LinkScope{Kind: ScopeWorktree, Path: "projects/worktrees/camp/x", Project: "projects/camp"},
		},
		{
			name:    "a festival scope may not carry one",
			scope:   LinkScope{Kind: ScopeFestival, Path: "festivals/active/x", Project: "projects/camp"},
			wantErr: "has no owning project",
			why:     "a festival is not a checkout of a project",
		},
		{
			name:    "a worktree path is not a project",
			scope:   LinkScope{Kind: ScopeWorktree, Path: "projects/worktrees/camp/x", Project: "projects/worktrees/camp/x"},
			wantErr: "must name a project directory",
			why:     "recording the worktree as the project is the bug being fixed",
		},
		{
			name:    "a subdirectory is not a project",
			scope:   LinkScope{Kind: ScopeWorktree, Path: "projects/worktrees/camp/x", Project: "projects/camp/internal"},
			wantErr: "must name a project directory",
		},
		{
			name:    "an absolute project is rejected",
			scope:   LinkScope{Kind: ScopeWorktree, Path: "projects/worktrees/camp/x", Project: "/projects/camp"},
			wantErr: "camp-relative",
		},
		{
			name:    "a traversing project is rejected",
			scope:   LinkScope{Kind: ScopeWorktree, Path: "projects/worktrees/camp/x", Project: "projects/../etc"},
			wantErr: "must not contain ..",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			link := base
			link.Scope = tc.scope
			errs := Validate(context.Background(), &Links{Version: LinksSchemaVersion, Links: []Link{link}},
				ValidateOptions{Now: time.Now()})
			var got string
			for _, e := range errs {
				if e.Field == "scope.project" {
					got = e.Message
				}
			}
			if tc.wantErr == "" {
				if got != "" {
					t.Fatalf("unexpected scope.project error %q (%s)", got, tc.why)
				}
				return
			}
			if !strings.Contains(got, tc.wantErr) {
				t.Fatalf("scope.project error = %q, want it to mention %q (%s)", got, tc.wantErr, tc.why)
			}
		})
	}
}

func TestValidate_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	errs := Validate(ctx, Empty(), ValidateOptions{})
	if len(errs) != 1 || errs[0].Field != "context" {
		t.Fatalf("Validate on a cancelled context = %v, want a single context failure", errs)
	}
}

// links.yaml is tracked and shared, so every registry already on disk predates
// the scope.project field. Loading one must not change, drop, or reject
// anything, and a row that never gains a project must not start emitting an
// empty key.
func TestScopeProject_YAMLCompatibility(t *testing.T) {
	const existing = `version: workitem-links/v1alpha1
links:
  - id: lnk_20260726_aaaaaa
    workitem_id: design-example-2026-07-26
    workitem_key: design:workflow/design/example
    scope:
      kind: worktree
      path: projects/worktrees/camp-timeline/festival-activity-host
    role: primary
    created_at: 2026-07-26T00:00:00Z
    created_by: test
`
	var registry Links
	if err := yaml.Unmarshal([]byte(existing), &registry); err != nil {
		t.Fatalf("a registry without scope.project must still load: %v", err)
	}
	if len(registry.Links) != 1 {
		t.Fatalf("loaded %d links, want 1", len(registry.Links))
	}
	if got := registry.Links[0].Scope.Project; got != "" {
		t.Fatalf("Scope.Project = %q, want empty on a row that does not record one", got)
	}
	if got := registry.Links[0].Scope.ProjectFor(ScopeLayout{}); got != "projects/camp-timeline" {
		t.Fatalf("ProjectFor = %q; an existing row must still resolve to its project", got)
	}

	unchanged, err := marshalYAML(&registry)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(unchanged), "project:") {
		t.Fatalf("re-saving an untouched registry added a project key:\n%s", unchanged)
	}

	BackfillProjects(ScopeLayout{}, &registry)
	filled, err := marshalYAML(&registry)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(filled), "project: projects/camp-timeline") {
		t.Fatalf("backfilled registry does not persist the project:\n%s", filled)
	}

	var reloaded Links
	if err := yaml.Unmarshal(filled, &reloaded); err != nil {
		t.Fatalf("reload backfilled registry: %v", err)
	}
	if got := reloaded.Links[0].Scope.Project; got != "projects/camp-timeline" {
		t.Fatalf("reloaded Scope.Project = %q, want projects/camp-timeline", got)
	}
}

// A camp may configure paths.projects and paths.worktrees. The writers infer a
// scope kind from that layout, so the validator has to read the same one, or
// camp rejects the link it just decided to write.
func TestValidate_HonoursAConfiguredLayout(t *testing.T) {
	custom := LayoutFor("repos/", "repos/trees/")
	base := Link{
		ID:         "lnk_20260907_0000bb",
		WorkitemID: "design-example-2026-09-07",
		Role:       RolePrimary,
		CreatedAt:  time.Now().UTC().Add(-time.Hour),
		CreatedBy:  "test",
	}

	cases := []struct {
		name    string
		layout  ScopeLayout
		scope   LinkScope
		wantErr string
		why     string
	}{
		{
			name:   "configured project path is accepted",
			layout: custom,
			scope:  LinkScope{Kind: ScopeProject, Path: "repos/camp"},
			why:    "this is the regression: the writer infers project here",
		},
		{
			name:   "configured worktree path is accepted",
			layout: custom,
			scope:  LinkScope{Kind: ScopeWorktree, Path: "repos/trees/camp/feat", Project: "repos/camp"},
		},
		{
			name:    "the configured worktrees dir is still not a project",
			layout:  custom,
			scope:   LinkScope{Kind: ScopeProject, Path: "repos/trees/camp/feat"},
			wantErr: "must not be under repos/trees/",
			why:     "a checkout is not a project under any layout",
		},
		{
			name:    "the default path is rejected under a configured layout",
			layout:  custom,
			scope:   LinkScope{Kind: ScopeProject, Path: "projects/camp"},
			wantErr: "requires path under repos/",
			why:     "the configured directory is the only projects directory",
		},
		{
			name:    "a configured path is rejected under the default layout",
			scope:   LinkScope{Kind: ScopeProject, Path: "repos/camp"},
			wantErr: "requires path under projects/",
			why:     "the default layout must not silently accept another camp's shape",
		},
		{
			name:  "the default layout still accepts the default paths",
			scope: LinkScope{Kind: ScopeWorktree, Path: "projects/worktrees/camp/feat"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			link := base
			link.Scope = tc.scope
			errs := Validate(context.Background(), &Links{Version: LinksSchemaVersion, Links: []Link{link}},
				ValidateOptions{Now: time.Now(), Layout: tc.layout})
			var got string
			for _, e := range errs {
				if e.Field == "scope.path" {
					got = e.Message
				}
			}
			if tc.wantErr == "" {
				if got != "" {
					t.Fatalf("unexpected scope.path error %q (%s)", got, tc.why)
				}
				return
			}
			if !strings.Contains(got, tc.wantErr) {
				t.Fatalf("scope.path error = %q, want it to mention %q (%s)", got, tc.wantErr, tc.why)
			}
		})
	}
}

// A recorded project is data the validator constrains to a few kinds, and
// ProjectFor must not read it on the others: links.Load does not validate, so a
// hand-edited row reaches every read surface as written.
func TestLinkScope_ProjectForIgnoresProjectOnKindsThatCannotCarryOne(t *testing.T) {
	cases := []struct {
		name string
		kind ScopeKind
		want string
	}{
		{name: "festival", kind: ScopeFestival},
		{name: "campaign path", kind: ScopeCampaignPath},
		{name: "repo keeps it", kind: ScopeRepo, want: "projects/camp"},
		{name: "project keeps it", kind: ScopeProject, want: "projects/camp"},
		{name: "worktree keeps it", kind: ScopeWorktree, want: "projects/camp"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scope := LinkScope{Kind: tc.kind, Path: "festivals/active/x", Project: "projects/camp"}
			if got := scope.ProjectFor(ScopeLayout{}); got != tc.want {
				t.Fatalf("ProjectFor = %q, want %q; a kind the validator forbids a project on "+
					"must not get one from an unvalidated registry", got, tc.want)
			}
		})
	}
}
