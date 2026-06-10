package config

import (
	"context"
	"reflect"
	"testing"
)

func nestedConceptFixture() []ConceptEntry {
	depth1 := 1
	return []ConceptEntry{
		{Name: "projects", Path: "projects/", Description: "Active development projects", Depth: &depth1, Ignore: []string{"worktrees/"}},
		{
			Name:        "workflow",
			Path:        "workflow/",
			Description: "Workflows",
			Children: []ConceptEntry{
				{Name: "festivals", Path: "festivals/", Description: "Planning cycles"},
				{Name: "design", Path: "workflow/design/", Description: "Design documents", Depth: &depth1},
				{Name: "explore", Path: "workflow/explore/", Description: "Exploratory notes", Depth: &depth1},
			},
		},
		{Name: "docs", Path: "docs/", Description: "Documentation"},
	}
}

func TestConceptChildren_RoundTrips(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	cfg := &CampaignConfig{
		ID:          "id-1",
		Name:        "round-trip",
		Type:        CampaignTypeProduct,
		ConceptList: nestedConceptFixture(),
	}

	if err := SaveCampaignConfig(ctx, root, cfg); err != nil {
		t.Fatalf("SaveCampaignConfig: %v", err)
	}

	loaded, err := LoadCampaignConfig(ctx, root)
	if err != nil {
		t.Fatalf("LoadCampaignConfig: %v", err)
	}

	if !reflect.DeepEqual(loaded.ConceptList, cfg.ConceptList) {
		t.Errorf("concept tree did not round-trip:\n got  %#v\n want %#v", loaded.ConceptList, cfg.ConceptList)
	}

	// The workflow parent must keep its children, in order.
	var workflow *ConceptEntry
	for i := range loaded.ConceptList {
		if loaded.ConceptList[i].Name == "workflow" {
			workflow = &loaded.ConceptList[i]
		}
	}
	if workflow == nil {
		t.Fatal("workflow concept missing after round-trip")
	}
	if len(workflow.Children) != 3 || workflow.Children[0].Name != "festivals" {
		t.Errorf("workflow children not preserved in order: %+v", workflow.Children)
	}
}

func TestConceptChildren_AbsentBehavesAsFlat(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	flat := []ConceptEntry{
		{Name: "projects", Path: "projects/"},
		{Name: "festivals", Path: "festivals/"},
		{Name: "docs", Path: "docs/"},
	}
	cfg := &CampaignConfig{ID: "id-2", Name: "flat", Type: CampaignTypeProduct, ConceptList: flat}

	if err := SaveCampaignConfig(ctx, root, cfg); err != nil {
		t.Fatalf("SaveCampaignConfig: %v", err)
	}
	loaded, err := LoadCampaignConfig(ctx, root)
	if err != nil {
		t.Fatalf("LoadCampaignConfig: %v", err)
	}

	if !reflect.DeepEqual(loaded.ConceptList, flat) {
		t.Errorf("flat config changed: got %#v want %#v", loaded.ConceptList, flat)
	}
	for _, c := range loaded.ConceptList {
		if c.Children != nil {
			t.Errorf("flat concept %q gained children: %+v", c.Name, c.Children)
		}
	}
}
