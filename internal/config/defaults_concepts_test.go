package config

import "testing"

func TestDefaultConcepts_ExploreIncludedDungeonExcluded(t *testing.T) {
	concepts := DefaultConcepts()

	byName := make(map[string]ConceptEntry, len(concepts))
	for _, c := range concepts {
		byName[c.Name] = c
	}

	explore, ok := byName["explore"]
	if !ok {
		t.Fatal("default concepts should include workflow/explore concept")
	}
	if explore.Path != "workflow/explore/" {
		t.Fatalf("explore path = %q, want %q", explore.Path, "workflow/explore/")
	}

	if _, ok := byName["dungeon"]; ok {
		t.Fatal("default concepts should not include dungeon concept")
	}

	intents, ok := byName["intents"]
	if !ok {
		t.Fatal("default concepts should include intents concept")
	}
	if intents.Path != ".campaign/intents/" {
		t.Fatalf("intents path = %q, want %q", intents.Path, ".campaign/intents/")
	}

	workflow, ok := byName["workflow"]
	if !ok {
		t.Fatal("default concepts should include workflow concept")
	}
	if !containsString(workflow.Ignore, "explore/") {
		t.Fatalf("workflow ignore list should include explore/, got %v", workflow.Ignore)
	}
}

func TestCampaignConfigConcepts_FallbackExcludesDungeonIncludesExplore(t *testing.T) {
	cfg := &CampaignConfig{} // no explicit ConceptList, uses fallback
	concepts := cfg.Concepts()

	hasExplore := false
	hasDungeon := false
	for _, c := range concepts {
		switch c.Name {
		case "intents":
			if c.Path != ".campaign/intents/" {
				t.Fatalf("fallback intents path = %q, want %q", c.Path, ".campaign/intents/")
			}
		case "explore":
			hasExplore = true
			if c.Path != "workflow/explore/" {
				t.Fatalf("fallback explore path = %q, want %q", c.Path, "workflow/explore/")
			}
		case "dungeon":
			hasDungeon = true
		}
	}

	if !hasExplore {
		t.Fatal("fallback concepts should include explore")
	}
	if hasDungeon {
		t.Fatal("fallback concepts should not include dungeon")
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

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
