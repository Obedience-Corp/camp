package nav

import (
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
)

func TestResolveConfiguredTarget_UsesShortcutKey(t *testing.T) {
	cfg := &config.CampaignConfig{
		Jumps: &config.JumpsConfig{
			Shortcuts: map[string]config.ShortcutConfig{
				"de": {Path: "workflow/design/"},
			},
		},
	}

	got := ResolveConfiguredTarget(cfg, []string{"de"})
	if !got.Matched {
		t.Fatal("ResolveConfiguredTarget() did not match shortcut key")
	}
	if got.Category != CategoryDesign {
		t.Fatalf("Category = %q, want %q", got.Category, CategoryDesign)
	}
}

func TestResolveConfiguredTarget_UsesConfiguredConceptName(t *testing.T) {
	cfg := &config.CampaignConfig{
		ConceptList: []config.ConceptEntry{
			{Name: "research", Path: "notes/research/"},
		},
		Jumps: &config.JumpsConfig{
			Shortcuts: map[string]config.ShortcutConfig{
				"r": {Path: "notes/research/"},
			},
		},
	}

	got := ResolveConfiguredTarget(cfg, []string{"research"})
	if !got.Matched {
		t.Fatal("ResolveConfiguredTarget() did not match concept name")
	}
	if got.RelativePath != "notes/research/" {
		t.Fatalf("RelativePath = %q, want %q", got.RelativePath, "notes/research/")
	}
}

func TestResolveConfiguredTarget_UsesPathAliasFromShortcut(t *testing.T) {
	cfg := &config.CampaignConfig{
		Jumps: &config.JumpsConfig{
			Shortcuts: map[string]config.ShortcutConfig{
				"ai": {Path: "ai_docs/"},
			},
		},
	}

	got := ResolveConfiguredTarget(cfg, []string{"ai_docs"})
	if !got.Matched {
		t.Fatal("ResolveConfiguredTarget() did not match long-form path alias")
	}
	if got.Category != CategoryAIDocs {
		t.Fatalf("Category = %q, want %q", got.Category, CategoryAIDocs)
	}
}

func TestResolveConfiguredTarget_SupportsSlashDrill(t *testing.T) {
	cfg := &config.CampaignConfig{
		ConceptList: []config.ConceptEntry{
			{Name: "design", Path: "workflow/design/"},
		},
	}

	got := ResolveConfiguredTarget(cfg, []string{"design/festival_app"})
	if !got.Matched {
		t.Fatal("ResolveConfiguredTarget() did not match slash drill")
	}
	if got.Category != CategoryDesign {
		t.Fatalf("Category = %q, want %q", got.Category, CategoryDesign)
	}
	if got.Query != "festival_app" {
		t.Fatalf("Query = %q, want %q", got.Query, "festival_app")
	}
}

func TestResolveConfiguredTarget_SupportsShortcutAtDrill(t *testing.T) {
	cfg := &config.CampaignConfig{
		Jumps: &config.JumpsConfig{
			Shortcuts: map[string]config.ShortcutConfig{
				"de": {Path: "workflow/design/"},
			},
		},
	}

	got := ResolveConfiguredTarget(cfg, []string{"de@festival_app/components"})
	if !got.Matched {
		t.Fatal("ResolveConfiguredTarget() did not match shortcut drill")
	}
	if got.Category != CategoryDesign {
		t.Fatalf("Category = %q, want %q", got.Category, CategoryDesign)
	}
	if got.Query != "festival_app/components" {
		t.Fatalf("Query = %q, want %q", got.Query, "festival_app/components")
	}
}

func TestTopLevelNavigationNames_IncludesShortcutsAliasesAndConcepts(t *testing.T) {
	cfg := &config.CampaignConfig{
		ConceptList: []config.ConceptEntry{
			{Name: "research", Path: "notes/research/"},
			{Name: "design", Path: "workflow/design/"},
		},
		Jumps: &config.JumpsConfig{
			Shortcuts: map[string]config.ShortcutConfig{
				"de": {Path: "workflow/design/"},
				"ai": {Path: "ai_docs/"},
			},
		},
	}

	got := TopLevelNavigationNames(cfg)
	assertContainsName(t, got, "de")
	assertContainsName(t, got, "design")
	assertContainsName(t, got, "ai")
	assertContainsName(t, got, "ai_docs")
	assertContainsName(t, got, "research")
}

func assertContainsName(t *testing.T, names []string, want string) {
	t.Helper()
	for _, name := range names {
		if name == want {
			return
		}
	}
	t.Fatalf("names %v do not contain %q", names, want)
}
