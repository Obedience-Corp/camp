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
