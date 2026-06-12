package concept

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/Obedience-Corp/camp/internal/config"
)

func itemNames(items []Item) []string {
	names := make([]string, len(items))
	for i, it := range items {
		names[i] = it.Name
	}
	return names
}

func itemPath(items []Item, name string) (string, bool) {
	for _, it := range items {
		if it.Name == name {
			return it.Path, true
		}
	}
	return "", false
}

func workflowParentConcepts(children ...config.ConceptEntry) []config.ConceptEntry {
	return []config.ConceptEntry{
		{Name: "projects", Path: "projects"},
		{Name: "workflow", Path: "workflow", Children: children},
		{Name: "docs", Path: "docs"},
	}
}

func TestListItems_ParentChildren_ConfigOnly(t *testing.T) {
	concepts := workflowParentConcepts(
		config.ConceptEntry{Name: "festivals", Path: "festivals"},
		config.ConceptEntry{Name: "design", Path: "workflow/design"},
	)
	fsys := fstest.MapFS{
		"workflow/design/doc/file": &fstest.MapFile{Data: []byte("")},
	}
	svc := NewFSService("", concepts, fsys)

	items, err := svc.ListItems(context.Background(), "workflow", "")
	if err != nil {
		t.Fatalf("ListItems: %v", err)
	}
	got := itemNames(items)
	if len(got) != 2 || got[0] != "festivals" || got[1] != "design" {
		t.Fatalf("children = %v, want [festivals design] in config order", got)
	}

	// Each child resolves to its own configured path.
	if p, _ := itemPath(items, "festivals"); p != "festivals" {
		t.Errorf("festivals path = %q, want festivals", p)
	}
	if p, _ := itemPath(items, "design"); p != "workflow/design" {
		t.Errorf("design path = %q, want workflow/design", p)
	}
}

func TestListItems_ParentChildren_MergesDiskOnly(t *testing.T) {
	concepts := workflowParentConcepts(
		config.ConceptEntry{Name: "design", Path: "workflow/design"},
	)
	fsys := fstest.MapFS{
		"workflow/design/file":    &fstest.MapFile{Data: []byte("")},
		"workflow/research/file":  &fstest.MapFile{Data: []byte("")}, // disk-only custom workflow
		"workflow/.hidden/file":   &fstest.MapFile{Data: []byte("")},
		"workflow/notes-file.txt": &fstest.MapFile{Data: []byte("")}, // not a dir, must not appear
	}
	svc := NewFSService("", concepts, fsys)

	items, err := svc.ListItems(context.Background(), "workflow", "")
	if err != nil {
		t.Fatalf("ListItems: %v", err)
	}
	got := itemNames(items)
	// design (config) first, then research (disk-only). No hidden/file entries.
	if len(got) != 2 {
		t.Fatalf("items = %v, want exactly [design research]", got)
	}
	if got[0] != "design" {
		t.Errorf("config child should come first, got %v", got)
	}
	if _, ok := itemPath(items, "research"); !ok {
		t.Errorf("disk-only workflow/research not surfaced: %v", got)
	}
}

func TestListItems_ParentChildren_FiltersIntent(t *testing.T) {
	concepts := workflowParentConcepts(
		config.ConceptEntry{Name: "intent", Path: "workflow/intent"},
		config.ConceptEntry{Name: "design", Path: "workflow/design"},
	)
	fsys := fstest.MapFS{
		"workflow/design/file": &fstest.MapFile{Data: []byte("")},
		"workflow/intent/file": &fstest.MapFile{Data: []byte("")},
	}
	svc := NewFSService("", concepts, fsys)

	items, err := svc.ListItems(context.Background(), "workflow", "")
	if err != nil {
		t.Fatalf("ListItems: %v", err)
	}
	for _, name := range itemNames(items) {
		if name == "intent" || name == "intents" {
			t.Fatalf("intent workflow should be filtered from the picker, got %v", itemNames(items))
		}
	}
}

func TestListItems_ParentChildren_DrillUsesChildPath(t *testing.T) {
	concepts := workflowParentConcepts(
		config.ConceptEntry{Name: "festivals", Path: "festivals"},
	)
	fsys := fstest.MapFS{
		"festivals/active/my-fest/FESTIVAL_GOAL.md": &fstest.MapFile{Data: []byte("")},
		"festivals/planning/other-fest/file":        &fstest.MapFile{Data: []byte("")},
		"workflow/festivals/decoy/file":             &fstest.MapFile{Data: []byte("")},
	}
	svc := NewFSService("", concepts, fsys)

	items, err := svc.ListItems(context.Background(), "workflow", "festivals")
	if err != nil {
		t.Fatalf("ListItems: %v", err)
	}
	got := itemNames(items)
	if len(got) != 2 || got[0] != "active" || got[1] != "planning" {
		t.Fatalf("drill items = %v, want [active planning] from festivals/, not workflow/festivals", got)
	}
	if p, _ := itemPath(items, "active"); p != "festivals/active" {
		t.Errorf("active path = %q, want festivals/active", p)
	}

	items, err = svc.ListItems(context.Background(), "workflow", "festivals/active")
	if err != nil {
		t.Fatalf("ListItems nested: %v", err)
	}
	if got := itemNames(items); len(got) != 1 || got[0] != "my-fest" {
		t.Fatalf("nested drill items = %v, want [my-fest]", got)
	}
	if p, _ := itemPath(items, "my-fest"); p != "festivals/active/my-fest" {
		t.Errorf("my-fest path = %q, want festivals/active/my-fest", p)
	}
}

