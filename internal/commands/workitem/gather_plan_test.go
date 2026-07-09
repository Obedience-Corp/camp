package workitem

import (
	"strings"
	"testing"

	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
	"github.com/Obedience-Corp/camp/internal/workitem/priority"
)

func designItem(slug, title, id string) wkitem.WorkItem {
	item := wkitem.WorkItem{
		Key:          "design:workflow/design/" + slug,
		WorkflowType: wkitem.WorkflowTypeDesign,
		Title:        title,
		RelativePath: "workflow/design/" + slug,
		ItemKind:     wkitem.ItemKindDirectory,
		StableID:     id,
	}
	return item
}

func TestMatchGatherSelector(t *testing.T) {
	items := []wkitem.WorkItem{
		designItem("auth-flow", "Auth Flow", "design-auth-flow-2026-07-01"),
		designItem("auth-tokens", "Auth Tokens", "design-auth-tokens-2026-07-02"),
	}

	cases := []struct {
		name    string
		query   string
		want    string
		wantErr string
	}{
		{"by slug", "auth-flow", "workflow/design/auth-flow", ""},
		{"by path", "workflow/design/auth-tokens", "workflow/design/auth-tokens", ""},
		{"by path trailing slash", "workflow/design/auth-tokens/", "workflow/design/auth-tokens", ""},
		{"by id", "design-auth-flow-2026-07-01", "workflow/design/auth-flow", ""},
		{"by key", "design:workflow/design/auth-flow", "workflow/design/auth-flow", ""},
		{"slug case insensitive", "AUTH-FLOW", "workflow/design/auth-flow", ""},
		{"not found", "missing", "", "no workitem matched selector"},
		{"empty", "  ", "", "selector must not be empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := matchGatherSelector(items, tc.query)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("matchGatherSelector(%q) error = %v, want containing %q", tc.query, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("matchGatherSelector(%q) error = %v", tc.query, err)
			}
			if got.RelativePath != tc.want {
				t.Fatalf("matchGatherSelector(%q) = %s, want %s", tc.query, got.RelativePath, tc.want)
			}
		})
	}
}

func TestMatchGatherSelector_Ambiguous(t *testing.T) {
	items := []wkitem.WorkItem{
		designItem("dup", "First", "id-one"),
		designItem("dup", "Second", "id-two"),
	}
	_, err := matchGatherSelector(items, "dup")
	if err == nil || !strings.Contains(err.Error(), "multiple workitems") {
		t.Fatalf("expected ambiguity error, got %v", err)
	}
}

func TestDedupeGatherItems(t *testing.T) {
	a := designItem("a", "A", "id-a")
	b := designItem("b", "B", "id-b")
	got := dedupeGatherItems([]wkitem.WorkItem{a, b, a})
	if len(got) != 2 || got[0].RelativePath != a.RelativePath || got[1].RelativePath != b.RelativePath {
		t.Fatalf("dedupeGatherItems returned %+v", got)
	}
}

