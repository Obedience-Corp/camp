package listview

import "testing"

func TestSectionsGroupsRowsByDomainKey(t *testing.T) {
	rows := []Row{
		{Key: "a", GroupKey: "platform", GroupLabel: "Platform", StyleToken: "group:platform"},
		{Key: "b", GroupKey: "platform", GroupLabel: "Platform", StyleToken: "group:platform"},
		{Key: "c", GroupKey: "docs", GroupLabel: "Docs", StyleToken: "group:docs"},
	}

	sections := Sections(rows, "group")
	if len(sections) != 2 {
		t.Fatalf("len(sections) = %d, want 2", len(sections))
	}
	if sections[0].Key != "platform" || len(sections[0].Rows) != 2 {
		t.Fatalf("first section = %#v, want platform with 2 rows", sections[0])
	}
	if sections[0].StyleToken != "group:platform" {
		t.Fatalf("style token = %q, want group:platform", sections[0].StyleToken)
	}
}

func TestSortUsesGenericSortKeys(t *testing.T) {
	rows := []Row{
		{Key: "b", Title: "B", SortKeys: []SortKey{{Name: "priority", Rank: 2}}},
		{Key: "a", Title: "A", SortKeys: []SortKey{{Name: "priority", Rank: 1}}},
	}

	Sort(rows)
	if rows[0].Key != "a" || rows[1].Key != "b" {
		t.Fatalf("sorted keys = %s, %s; want a, b", rows[0].Key, rows[1].Key)
	}
}

func TestFilterByGroup(t *testing.T) {
	rows := []Row{
		{Key: "a", GroupKey: "platform"},
		{Key: "b", GroupKey: "docs"},
	}

	filtered := Filter(rows, []string{"docs"})
	if len(filtered) != 1 || filtered[0].Key != "b" {
		t.Fatalf("filtered = %#v, want only b", filtered)
	}
}

func TestProjectLikeRowsUseSamePackage(t *testing.T) {
	rows := []Row{
		{
			Key:        "project:camp",
			Title:      "camp",
			Path:       "projects/camp",
			GroupKey:   "platform",
			GroupLabel: "Platform",
			StyleToken: "project-group:platform",
			Fields: map[string]string{
				"status": "active",
			},
		},
	}

	sections := Sections(rows, "group")
	if len(sections) != 1 {
		t.Fatalf("len(sections) = %d, want 1", len(sections))
	}
	if sections[0].Rows[0].Path != "projects/camp" {
		t.Fatalf("project path = %q, want projects/camp", sections[0].Rows[0].Path)
	}
}
