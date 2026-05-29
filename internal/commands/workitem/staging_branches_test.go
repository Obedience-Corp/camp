package workitem

import (
	"os"
	"path/filepath"
	"testing"

	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
)

func TestSelectPrimaryFestivalScope_MatchesResolvedFestival(t *testing.T) {
	wi := &wkitem.WorkItem{StableID: "WI001", Key: "demo"}
	registry := &links.Links{Links: []links.Link{
		{WorkitemID: "WI001", Role: links.RolePrimary, Scope: links.LinkScope{Kind: links.ScopeFestival, Path: "festivals/active/AA0001"}},
		{WorkitemID: "WI001", Role: links.RolePrimary, Scope: links.LinkScope{Kind: links.ScopeFestival, Path: "festivals/active/ZZ0002"}},
	}}

	tests := []struct {
		name       string
		festivalID string
		want       string
	}{
		{"first festival", "AA0001", "festivals/active/AA0001"},
		{"second festival", "ZZ0002", "festivals/active/ZZ0002"},
		{"no id falls back to first", "", "festivals/active/AA0001"},
		{"unmatched id stages nothing", "QQ9999", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := selectPrimaryFestivalScope(registry, wi, tc.festivalID)
			if got != tc.want {
				t.Errorf("selectPrimaryFestivalScope(%q) = %q, want %q", tc.festivalID, got, tc.want)
			}
		})
	}
}

func TestSelectPrimaryFestivalScope_IgnoresNonPrimaryAndNonFestival(t *testing.T) {
	wi := &wkitem.WorkItem{StableID: "WI001", Key: "demo"}
	registry := &links.Links{Links: []links.Link{
		{WorkitemID: "WI001", Role: links.RoleRelated, Scope: links.LinkScope{Kind: links.ScopeFestival, Path: "festivals/active/AA0001"}},
		{WorkitemID: "WI001", Role: links.RolePrimary, Scope: links.LinkScope{Kind: links.ScopeProject, Path: "projects/demo"}},
		{WorkitemID: "OTHER", Role: links.RolePrimary, Scope: links.LinkScope{Kind: links.ScopeFestival, Path: "festivals/active/BB0003"}},
	}}
	if got := selectPrimaryFestivalScope(registry, wi, "AA0001"); got != "" {
		t.Errorf("selectPrimaryFestivalScope = %q, want empty (no primary festival link for workitem)", got)
	}
}

func TestCwdSubGitRepo_HonorsSymlinkedCampaignRoot(t *testing.T) {
	tmp := t.TempDir()
	realRoot := filepath.Join(tmp, "real")
	if err := os.MkdirAll(realRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	subRepo := filepath.Join(realRoot, "projects", "demo")
	if err := os.MkdirAll(filepath.Join(subRepo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	linkRoot := filepath.Join(tmp, "link")
	if err := os.Symlink(realRoot, linkRoot); err != nil {
		t.Skipf("symlink unsupported in this environment: %v", err)
	}

	cwdViaLink := filepath.Join(linkRoot, "projects", "demo")

	got, ok := cwdSubGitRepo(cwdViaLink, linkRoot)
	if !ok {
		t.Fatalf("cwdSubGitRepo should detect sub-repo via symlinked cwd; got ok=false")
	}
	wantCanonical, err := filepath.EvalSymlinks(subRepo)
	if err != nil {
		t.Fatal(err)
	}
	if got != wantCanonical {
		t.Errorf("cwdSubGitRepo = %q, want canonical %q", got, wantCanonical)
	}
}