func TestGatherBlockedByRun(t *testing.T) {
	cases := []struct {
		name string
		run  *wkitem.LocalRunProgress
		want bool
	}{
		{"nil run", nil, false},
		{"no active run", &wkitem.LocalRunProgress{RunStatus: "running"}, false},
		{"active running", &wkitem.LocalRunProgress{ActiveRunID: "r1", RunStatus: "running"}, true},
		{"active created", &wkitem.LocalRunProgress{ActiveRunID: "r1", RunStatus: "created"}, true},
		{"completed", &wkitem.LocalRunProgress{ActiveRunID: "r1", RunStatus: "completed"}, false},
		{"abandoned", &wkitem.LocalRunProgress{ActiveRunID: "r1", RunStatus: "abandoned"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := gatherBlockedByRun(tc.run); got != tc.want {
				t.Fatalf("gatherBlockedByRun() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGatherReadme(t *testing.T) {
	linked := designItem("auth-flow", "Auth Flow", "id-a")
	linked.PrimaryDoc = "workflow/design/auth-flow/README.md"
	linked.Summary = "Login  and\nsession design."
	bare := designItem("auth-tokens", "", "id-b")

	got := gatherReadme("Unified Auth", "design", []wkitem.WorkItem{linked, bare})

	for _, want := range []string{
		"# Unified Auth\n",
		"combining 2 workitems",
		"- [Auth Flow](auth-flow/README.md): Login and session design.",
		"- auth-tokens (auth-tokens/)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("gatherReadme missing %q in:\n%s", want, got)
		}
	}
}

func TestGatherPrimaryDocRel(t *testing.T) {
	item := designItem("pkg", "Pkg", "id")
	cases := []struct {
		name string
		doc  string
		want string
	}{
		{"readme", "workflow/design/pkg/README.md", "pkg/README.md"},
		{"nested doc", "workflow/design/pkg/docs/01-scope.md", "pkg/docs/01-scope.md"},
		{"empty", "", ""},
		{"outside dir", "workflow/design/other/README.md", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			item.PrimaryDoc = tc.doc
			if got := gatherPrimaryDocRel(item); got != tc.want {
				t.Fatalf("gatherPrimaryDocRel(%q) = %q, want %q", tc.doc, got, tc.want)
			}
		})
	}
}

func TestMigrateGatherPriorities(t *testing.T) {
	const (
		keyA   = "design:workflow/design/a"
		keyB   = "design:workflow/design/b"
		target = "design:workflow/design/combined"
	)

	t.Run("highest priority wins and sources clear", func(t *testing.T) {
		store := priority.NewStore()
		priority.Set(store, keyA, priority.Low)
		priority.Set(store, keyB, priority.High)

		if !migrateGatherPriorities(store, []string{keyA, keyB}, target) {
			t.Fatal("expected store change")
		}
		if _, ok := store.ManualPriorities[keyA]; ok {
			t.Fatal("source A priority should be cleared")
		}
		if _, ok := store.ManualPriorities[keyB]; ok {
			t.Fatal("source B priority should be cleared")
		}
		if got := store.ManualPriorities[target].Priority; got != priority.High {
			t.Fatalf("target priority = %q, want high", got)
		}
	})

	t.Run("attention entries drop and shared group carries", func(t *testing.T) {
		store := priority.NewStore()
		priority.SetAttentionStage(store, keyA, priority.AttentionCurrent)
		priority.SetGroup(store, keyA, "auth")
		priority.SetGroup(store, keyB, "auth")

		migrateGatherPriorities(store, []string{keyA, keyB}, target)

		if _, ok := store.Attention[keyA]; ok {
			t.Fatal("source A attention should be cleared")
		}
		if _, ok := store.Attention[keyB]; ok {
			t.Fatal("source B attention should be cleared")
		}
		entry, ok := store.Attention[target]
		if !ok || entry.Group != "auth" {
			t.Fatalf("target attention = %+v (ok=%v), want group auth", entry, ok)
		}
		if entry.Stage != priority.AttentionNone {
			t.Fatalf("target stage = %q, want none", entry.Stage)
		}
	})

	t.Run("conflicting groups drop", func(t *testing.T) {
		store := priority.NewStore()
		priority.SetGroup(store, keyA, "auth")
		priority.SetGroup(store, keyB, "billing")

		migrateGatherPriorities(store, []string{keyA, keyB}, target)

		if _, ok := store.Attention[target]; ok {
			t.Fatal("target should not inherit a group when sources disagree")
		}
	})

	t.Run("no entries no change", func(t *testing.T) {
		store := priority.NewStore()
		if migrateGatherPriorities(store, []string{keyA, keyB}, target) {
			t.Fatal("expected no change for empty store")
		}
	})
}

func TestRehomeGatherLinks(t *testing.T) {
	link := func(id, wiID, scopePath string, role links.Role) links.Link {
		return links.Link{
			ID:         id,
			WorkitemID: wiID,
			Scope:      links.LinkScope{Kind: links.ScopeProject, Path: scopePath},
			Role:       role,
		}
	}

	t.Run("repoints and dedupes", func(t *testing.T) {
		reg := &links.Links{Links: []links.Link{
			link("lnk_20260701_aaaaaa", "src-a", "projects/camp", links.RolePrimary),
			link("lnk_20260701_bbbbbb", "src-b", "projects/camp", links.RolePrimary),
			link("lnk_20260701_cccccc", "other", "projects/fest", links.RolePrimary),
		}}
		idMap := map[string]string{"src-a": "combined", "src-b": "combined"}

		if !rehomeGatherLinks(reg, idMap) {
			t.Fatal("expected registry change")
		}
		if len(reg.Links) != 2 {
			t.Fatalf("links after rehome = %d, want 2 (duplicate dropped)", len(reg.Links))
		}
		if reg.Links[0].WorkitemID != "combined" {
			t.Fatalf("first link workitem = %q, want combined", reg.Links[0].WorkitemID)
		}
		if reg.Links[1].WorkitemID != "other" {
			t.Fatalf("unrelated link workitem = %q, want other", reg.Links[1].WorkitemID)
		}
	})

	t.Run("no matching ids no change", func(t *testing.T) {
		reg := &links.Links{Links: []links.Link{
			link("lnk_20260701_aaaaaa", "other", "projects/camp", links.RolePrimary),
		}}
		if rehomeGatherLinks(reg, map[string]string{"src-a": "combined"}) {
			t.Fatal("expected no change")
		}
	})
}
