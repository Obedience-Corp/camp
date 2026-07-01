package notes

import "testing"

func TestBuildGroupsNestsPRConstituents(t *testing.T) {
	t.Parallel()

	entries := []RawEntry{
		{
			Subject: "fix(worktrees): keep git worktrees out of campaign status and commits (#361)",
			IsMerge: true,
			ChildSubjects: []string{
				"[obey-campaign:8deed8b4] fix(repair): commit in-place root .gitignore edits during repair",
				"[obey-campaign:8deed8b4] fix(repair): derive worktrees .gitignore check from configured path",
				"Merge branch 'main' into fix/worktrees-gitignore",
				"[obey-campaign:8deed8b4] fix(worktrees): keep git worktrees out of campaign status and commits",
			},
		},
		{
			Subject: "feat(commitkit): expose campaign name accessors (#360)",
			IsMerge: true,
			ChildSubjects: []string{
				"[obey-campaign:8deed8b4] feat(commitkit): expose campaign name accessors",
			},
		},
	}

	groups := BuildGroups(entries)

	if len(groups) != 2 {
		t.Fatalf("BuildGroups() len = %d, want 2", len(groups))
	}

	pr361 := groups[0]
	if pr361.Change.PRNumber != 361 || pr361.Change.Category != CategoryFix {
		t.Fatalf("group[0] headline = %+v, want #361 fix", pr361.Change)
	}
	if len(pr361.Children) != 2 {
		t.Fatalf("group[0] children = %d, want 2 (Merge + PR-title-dup filtered)\n%+v", len(pr361.Children), pr361.Children)
	}
	if pr361.Children[0].Text != "Commit in-place root .gitignore edits during repair" {
		t.Fatalf("group[0] child[0] = %q", pr361.Children[0].Text)
	}
	if pr361.Children[1].Text != "Derive worktrees .gitignore check from configured path" {
		t.Fatalf("group[0] child[1] = %q", pr361.Children[1].Text)
	}

	pr360 := groups[1]
	if pr360.Change.PRNumber != 360 || pr360.Change.Category != CategoryFeature {
		t.Fatalf("group[1] headline = %+v, want #360 feature", pr360.Change)
	}
	if len(pr360.Children) != 0 {
		t.Fatalf("group[1] children = %d, want 0 (lone commit equals PR title)", len(pr360.Children))
	}
}

func TestBuildGroupsKeepsStandaloneCommits(t *testing.T) {
	t.Parallel()

	entries := []RawEntry{
		{Subject: "fix(commit): unstage worktrees after staging instead of exclude pathspec"},
	}

	groups := BuildGroups(entries)
	if len(groups) != 1 {
		t.Fatalf("BuildGroups() len = %d, want 1", len(groups))
	}
	if len(groups[0].Children) != 0 {
		t.Fatalf("standalone commit should have no children, got %d", len(groups[0].Children))
	}
	if groups[0].Change.Text != "Unstage worktrees after staging instead of exclude pathspec" {
		t.Fatalf("standalone change text = %q", groups[0].Change.Text)
	}
}

func TestBuildGroupsDropsStatusChangeBookkeeping(t *testing.T) {
	t.Parallel()

	entries := []RawEntry{
		{Subject: "[OBEY-CAMPAIGN-8deed8b4] chore(fest): status change: festival-app-read-scaling-FA0019 (FA0019) ready -> active"},
		{
			Subject: "feat(x): real feature (#500)",
			IsMerge: true,
			ChildSubjects: []string{
				"[OBEY-CAMPAIGN-8deed8b4] chore(fest): status change: something planning -> ready",
				"feat(x): add the real thing",
			},
		},
	}

	groups := BuildGroups(entries)
	if len(groups) != 1 {
		t.Fatalf("BuildGroups() len = %d, want 1 (standalone status-change dropped)", len(groups))
	}
	if len(groups[0].Children) != 1 {
		t.Fatalf("children = %d, want 1 (child status-change dropped)\n%+v", len(groups[0].Children), groups[0].Children)
	}
	if groups[0].Children[0].Text != "Add the real thing" {
		t.Fatalf("child[0] = %q", groups[0].Children[0].Text)
	}
}

func TestBuildGroupsDedupesChildrenWithinGroup(t *testing.T) {
	t.Parallel()

	entries := []RawEntry{
		{
			Subject: "fix(y): headline (#42)",
			IsMerge: true,
			ChildSubjects: []string{
				"fix(y): apply the same tweak",
				"fix(y): apply the same tweak",
			},
		},
	}

	groups := BuildGroups(entries)
	if len(groups[0].Children) != 1 {
		t.Fatalf("duplicate children should collapse to 1, got %d", len(groups[0].Children))
	}
}

func TestLeafChangesCountsChildrenNotHeadline(t *testing.T) {
	t.Parallel()

	groups := []Group{
		{Change: Change{Text: "Feature A", Category: CategoryFeature}},
		{
			Change: Change{Text: "PR headline", PRNumber: 7, Category: CategoryFix},
			Children: []Change{
				{Text: "Fix one", Category: CategoryFix},
				{Text: "Fix two", Category: CategoryFix},
				{Text: "Fix three", Category: CategoryFix},
			},
		},
	}

	leaves := LeafChanges(groups)
	if len(leaves) != 4 {
		t.Fatalf("LeafChanges() len = %d, want 4 (1 standalone + 3 children)", len(leaves))
	}
	for _, leaf := range leaves {
		if leaf.Text == "PR headline" {
			t.Fatalf("headline of a group with children must not be counted as a leaf")
		}
	}
}
