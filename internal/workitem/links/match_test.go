package links

import "testing"

// TestReleasedOnShelve covers the three arms a shelve matches on. The path arm
// is the 2026-08-20 regression: two links scoped to
// workflow/explore/firstmate-competitive-analysis carried the workitem id the
// directory had under its previous name, so an id/key-only match left them
// behind pointing at a path the sweep had just emptied.
func TestReleasedOnShelve(t *testing.T) {
	const (
		id     = "explore-firstmate-competitive-analysis-2026-08-19"
		key    = "explore:workflow/explore/firstmate-competitive-analysis"
		dirRel = "workflow/explore/firstmate-competitive-analysis"
	)

	tests := []struct {
		name string
		link Link
		id   string
		key  string
		dir  string
		want bool
	}{
		// Non-matches first.
		{
			name: "an unrelated link is untouched",
			link: Link{WorkitemID: "design-something-else", Scope: LinkScope{Kind: ScopeProject, Path: "projects/camp"}},
			id:   id, key: key, dir: dirRel,
			want: false,
		},
		{
			name: "a sibling sharing a path prefix must NOT match",
			link: Link{WorkitemID: "other", Scope: LinkScope{
				Kind: ScopeCampaignPath, Path: "workflow/explore/firstmate-competitive-analysis-notes",
			}},
			id: id, key: key, dir: dirRel,
			want: false,
		},
		{
			name: "the parent directory is not inside the workitem",
			link: Link{WorkitemID: "other", Scope: LinkScope{Kind: ScopeCampaignPath, Path: "workflow/explore"}},
			id:   id, key: key, dir: dirRel,
			want: false,
		},
		{
			name: "an empty dirRel releases nothing by path",
			link: Link{WorkitemID: "other", Scope: LinkScope{Kind: ScopeCampaignPath, Path: "workflow/explore/anything"}},
			id:   "", key: "", dir: "",
			want: false,
		},
		{
			name: "an empty scope path never matches a real directory",
			link: Link{WorkitemID: "other", Scope: LinkScope{Kind: ScopeProject, Path: ""}},
			id:   id, key: key, dir: dirRel,
			want: false,
		},
		// Matches.
		{
			name: "id match",
			link: Link{WorkitemID: id, Scope: LinkScope{Kind: ScopeProject, Path: "projects/camp"}},
			id:   id, key: key, dir: dirRel,
			want: true,
		},
		{
			name: "key match with a different id",
			link: Link{WorkitemID: "stale-id", WorkitemKey: key, Scope: LinkScope{Kind: ScopeProject, Path: "projects/camp"}},
			id:   id, key: key, dir: dirRel,
			want: true,
		},
		{
			name: "exact path match under a different workitem id (the rename regression)",
			link: Link{
				WorkitemID: "explore-gnhf-competitive-analysis-2026-08-20",
				Scope:      LinkScope{Kind: ScopeCampaignPath, Path: dirRel},
			},
			id: id, key: key, dir: dirRel,
			want: true,
		},
		{
			name: "nested path match under a different workitem id",
			link: Link{
				WorkitemID: "explore-gnhf-competitive-analysis-2026-08-20",
				Scope:      LinkScope{Kind: ScopeCampaignPath, Path: dirRel + "/.workflow"},
			},
			id: id, key: key, dir: dirRel,
			want: true,
		},
		{
			name: "path-only match still applies when the shelved item has no id or key",
			link: Link{WorkitemID: "whatever", Scope: LinkScope{Kind: ScopeCampaignPath, Path: dirRel + "/notes"}},
			id:   "", key: "", dir: dirRel,
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ReleasedOnShelve(tc.link, tc.id, tc.key, tc.dir); got != tc.want {
				t.Errorf("ReleasedOnShelve(%+v, %q, %q, %q) = %v, want %v",
					tc.link, tc.id, tc.key, tc.dir, got, tc.want)
			}
		})
	}
}

func TestScopeWithin(t *testing.T) {
	tests := []struct {
		name  string
		scope string
		dir   string
		want  bool
	}{
		{name: "both empty", scope: "", dir: "", want: false},
		{name: "empty dir", scope: "workflow/explore/a", dir: "", want: false},
		{name: "empty scope", scope: "", dir: "workflow/explore/a", want: false},
		{name: "dot dir matches nothing", scope: "workflow/explore/a", dir: ".", want: false},
		{name: "absolute dir matches nothing", scope: "workflow/explore/a", dir: "/workflow/explore", want: false},
		{name: "escaping dir matches nothing", scope: "workflow/explore/a", dir: "../elsewhere", want: false},
		{name: "prefix without a segment boundary", scope: "workflow/explore/ab", dir: "workflow/explore/a", want: false},
		{name: "exact", scope: "workflow/explore/a", dir: "workflow/explore/a", want: true},
		{name: "nested one level", scope: "workflow/explore/a/b", dir: "workflow/explore/a", want: true},
		{name: "nested deeply", scope: "workflow/explore/a/b/c/d", dir: "workflow/explore/a", want: true},
		{name: "leading ./ is normalized away", scope: "./workflow/explore/a/b", dir: "workflow/explore/a", want: true},
		{name: "trailing slash is normalized away", scope: "workflow/explore/a/", dir: "workflow/explore/a", want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ScopeWithin(tc.scope, tc.dir); got != tc.want {
				t.Errorf("ScopeWithin(%q, %q) = %v, want %v", tc.scope, tc.dir, got, tc.want)
			}
		})
	}
}
