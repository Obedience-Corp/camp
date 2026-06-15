package config

import "testing"

func childByName(children []ConceptEntry, name string) (ConceptEntry, bool) {
	for _, c := range children {
		if c.Name == name {
			return c, true
		}
	}
	return ConceptEntry{}, false
}

func TestDefaultConcepts_NestedWorkflowShape(t *testing.T) {
	concepts := DefaultConcepts()

	byName := make(map[string]ConceptEntry, len(concepts))
	for _, c := range concepts {
		byName[c.Name] = c
	}

	// Top level is exactly projects, workflow, docs.
	for _, want := range []string{"projects", "workflow", "docs"} {
		if _, ok := byName[want]; !ok {
			t.Fatalf("default concepts should include top-level %q", want)
		}
	}
	// worktrees, intents, dungeon, and the workflow collections are NOT top-level.
	for _, gone := range []string{"worktrees", "intents", "dungeon", "explore", "design", "festivals"} {
		if _, ok := byName[gone]; ok {
			t.Fatalf("%q should not be a top-level concept anymore", gone)
		}
	}

	workflow := byName["workflow"]
	explore, ok := childByName(workflow.Children, "explore")
	if !ok {
		t.Fatal("explore should be a child of workflow")
	}
	if explore.Path != "workflow/explore/" {
		t.Fatalf("explore path = %q, want %q", explore.Path, "workflow/explore/")
	}
	if _, ok := childByName(workflow.Children, "festivals"); !ok {
		t.Fatal("festivals should be a child of workflow")
	}
}

func TestCampaignConfigConcepts_FallbackNestedShape(t *testing.T) {
	cfg := &CampaignConfig{} // no explicit ConceptList, uses fallback
	concepts := cfg.Concepts()

	var workflow *ConceptEntry
	for i := range concepts {
		switch concepts[i].Name {
		case "dungeon", "intents", "worktrees":
			t.Fatalf("fallback should not include top-level %q", concepts[i].Name)
		case "workflow":
			workflow = &concepts[i]
		}
	}
	if workflow == nil {
		t.Fatal("fallback concepts should include workflow")
	}
	explore, ok := childByName(workflow.Children, "explore")
	if !ok {
		t.Fatal("fallback workflow should include an explore child")
	}
	if explore.Path != "workflow/explore/" {
		t.Fatalf("fallback explore path = %q, want %q", explore.Path, "workflow/explore/")
	}
}

func TestDefaultNavigationShortcuts_DungeonIsNavigationOnly(t *testing.T) {
	shortcuts := DefaultNavigationShortcuts()

	intent, ok := shortcuts["i"]
	if !ok {
		t.Fatal("default navigation shortcuts should include i")
	}
	if intent.Path != ".campaign/intents/" {
		t.Fatalf("i path = %q, want %q", intent.Path, ".campaign/intents/")
	}
	if intent.Concept != "intent" {
		t.Fatalf("i concept = %q, want %q", intent.Concept, "intent")
	}

	wt, ok := shortcuts["wt"]
	if !ok {
		t.Fatal("default navigation shortcuts should include wt")
	}
	if wt.Concept != "project worktree" {
		t.Fatalf("wt concept = %q, want %q", wt.Concept, "project worktree")
	}

	ex, ok := shortcuts["ex"]
	if !ok {
		t.Fatal("default navigation shortcuts should include ex")
	}
	if ex.Path != "workflow/explore/" {
		t.Fatalf("ex path = %q, want %q", ex.Path, "workflow/explore/")
	}
	if ex.Concept != "" {
		t.Fatalf("ex concept = %q, want empty for navigation-only shortcut", ex.Concept)
	}

	du, ok := shortcuts["du"]
	if !ok {
		t.Fatal("default navigation shortcuts should include du")
	}
	if du.Path != "dungeon/" {
		t.Fatalf("du path = %q, want %q", du.Path, "dungeon/")
	}
	if du.Concept != "" {
		t.Fatalf("du concept = %q, want empty for navigation-only shortcut", du.Concept)
	}
}