func TestListItems_ParentChildren_DrillHonorsChildDepth(t *testing.T) {
	depth1 := 1
	concepts := workflowParentConcepts(
		config.ConceptEntry{Name: "design", Path: "workflow/design", Depth: &depth1},
	)
	fsys := fstest.MapFS{
		"workflow/design/doc/nested/file": &fstest.MapFile{Data: []byte("")},
	}
	svc := NewFSService("", concepts, fsys)

	items, err := svc.ListItems(context.Background(), "workflow", "design")
	if err != nil {
		t.Fatalf("ListItems: %v", err)
	}
	if len(items) != 1 || items[0].Name != "doc" {
		t.Fatalf("design items = %v, want [doc]", itemNames(items))
	}
	if !items[0].DrillDisabled {
		t.Error("child depth 1 should mark items at max depth DrillDisabled")
	}

	items, err = svc.ListItems(context.Background(), "workflow", "design/doc")
	if err != nil {
		t.Fatalf("ListItems beyond depth: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("drilling past child depth = %v, want empty", itemNames(items))
	}
}

func TestListItems_ParentChildren_DrillHonorsChildIgnore(t *testing.T) {
	concepts := workflowParentConcepts(
		config.ConceptEntry{Name: "festivals", Path: "festivals", Ignore: []string{"dungeon/"}},
	)
	fsys := fstest.MapFS{
		"festivals/active/my-fest/file": &fstest.MapFile{Data: []byte("")},
		"festivals/dungeon/done/file":   &fstest.MapFile{Data: []byte("")},
	}
	svc := NewFSService("", concepts, fsys)

	items, err := svc.ListItems(context.Background(), "workflow", "festivals")
	if err != nil {
		t.Fatalf("ListItems: %v", err)
	}
	if got := itemNames(items); len(got) != 1 || got[0] != "active" {
		t.Fatalf("drill items = %v, want [active] with dungeon ignored", got)
	}
}

func TestListItems_ParentChildren_DiskOnlyDirUsesParentPath(t *testing.T) {
	concepts := workflowParentConcepts(
		config.ConceptEntry{Name: "festivals", Path: "festivals"},
	)
	fsys := fstest.MapFS{
		"festivals/active/file":          &fstest.MapFile{Data: []byte("")},
		"workflow/research/topic/file":   &fstest.MapFile{Data: []byte("")},
		"workflow/research/another/file": &fstest.MapFile{Data: []byte("")},
	}
	svc := NewFSService("", concepts, fsys)

	items, err := svc.ListItems(context.Background(), "workflow", "research")
	if err != nil {
		t.Fatalf("ListItems: %v", err)
	}
	got := itemNames(items)
	if len(got) != 2 || got[0] != "another" || got[1] != "topic" {
		t.Fatalf("disk-only drill items = %v, want [another topic] under workflow/research", got)
	}
	if p, _ := itemPath(items, "topic"); p != "workflow/research/topic" {
		t.Errorf("topic path = %q, want workflow/research/topic", p)
	}
}

func TestListItems_ParentChildren_DepthZeroChildNotDrillable(t *testing.T) {
	depth0 := 0
	concepts := workflowParentConcepts(
		config.ConceptEntry{Name: "docs", Path: "docs", Depth: &depth0},
	)
	fsys := fstest.MapFS{
		"docs/api/file": &fstest.MapFile{Data: []byte("")},
	}
	svc := NewFSService("", concepts, fsys)

	items, err := svc.ListItems(context.Background(), "workflow", "")
	if err != nil {
		t.Fatalf("ListItems: %v", err)
	}
	if len(items) != 1 || items[0].Name != "docs" {
		t.Fatalf("submenu = %v, want [docs]", itemNames(items))
	}
	if !items[0].DrillDisabled {
		t.Error("depth-0 child should be DrillDisabled in the parent submenu")
	}
}

func TestList_PopulatesChildren(t *testing.T) {
	concepts := workflowParentConcepts(
		config.ConceptEntry{Name: "festivals", Path: "festivals"},
		config.ConceptEntry{Name: "design", Path: "workflow/design"},
	)
	svc := NewFSService("", concepts, fstest.MapFS{})

	list, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var workflow *Concept
	for i := range list {
		if list[i].Name == "workflow" {
			workflow = &list[i]
		}
	}
	if workflow == nil {
		t.Fatal("workflow concept missing from List")
	}
	if !workflow.HasItems {
		t.Error("workflow with children should report HasItems")
	}
	if len(workflow.Children) != 2 || workflow.Children[0].Name != "festivals" {
		t.Errorf("workflow.Children = %+v, want [festivals design]", workflow.Children)
	}
}
